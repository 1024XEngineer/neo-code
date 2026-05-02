package facts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"neo-code/internal/tools"
)

// Collector 负责把分散的工具/子代理/待办状态信号收敛为统一事实层。
type Collector struct {
	facts RuntimeFacts
	seen  map[string]struct{}
}

// NewCollector 创建一个空事实收集器。
func NewCollector() *Collector {
	return &Collector{
		seen: make(map[string]struct{}),
	}
}

// Snapshot 返回当前事实快照的深拷贝。
func (c *Collector) Snapshot() RuntimeFacts {
	if c == nil {
		return RuntimeFacts{}
	}
	return cloneRuntimeFacts(c.facts)
}

// ApplyTodoSnapshot 将最新 todo 汇总写入事实层。
func (c *Collector) ApplyTodoSnapshot(summary TodoSummaryLike) {
	if c == nil {
		return
	}
	c.facts.Todos.OpenRequiredCount = maxInt(0, summary.RequiredOpen)
	c.facts.Todos.CompletedRequiredCount = maxInt(0, summary.RequiredCompleted)
	c.facts.Todos.FailedRequiredCount = maxInt(0, summary.RequiredFailed)
}

// ApplyTodoConflict 记录 todo 冲突事实，供终态决策识别重复不可恢复失败。
func (c *Collector) ApplyTodoConflict(todoIDs []string) {
	if c == nil {
		return
	}
	for _, id := range normalizeStringList(todoIDs) {
		if c.markSeen("todo_conflict:" + id) {
			c.facts.Todos.ConflictIDs = append(c.facts.Todos.ConflictIDs, id)
			c.facts.Progress.ObservedFactCount++
		}
	}
}

// ApplyToolResult 将工具结果解析为结构化事实。
func (c *Collector) ApplyToolResult(toolName string, result tools.ToolResult) {
	if c == nil {
		return
	}
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = strings.TrimSpace(result.Name)
	}
	if name == "" {
		return
	}
	normalizedName := strings.ToLower(name)
	if result.IsError {
		c.applyToolErrorFact(name, result)
	}

	switch normalizedName {
	case strings.ToLower(tools.ToolNameTodoWrite):
		if result.IsError {
			break
		}
		c.applyTodoToolFacts(result)
	case strings.ToLower(tools.ToolNameFilesystemWriteFile):
		if result.IsError {
			break
		}
		c.applyWriteFileFacts(result)
	case strings.ToLower(tools.ToolNameFilesystemReadFile):
		if result.IsError {
			break
		}
		c.applyReadFileFacts(result)
	case strings.ToLower(tools.ToolNameFilesystemEdit):
		if result.IsError {
			break
		}
		c.applyEditFileFacts(result)
	case strings.ToLower(tools.ToolNameFilesystemGlob):
		if result.IsError {
			break
		}
		c.applyGlobFacts(result)
	case strings.ToLower(tools.ToolNameBash):
		c.applyCommandFacts(name, result)
	case strings.ToLower(tools.ToolNameSpawnSubAgent):
		c.applySpawnSubAgentFacts(result)
	default:
		// 其他工具暂不扩展领域事实，避免噪声扩散。
	}

	c.applyVerificationFacts(name, result)
}

// applyToolErrorFact 记录工具错误事实，供终态决策识别持续失败模式。
func (c *Collector) applyToolErrorFact(toolName string, result tools.ToolResult) {
	errorClass := strings.TrimSpace(result.ErrorClass)
	if errorClass == "" {
		errorClass = "generic_error"
	}
	content := strings.TrimSpace(result.Content)
	if len(content) > 256 {
		content = content[:256]
	}
	key := fmt.Sprintf("tool_error:%s:%s", strings.ToLower(strings.TrimSpace(toolName)), errorClass)
	if !c.markSeen(key) {
		return
	}
	c.facts.Errors.ToolErrors = append(c.facts.Errors.ToolErrors, ToolErrorFact{
		Tool:       strings.TrimSpace(toolName),
		ErrorClass: errorClass,
		Content:    content,
	})
}

// ApplySubAgentStarted 记录子代理启动事实。
func (c *Collector) ApplySubAgentStarted(fact SubAgentFact) {
	if c == nil {
		return
	}
	fact.TaskID = strings.TrimSpace(fact.TaskID)
	if fact.TaskID == "" {
		return
	}
	key := fmt.Sprintf("subagent_started:%s:%s", fact.TaskID, strings.TrimSpace(fact.Role))
	if !c.markSeen(key) {
		return
	}
	c.facts.SubAgents.Started = append(c.facts.SubAgents.Started, fact)
	c.facts.Progress.ObservedFactCount++
}

// ApplySubAgentFinished 记录子代理完成/失败事实。
func (c *Collector) ApplySubAgentFinished(fact SubAgentFact, succeeded bool) {
	if c == nil {
		return
	}
	fact.TaskID = strings.TrimSpace(fact.TaskID)
	if fact.TaskID == "" {
		return
	}
	state := "failed"
	if succeeded {
		state = "completed"
	}
	key := fmt.Sprintf("subagent_%s:%s:%s", state, fact.TaskID, strings.TrimSpace(fact.StopReason))
	if !c.markSeen(key) {
		return
	}
	if succeeded {
		c.facts.SubAgents.Completed = append(c.facts.SubAgents.Completed, fact)
	} else {
		c.facts.SubAgents.Failed = append(c.facts.SubAgents.Failed, fact)
	}
	c.facts.Progress.ObservedFactCount++
}

// applyTodoToolFacts 从 todo_write 元数据提取 todo 状态事实。
func (c *Collector) applyTodoToolFacts(result tools.ToolResult) {
	stateFact, _ := result.Metadata["state_fact"].(string)
	stateFact = strings.TrimSpace(stateFact)
	if stateFact == "" {
		return
	}

	todoIDs := normalizeStringList(readTodoIDsFromMetadata(result.Metadata))
	if len(todoIDs) == 0 {
		if id, ok := readString(result.Metadata, "id"); ok {
			todoIDs = []string{id}
		}
	}
	if len(todoIDs) == 0 {
		todoIDs = []string{"unknown"}
	}
	for _, todoID := range todoIDs {
		key := "todo_state:" + stateFact + ":" + todoID
		if !c.markSeen(key) {
			continue
		}
		switch stateFact {
		case "todo_created":
			c.facts.Todos.CreatedIDs = append(c.facts.Todos.CreatedIDs, todoID)
		case "todo_updated":
			c.facts.Todos.UpdatedIDs = append(c.facts.Todos.UpdatedIDs, todoID)
		case "todo_completed":
			c.facts.Todos.CompletedIDs = append(c.facts.Todos.CompletedIDs, todoID)
		case "todo_failed":
			c.facts.Todos.FailedIDs = append(c.facts.Todos.FailedIDs, todoID)
		default:
			c.facts.Todos.UpdatedIDs = append(c.facts.Todos.UpdatedIDs, todoID)
		}
		c.facts.Progress.ObservedFactCount++
	}
}

// applyWriteFileFacts 从写文件工具结果提取 workspace write 事实。
func (c *Collector) applyWriteFileFacts(result tools.ToolResult) {
	path, ok := readString(result.Metadata, "path")
	if !ok {
		return
	}
	noopWrite := readBool(result.Metadata, "noop_write")
	if !noopWrite {
		bytes := readInt(result.Metadata, "bytes")
		key := fmt.Sprintf("file_written:%s:%d", path, bytes)
		if c.markSeen(key) {
			c.facts.Files.Written = append(c.facts.Files.Written, FileWriteFact{
				Path:            path,
				Bytes:           bytes,
				WorkspaceWrite:  true,
				ExpectedContent: readStringDefault(result.Metadata, "written_content"),
			})
			c.facts.Progress.ObservedFactCount++
		}
	}
	existsSource := "filesystem_write_file"
	if noopWrite {
		existsSource = "filesystem_write_file_noop"
	}
	existsKey := fmt.Sprintf("file_exists:write:%s:%s", existsSource, path)
	if c.markSeen(existsKey) {
		c.facts.Files.Exists = append(c.facts.Files.Exists, FileExistFact{Path: path, Source: existsSource})
		c.facts.Progress.ObservedFactCount++
	}
	if !result.Facts.VerificationPerformed {
		return
	}
	contentFact := FileContentMatchFact{
		Path:               path,
		Scope:              strings.TrimSpace(result.Facts.VerificationScope),
		ExpectedContains:   normalizeStringList(readStringSlice(result.Metadata, "verification_expected")),
		VerificationPassed: result.Facts.VerificationPassed,
	}
	status := "failed"
	if contentFact.VerificationPassed {
		status = "passed"
	}
	matchSource := "write"
	if noopWrite {
		matchSource = "noop_write"
	}
	matchKey := fmt.Sprintf("file_content_match:%s:%s:%s:%s", matchSource, path, contentFact.Scope, status)
	if !c.markSeen(matchKey) {
		return
	}
	c.facts.Files.ContentMatch = append(c.facts.Files.ContentMatch, contentFact)
	c.facts.Progress.ObservedFactCount++
}

// applyEditFileFacts 从编辑工具结果提取 workspace write 事实，保证 edit 任务可进入统一验收链路。
func (c *Collector) applyEditFileFacts(result tools.ToolResult) {
	path, ok := readString(result.Metadata, "path")
	if !ok {
		return
	}
	key := fmt.Sprintf("file_written:edit:%s", path)
	if !c.markSeen(key) {
		return
	}
	c.facts.Files.Written = append(c.facts.Files.Written, FileWriteFact{
		Path:           path,
		Bytes:          readInt(result.Metadata, "replacement_length"),
		WorkspaceWrite: true,
	})
	c.facts.Progress.ObservedFactCount++
}

// applyReadFileFacts 从 read_file 工具结果提取存在性和内容匹配事实。
func (c *Collector) applyReadFileFacts(result tools.ToolResult) {
	path, ok := readString(result.Metadata, "path")
	if !ok {
		return
	}
	if c.markSeen("file_exists:read:" + path) {
		c.facts.Files.Exists = append(c.facts.Files.Exists, FileExistFact{Path: path, Source: "filesystem_read_file"})
		c.facts.Progress.ObservedFactCount++
	}
	if !result.Facts.VerificationPerformed {
		return
	}
	fact := FileContentMatchFact{
		Path:               path,
		Scope:              strings.TrimSpace(result.Facts.VerificationScope),
		ExpectedContains:   normalizeStringList(readStringSlice(result.Metadata, "verification_expected")),
		VerificationPassed: result.Facts.VerificationPassed,
	}
	status := "failed"
	if fact.VerificationPassed {
		status = "passed"
	}
	key := fmt.Sprintf("file_content_match:%s:%s:%s", path, fact.Scope, status)
	if !c.markSeen(key) {
		return
	}
	c.facts.Files.ContentMatch = append(c.facts.Files.ContentMatch, fact)
	c.facts.Progress.ObservedFactCount++
}

// applyGlobFacts 从 glob 工具结果提取存在性与验证事实。
func (c *Collector) applyGlobFacts(result tools.ToolResult) {
	lines := normalizeStringList(strings.Split(result.Content, "\n"))
	for _, line := range lines {
		key := "file_exists:glob:" + line
		if !c.markSeen(key) {
			continue
		}
		c.facts.Files.Exists = append(c.facts.Files.Exists, FileExistFact{Path: line, Source: "filesystem_glob"})
		c.facts.Progress.ObservedFactCount++
	}
}

// applyCommandFacts 从 bash 工具结果提取命令执行事实。
func (c *Collector) applyCommandFacts(toolName string, result tools.ToolResult) {
	exitCode := readInt(result.Metadata, "exit_code")
	command, _ := readString(result.Metadata, "normalized_intent")
	if command == "" {
		command, _ = readString(result.Metadata, "command")
	}
	succeeded := readBool(result.Metadata, "ok") && exitCode == 0
	key := fmt.Sprintf("command:%s:%d:%t", command, exitCode, succeeded)
	if !c.markSeen(key) {
		return
	}
	c.facts.Commands.Executed = append(c.facts.Commands.Executed, CommandFact{
		Tool:      strings.TrimSpace(toolName),
		Command:   command,
		ExitCode:  exitCode,
		Succeeded: succeeded,
	})
	c.facts.Progress.ObservedFactCount++
}

// applySpawnSubAgentFacts 从 spawn_subagent 工具结果提取结构化子代理事实。
func (c *Collector) applySpawnSubAgentFacts(result tools.ToolResult) {
	taskID, ok := readString(result.Metadata, "task_id")
	if !ok {
		return
	}
	role, _ := readString(result.Metadata, "role")
	state, _ := readString(result.Metadata, "state")
	stopReason, _ := readString(result.Metadata, "stop_reason")
	summary := extractSubAgentSummary(result.Content)
	fact := SubAgentFact{
		TaskID:     taskID,
		Role:       role,
		StopReason: stopReason,
		Summary:    summary,
		Artifacts:  normalizeStringList(readStringSlice(result.Metadata, "artifacts")),
	}
	c.ApplySubAgentStarted(fact)
	if strings.EqualFold(state, "succeeded") {
		c.ApplySubAgentFinished(fact, true)
		return
	}
	if strings.EqualFold(state, "failed") || strings.EqualFold(state, "canceled") {
		c.ApplySubAgentFinished(fact, false)
	}
}

// applyVerificationFacts 把工具声明的验证事实收敛到统一验证集合。
func (c *Collector) applyVerificationFacts(toolName string, result tools.ToolResult) {
	if !result.Facts.VerificationPerformed {
		return
	}
	passed := result.Facts.VerificationPassed
	fact := VerificationFact{
		Tool:   strings.TrimSpace(toolName),
		Scope:  strings.TrimSpace(result.Facts.VerificationScope),
		Reason: strings.TrimSpace(readStringDefault(result.Metadata, "verification_reason")),
	}
	status := "failed"
	if passed {
		status = "passed"
	}
	key := fmt.Sprintf("verification:%s:%s:%s", fact.Tool, fact.Scope, status)
	if !c.markSeen(key) {
		return
	}
	c.facts.Verification.Performed = append(c.facts.Verification.Performed, fact)
	if passed {
		c.facts.Verification.Passed = append(c.facts.Verification.Passed, fact)
	} else {
		c.facts.Verification.Failed = append(c.facts.Verification.Failed, fact)
	}
	c.facts.Progress.ObservedFactCount++
}

// markSeen 记录去重键，返回该键是否首次出现。
func (c *Collector) markSeen(key string) bool {
	if c == nil {
		return false
	}
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return false
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, exists := c.seen[trimmed]; exists {
		return false
	}
	c.seen[trimmed] = struct{}{}
	return true
}

// extractSubAgentSummary 从 spawn_subagent 文本结果中提取 summary 段，避免全文进入事实层。
func extractSubAgentSummary(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "summary:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "summary:"))
		}
	}
	return ""
}

// readTodoIDsFromMetadata 从 todo 元数据中尽可能提取 todo id 列表。
func readTodoIDsFromMetadata(metadata map[string]any) []string {
	keys := []string{"todo_ids", "ids", "items"}
	for _, key := range keys {
		values := readStringSlice(metadata, key)
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

// readString 从 metadata 读取字符串并做 trim。
func readString(metadata map[string]any, key string) (string, bool) {
	if metadata == nil {
		return "", false
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return "", false
	}
	value := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if value == "" {
		return "", false
	}
	return value, true
}

// readStringDefault 从 metadata 读取字符串，失败时返回空串。
func readStringDefault(metadata map[string]any, key string) string {
	value, _ := readString(metadata, key)
	return value
}

// readInt 从 metadata 读取整数值，不可解析时返回 0。
func readInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return 0
}

// readBool 从 metadata 读取布尔值，不可解析时返回 false。
func readBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	raw, ok := metadata[key]
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

// readStringSlice 从 metadata 读取字符串数组并做去空白、去重处理。
func readStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return normalizeStringList(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text == "" {
				continue
			}
			values = append(values, text)
		}
		return normalizeStringList(values)
	default:
		return nil
	}
}

// normalizeStringList 对字符串列表执行 trim、去重和排序，保证事实输出稳定可测。
func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// cloneRuntimeFacts 深拷贝事实快照，避免调用方误改内部状态。
func cloneRuntimeFacts(in RuntimeFacts) RuntimeFacts {
	out := in
	out.Todos.CreatedIDs = append([]string(nil), in.Todos.CreatedIDs...)
	out.Todos.UpdatedIDs = append([]string(nil), in.Todos.UpdatedIDs...)
	out.Todos.CompletedIDs = append([]string(nil), in.Todos.CompletedIDs...)
	out.Todos.FailedIDs = append([]string(nil), in.Todos.FailedIDs...)
	out.Todos.ConflictIDs = append([]string(nil), in.Todos.ConflictIDs...)
	out.Files.Written = append([]FileWriteFact(nil), in.Files.Written...)
	out.Files.Exists = append([]FileExistFact(nil), in.Files.Exists...)
	out.Files.ContentMatch = append([]FileContentMatchFact(nil), in.Files.ContentMatch...)
	out.Commands.Executed = append([]CommandFact(nil), in.Commands.Executed...)
	out.SubAgents.Started = append([]SubAgentFact(nil), in.SubAgents.Started...)
	out.SubAgents.Completed = append([]SubAgentFact(nil), in.SubAgents.Completed...)
	out.SubAgents.Failed = append([]SubAgentFact(nil), in.SubAgents.Failed...)
	out.Verification.Performed = append([]VerificationFact(nil), in.Verification.Performed...)
	out.Verification.Passed = append([]VerificationFact(nil), in.Verification.Passed...)
	out.Verification.Failed = append([]VerificationFact(nil), in.Verification.Failed...)
	return out
}

// maxInt 返回两个整数中的较大值。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// TodoSummaryLike 提供对 todo summary 的最小字段依赖，避免 facts 包反向依赖 runtime 包。
type TodoSummaryLike struct {
	RequiredOpen      int
	RequiredCompleted int
	RequiredFailed    int
}
