package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	providertypes "neo-code/internal/provider/types"
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

	if isSuccessfulRememberToolCall(call.Name, result, execErr) {
		state.mu.Lock()
		state.rememberedThisRun = true
		state.mu.Unlock()
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
	if execErr == nil {
		s.emitRunScoped(ctx, EventTodoUpdated, state, TodoEventPayload{Action: action})
		return
	}

	reason, _ := result.Metadata["reason_code"].(string)
	if strings.Contains(strings.ToLower(strings.TrimSpace(reason)), "conflict") {
		s.emitRunScoped(ctx, EventTodoConflict, state, TodoEventPayload{Action: action, Reason: reason})
	}
}

// hasSuccessfulWorkspaceWriteFact 判断工具结果是否产出了成功写入事实。
func hasSuccessfulWorkspaceWriteFact(result tools.ToolResult, execErr error) bool {
	if execErr != nil || result.IsError {
		return false
	}
	return result.Facts.WorkspaceWrite
}

func summarizeHookResultContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= 256 {
		return trimmed
	}
	return trimmed[:256]
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
