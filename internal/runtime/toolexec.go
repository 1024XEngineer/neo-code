package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"neo-code/internal/checkpoint"
	providertypes "neo-code/internal/provider/types"
	runtimefacts "neo-code/internal/runtime/facts"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/tools"
)

type indexedToolCall struct {
	index int
	call  providertypes.ToolCall
}

// executeAssistantToolCalls 并发执行 assistant 返回的全部工具调用并返回结构化执行摘要。
func (s *Service) executeAssistantToolCalls(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
) (toolExecutionSummary, error) {
	if len(assistant.ToolCalls) == 0 {
		return toolExecutionSummary{}, nil
	}

	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()

	parallelism := resolveToolParallelism(len(assistant.ToolCalls))
	toolLocks := buildToolExecutionLocks(assistant.ToolCalls)
	taskCh := make(chan indexedToolCall)
	results := make([]tools.ToolResult, len(assistant.ToolCalls))
	completed := make([]bool, len(assistant.ToolCalls))
	writes := make([]bool, len(assistant.ToolCalls))
	var mu sync.Mutex
	var firstErr error
	var workerWG sync.WaitGroup

	checkContext := func() bool {
		return shouldStopToolExecution(&mu, &firstErr, execCtx.Err())
	}

	for i := 0; i < parallelism; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for task := range taskCh {
				result, wrote, err := s.executeOneToolCall(
					execCtx,
					state,
					snapshot,
					task.call,
					toolLocks[normalizeToolLockKey(task.call.Name)],
					checkContext,
				)
				mu.Lock()
				results[task.index] = result
				completed[task.index] = true
				writes[task.index] = wrote
				mu.Unlock()
				if err != nil {
					recordAndCancelOnFirstError(&mu, &firstErr, err, cancelExec)
				}
			}
		}()
	}

	for index, call := range assistant.ToolCalls {
		if checkContext() {
			break
		}
		taskCh <- indexedToolCall{index: index, call: call}
	}

	close(taskCh)
	workerWG.Wait()

	summary := toolExecutionSummary{
		Calls: append([]providertypes.ToolCall(nil), assistant.ToolCalls...),
	}
	for index, ok := range completed {
		if !ok {
			continue
		}
		summary.Results = append(summary.Results, results[index])
		if writes[index] {
			summary.HasSuccessfulWorkspaceWrite = true
		}
	}
	summary.HasSuccessfulVerification = hasSuccessfulVerificationResult(summary.Results)
	return summary, firstErr
}

// executeOneToolCall 在单个 worker 中执行一次工具调用并处理结果回写与事件发射。
func (s *Service) executeOneToolCall(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	call providertypes.ToolCall,
	toolLock *sync.Mutex,
	checkContext func() bool,
) (tools.ToolResult, bool, error) {
	if checkContext() {
		return tools.ToolResult{}, false, ctx.Err()
	}

	toolLock.Lock()
	defer toolLock.Unlock()

	beforeToolHookOutput := s.runHookPoint(ctx, state, runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{
		Metadata: map[string]any{
			"tool_call_id":   strings.TrimSpace(call.ID),
			"tool_name":      strings.TrimSpace(call.Name),
			"tool_arguments": strings.TrimSpace(call.Arguments),
			"workdir":        strings.TrimSpace(snapshot.Workdir),
		},
	})
	if beforeToolHookOutput.Blocked {
		reason := findHookBlockMessage(beforeToolHookOutput)
		blockSource := findHookBlockSource(beforeToolHookOutput)
		result := tools.NewErrorResult(call.Name, hookErrorClassBlocked, reason, map[string]any{
			"hook_id":     beforeToolHookOutput.BlockedBy,
			"hook_source": string(blockSource),
			"point":       string(runtimehooks.HookPointBeforeToolCall),
		})
		result.ToolCallID = call.ID
		result.ErrorClass = hookErrorClassBlocked
		s.emitRunScoped(ctx, EventHookBlocked, state, HookBlockedPayload{
			HookID:     strings.TrimSpace(beforeToolHookOutput.BlockedBy),
			Source:     string(blockSource),
			Point:      string(runtimehooks.HookPointBeforeToolCall),
			ToolCallID: strings.TrimSpace(call.ID),
			ToolName:   strings.TrimSpace(call.Name),
			Reason:     reason,
			Enforced:   true,
		})
		if err := s.appendToolMessageAndSave(ctx, state, call, result); err != nil {
			return result, false, err
		}
		s.emitRunScoped(ctx, EventToolResult, state, result)
		return result, false, nil
	}

	s.emitRunScoped(ctx, EventToolStart, state, call)

	isWrite := isFileWriteTool(call.Name)
	isBash := strings.EqualFold(strings.TrimSpace(call.Name), tools.ToolNameBash)

	var preSnaps map[string]fileSnapshot
	var preFingerprint checkpoint.WorkdirFingerprint
	var bashCapturedPaths []string
	var bashCommand string
	var touchedPaths []string
	var removeDirNestedPaths []string

	if isWrite {
		touchedPaths = toolCallTouchedPaths(call, snapshot.Workdir)
		if len(touchedPaths) > 0 {
			preSnaps = make(map[string]fileSnapshot, len(touchedPaths))
			for _, p := range touchedPaths {
				preSnaps[p] = captureFileSnapshot(p)
				if s.perEditStore != nil {
					_, _ = s.perEditStore.CapturePreWrite(p)
				}
				// remove_dir: recursively pre-capture all nested files/dirs.
				if strings.EqualFold(strings.TrimSpace(call.Name), tools.ToolNameFilesystemRemoveDir) {
					if info, err := os.Stat(p); err == nil && info.IsDir() {
						_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
							if err != nil || path == p {
								return nil
							}
							removeDirNestedPaths = append(removeDirNestedPaths, path)
							if s.perEditStore != nil {
								_, _ = s.perEditStore.CapturePreWrite(path)
							}
							return nil
						})
					}
				}
			}
		}
	} else if isBash && s.perEditStore != nil {
		bashCommand = bashCommandFromCall(call)
		if checkpoint.BashLikelyWritesFiles(bashCommand) {
			bashCapturedPaths = checkpoint.SourceFilesInWorkdir(bashCommand, snapshot.Workdir)
			if len(bashCapturedPaths) > 0 {
				_, _ = s.perEditStore.CaptureBatch(bashCapturedPaths)
			}
			if fp, _, err := checkpoint.ScanWorkdir(ctx, snapshot.Workdir, checkpoint.DefaultFingerprintOptions()); err == nil {
				preFingerprint = fp
			}
		}
	}

	result, execErr := s.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:       state.runID,
		SessionID:   state.session.ID,
		TaskID:      state.taskID,
		AgentID:     state.agentID,
		Capability:  state.capabilityToken,
		State:       state,
		Call:        call,
		Workdir:     snapshot.Workdir,
		ToolTimeout: snapshot.ToolTimeout,
	})

	if isWrite && len(preSnaps) > 0 && execErr == nil && !result.IsError {
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		diffs := make([]map[string]any, 0, len(preSnaps))
		for path, snap := range preSnaps {
			diff, err := snap.Diff()
			if err != nil {
				continue
			}
			diffs = append(diffs, map[string]any{
				"path":    path,
				"diff":    diff,
				"was_new": snap.WasNew(),
			})
		}
		if len(diffs) > 0 {
			result.Metadata["tool_diffs"] = diffs
			if len(diffs) == 1 {
				result.Metadata["tool_diff"] = diffs[0]["diff"]
				result.Metadata["tool_diff_new"] = diffs[0]["was_new"]
			}
		}
	}

	if isWrite && execErr == nil && !result.IsError && s.perEditStore != nil {
		switch strings.TrimSpace(call.Name) {
		case tools.ToolNameFilesystemRemoveDir:
			if len(removeDirNestedPaths) > 0 && len(touchedPaths) > 0 {
				allPaths := append([]string{touchedPaths[0]}, removeDirNestedPaths...)
				_ = s.perEditStore.CapturePostDelete(allPaths)
			} else if len(touchedPaths) > 0 {
				_ = s.perEditStore.CapturePostDelete(touchedPaths)
			}
		case tools.ToolNameFilesystemDeleteFile:
			if len(touchedPaths) > 0 {
				_ = s.perEditStore.CapturePostDelete(touchedPaths)
			}
		}
	}

	if isBash && preFingerprint != nil && execErr == nil && !result.IsError {
		if afterFP, _, err := checkpoint.ScanWorkdir(ctx, snapshot.Workdir, checkpoint.DefaultFingerprintOptions()); err == nil {
			fpDiff := checkpoint.DiffFingerprints(preFingerprint, afterFP)
			if len(fpDiff.Added) > 0 || len(fpDiff.Modified) > 0 || len(fpDiff.Deleted) > 0 {
				covered := make(map[string]struct{}, len(bashCapturedPaths))
				for _, p := range bashCapturedPaths {
					covered[filepath.Clean(p)] = struct{}{}
				}
				uncovered := collectUncoveredBashPaths(snapshot.Workdir, fpDiff, covered)
				s.emitBashSideEffectEvent(ctx, state, call, bashCommand, fpDiff, bashCapturedPaths, uncovered)
			}
		}
	}

	if errors.Is(execErr, context.Canceled) {
		s.emitAfterToolResultHook(ctx, state, call, result, execErr, snapshot.Workdir)
		s.emitAfterToolFailureHook(ctx, state, call, result, execErr, snapshot.Workdir)
		return result, false, execErr
	}
	if execErr != nil && strings.TrimSpace(result.Content) == "" {
		result.Content = execErr.Error()
	}
	s.emitAfterToolResultHook(ctx, state, call, result, execErr, snapshot.Workdir)
	if execErr != nil || result.IsError {
		s.emitAfterToolFailureHook(ctx, state, call, result, execErr, snapshot.Workdir)
	}

	if err := s.appendToolMessageAndSave(ctx, state, call, result); err != nil {
		if execErr != nil && errors.Is(err, context.Canceled) {
			s.emitRunScoped(ctx, EventToolResult, state, result)
		}
		return result, false, err
	}

	s.emitRunScoped(ctx, EventToolResult, state, result)
	s.emitTodoToolEvent(ctx, state, call, result, execErr)
	state.mu.Lock()
	hasFactsUpdate := false
	if state.factsCollector != nil {
		state.factsCollector.ApplyToolResult(call.Name, result)
		hasFactsUpdate = true
	}
	state.mu.Unlock()
	if hasFactsUpdate {
		s.emitFactsUpdated(state, "tool_result")
		if strings.EqualFold(strings.TrimSpace(call.Name), tools.ToolNameSpawnSubAgent) {
			s.emitSubAgentSnapshotUpdated(state, "tool_result")
		}
	}

	if isSuccessfulRememberToolCall(call.Name, result, execErr) {
		state.mu.Lock()
		state.rememberedThisRun = true
		state.mu.Unlock()
	}

	if isBash && execErr == nil && !result.IsError && len(bashCapturedPaths) > 0 {
		result.Facts.WorkspaceWrite = true
	}

	if checkContext() {
		return result, hasSuccessfulWorkspaceWriteFact(result, execErr), ctx.Err()
	}
	if execErr != nil {
		return result, false, nil
	}
	return result, hasSuccessfulWorkspaceWriteFact(result, execErr), nil
}

// resolveToolParallelism 计算本轮工具执行的并发上限，避免无界 goroutine 扩散。
func resolveToolParallelism(toolCallCount int) int {
	if toolCallCount <= 0 {
		return 1
	}
	if toolCallCount < defaultToolParallelism {
		return toolCallCount
	}
	return defaultToolParallelism
}

// buildToolExecutionLocks 按工具名构造互斥锁，确保同名工具调用在单轮内串行执行。
func buildToolExecutionLocks(calls []providertypes.ToolCall) map[string]*sync.Mutex {
	locks := make(map[string]*sync.Mutex, len(calls))
	for _, call := range calls {
		key := normalizeToolLockKey(call.Name)
		if _, exists := locks[key]; !exists {
			locks[key] = &sync.Mutex{}
		}
	}
	return locks
}

// normalizeToolLockKey 将工具名规范化为锁键，防止大小写差异导致重复并发执行。
func normalizeToolLockKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// rememberFirstError 记录首次错误，后续错误只保留用于日志和事件路径。
func rememberFirstError(mu *sync.Mutex, firstErr *error, err error) bool {
	if err == nil {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if *firstErr == nil {
		*firstErr = err
		return true
	}
	return false
}

// shouldStopToolExecution 统一判断工具执行是否应停止，并在上下文取消时兜底记录错误原因。
func shouldStopToolExecution(mu *sync.Mutex, firstErr *error, contextErr error) bool {
	mu.Lock()
	defer mu.Unlock()
	if contextErr != nil && *firstErr == nil {
		*firstErr = contextErr
	}
	return *firstErr != nil
}

// recordAndCancelOnFirstError 在首次记录错误时触发执行上下文取消，阻止后续工具继续派发。
func recordAndCancelOnFirstError(mu *sync.Mutex, firstErr *error, err error, cancel context.CancelFunc) {
	if rememberFirstError(mu, firstErr, err) {
		cancel()
	}
}

// emitTodoToolEvent 在 todo_write 调用后补充 Todo 领域事件。
func (s *Service) emitTodoToolEvent(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	result tools.ToolResult,
	execErr error,
) {
	if !strings.EqualFold(strings.TrimSpace(call.Name), tools.ToolNameTodoWrite) {
		return
	}

	action, _ := result.Metadata["action"].(string)
	payload := buildTodoEventPayload(state, strings.TrimSpace(action), "")
	if execErr == nil && !result.IsError {
		state.mu.Lock()
		if state.factsCollector != nil {
			state.factsCollector.ApplyTodoSnapshot(runtimefacts.TodoSummaryLike{
				RequiredOpen:      payload.Summary.RequiredOpen,
				RequiredCompleted: payload.Summary.RequiredCompleted,
				RequiredFailed:    payload.Summary.RequiredFailed,
			})
		}
		state.mu.Unlock()
		s.emitRunScoped(ctx, EventTodoUpdated, state, payload)
		s.emitRunScopedOptional(EventTodoSnapshotUpdated, state, payload)
		s.emitRuntimeSnapshotUpdated(ctx, state, "todo_updated")
		return
	}

	reason, _ := result.Metadata["reason_code"].(string)
	reason = strings.TrimSpace(reason)
	if reason == "" && execErr != nil {
		reason = strings.TrimSpace(execErr.Error())
	}
	if reason == "" {
		reason = strings.TrimSpace(result.ErrorClass)
	}
	if reason == "" {
		reason = "todo_write_failed"
	}
	payload.Reason = reason
	state.mu.Lock()
	hasFactsUpdate := false
	if state.factsCollector != nil {
		state.factsCollector.ApplyTodoSnapshot(runtimefacts.TodoSummaryLike{
			RequiredOpen:      payload.Summary.RequiredOpen,
			RequiredCompleted: payload.Summary.RequiredCompleted,
			RequiredFailed:    payload.Summary.RequiredFailed,
		})
		conflictIDs := extractTodoIDsFromPayload(payload.Items)
		state.factsCollector.ApplyTodoConflict(conflictIDs)
		hasFactsUpdate = true
	}
	state.mu.Unlock()
	if hasFactsUpdate {
		s.emitFactsUpdated(state, "todo_conflict")
	}
	s.emitRunScoped(ctx, EventTodoConflict, state, payload)
	s.emitRunScopedOptional(EventTodoSnapshotUpdated, state, payload)
	s.emitRuntimeSnapshotUpdated(ctx, state, "todo_conflict")
}

// hasSuccessfulWorkspaceWriteFact 判断工具结果是否产出了成功写入事实。
func hasSuccessfulWorkspaceWriteFact(result tools.ToolResult, execErr error) bool {
	if execErr != nil || result.IsError {
		return false
	}
	if toolResultNoopWrite(result.Metadata) {
		return false
	}
	return result.Facts.WorkspaceWrite
}

// toolResultNoopWrite 判断工具结果是否声明了 no-op 写入（内容未变化）。
func toolResultNoopWrite(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	raw, ok := metadata["noop_write"]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// toolResultFilePath 从工具结果 metadata 中取文件路径。
func toolResultFilePath(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	p, _ := metadata["path"].(string)
	return strings.TrimSpace(p)
}

// isFileWriteTool 判断工具调用是否为文件写入类工具，需在执行前后做 diff。
func isFileWriteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case tools.ToolNameFilesystemWriteFile,
		tools.ToolNameFilesystemEdit,
		tools.ToolNameFilesystemMoveFile,
		tools.ToolNameFilesystemCopyFile,
		tools.ToolNameFilesystemDeleteFile,
		tools.ToolNameFilesystemCreateDir,
		tools.ToolNameFilesystemRemoveDir:
		return true
	}
	return false
}

// toolCallTouchedPaths 从工具调用参数中提取所有可能被修改的工作区绝对路径。
// move/copy 同时返回 source 与 destination；其他写工具返回单个 path。
func toolCallTouchedPaths(call providertypes.ToolCall, workdir string) []string {
	args := strings.TrimSpace(call.Arguments)
	if args == "" {
		return nil
	}
	switch strings.TrimSpace(call.Name) {
	case tools.ToolNameFilesystemMoveFile, tools.ToolNameFilesystemCopyFile:
		var parsed struct {
			SourcePath      string `json:"source_path"`
			DestinationPath string `json:"destination_path"`
		}
		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			return nil
		}
		return resolveWorkdirPaths(workdir, parsed.SourcePath, parsed.DestinationPath)
	default:
		var parsed struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			return nil
		}
		return resolveWorkdirPaths(workdir, parsed.Path)
	}
}

// resolveWorkdirPaths 将多个相对/绝对路径解析为工作区绝对路径，丢弃空字符串。
func resolveWorkdirPaths(workdir string, raw ...string) []string {
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if isAbsolutePath(p) {
			out = append(out, toSlash(filepath.Clean(p)))
			continue
		}
		wd := strings.TrimSpace(workdir)
		if wd == "" {
			out = append(out, toSlash(filepath.Clean(p)))
			continue
		}
		out = append(out, toSlash(filepath.Clean(filepath.Join(wd, p))))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isAbsolutePath 判断路径是否为绝对路径，兼容 POSIX 风格（以 / 开头）和 Windows 风格。
func isAbsolutePath(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/")
}

// toSlash 统一路径分隔符为正斜杠，确保跨平台比较一致性。
func toSlash(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// bashCommandFromCall 从 bash 工具调用参数解析 command 字段，兼容 cmd 别名。
func bashCommandFromCall(call providertypes.ToolCall) string {
	args := strings.TrimSpace(call.Arguments)
	if args == "" {
		return ""
	}
	var parsed struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	if c := strings.TrimSpace(parsed.Command); c != "" {
		return c
	}
	return strings.TrimSpace(parsed.Cmd)
}

// collectUncoveredBashPaths 把 fingerprint 检测到的变更路径与启发式预捕获集合做差，
// 输出 EventBashSideEffect.UncoveredPaths 用于可观测性提醒。
func collectUncoveredBashPaths(workdir string, fpDiff checkpoint.FingerprintDiff, covered map[string]struct{}) []string {
	if len(fpDiff.Added) == 0 && len(fpDiff.Modified) == 0 {
		return nil
	}
	wd := strings.TrimSpace(workdir)
	seen := make(map[string]struct{})
	out := make([]string, 0)
	check := func(rel string) {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			return
		}
		var abs string
if isAbsolutePath(rel) {
abs = toSlash(filepath.Clean(rel))
} else if wd != "" {
abs = toSlash(filepath.Clean(filepath.Join(wd, rel)))
} else {
abs = toSlash(filepath.Clean(rel))
}
		if _, ok := covered[abs]; ok {
			return
		}
		if _, dup := seen[rel]; dup {
			return
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	for _, p := range fpDiff.Modified {
		check(p)
	}
	for _, p := range fpDiff.Added {
		check(p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// emitBashSideEffectEvent 派发 EventBashSideEffect，将 fingerprint 变化分类成 added/modified/deleted。
func (s *Service) emitBashSideEffectEvent(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	command string,
	fpDiff checkpoint.FingerprintDiff,
	preCaptured []string,
	uncovered []string,
) {
	changes := make([]FileChange, 0, len(fpDiff.Added)+len(fpDiff.Modified)+len(fpDiff.Deleted))
	for _, p := range fpDiff.Added {
		changes = append(changes, FileChange{Path: p, Kind: "added"})
	}
	for _, p := range fpDiff.Modified {
		changes = append(changes, FileChange{Path: p, Kind: "modified"})
	}
	for _, p := range fpDiff.Deleted {
		changes = append(changes, FileChange{Path: p, Kind: "deleted"})
	}
	if len(changes) == 0 {
		return
	}
	payload := BashSideEffectPayload{
		ToolCallID:                strings.TrimSpace(call.ID),
		Command:                   strings.TrimSpace(command),
		Changes:                   changes,
		PreemptivelyCapturedPaths: preCaptured,
		UncoveredPaths:            uncovered,
	}
	s.emitRunScoped(ctx, EventBashSideEffect, state, payload)
}

func summarizeHookResultContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= 256 {
		return trimmed
	}
	return trimmed[:256]
}

// extractTodoIDsFromPayload 提取 todo 事件快照中的条目 ID，用于冲突事实去重统计。
func extractTodoIDsFromPayload(items []TodoViewItem) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// buildTodoEventPayload 构建 todo 事件快照，确保 UI 可即时渲染结构化收敛信息。
func buildTodoEventPayload(state *runState, action string, reason string) TodoEventPayload {
	payload := TodoEventPayload{
		Action: strings.TrimSpace(action),
		Reason: strings.TrimSpace(reason),
	}
	if state == nil {
		return payload
	}

	state.mu.Lock()
	todos := cloneTodosForPersistence(state.session.Todos)
	state.mu.Unlock()
	snapshot := buildTodoSnapshotFromItems(todos)
	payload.Items = snapshot.Items
	payload.Summary = snapshot.Summary
	return payload
}

// emitAfterToolResultHook 在工具结果确定后触发 after_tool_result 挂点，仅提供只读摘要元信息。
func (s *Service) emitAfterToolResultHook(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	result tools.ToolResult,
	execErr error,
	workdir string,
) {
	afterToolHookMetadata := map[string]any{
		"tool_call_id":            strings.TrimSpace(call.ID),
		"tool_name":               strings.TrimSpace(call.Name),
		"is_error":                result.IsError,
		"error_class":             strings.TrimSpace(result.ErrorClass),
		"result_content_preview":  summarizeHookResultContent(result.Content),
		"result_metadata_present": len(result.Metadata) > 0,
		"workdir":                 strings.TrimSpace(workdir),
	}
	if execErr != nil {
		afterToolHookMetadata["execution_error"] = strings.TrimSpace(execErr.Error())
	}
	_ = s.runHookPoint(ctx, state, runtimehooks.HookPointAfterToolResult, runtimehooks.HookContext{
		Metadata: afterToolHookMetadata,
	})
}

// emitAfterToolFailureHook 在工具失败后触发 after_tool_failure 挂点，仅提供只读失败摘要。
func (s *Service) emitAfterToolFailureHook(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	result tools.ToolResult,
	execErr error,
	workdir string,
) {
	afterToolFailureMetadata := map[string]any{
		"tool_call_id": strings.TrimSpace(call.ID),
		"tool_name":    strings.TrimSpace(call.Name),
		"is_error":     result.IsError,
		"error_class":  strings.TrimSpace(result.ErrorClass),
		"workdir":      strings.TrimSpace(workdir),
	}
	if execErr != nil {
		afterToolFailureMetadata["execution_error"] = strings.TrimSpace(execErr.Error())
	}
	if preview := summarizeHookResultContent(result.Content); preview != "" {
		afterToolFailureMetadata["result_content_preview"] = preview
	}
	_ = s.runHookPoint(ctx, state, runtimehooks.HookPointAfterToolFailure, runtimehooks.HookContext{
		Metadata: afterToolFailureMetadata,
	})
}
