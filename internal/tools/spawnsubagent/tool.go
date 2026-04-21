package spawnsubagent

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

const (
	maxSpawnArgumentsBytes = 64 * 1024
	maxSpawnItems          = 64
	maxSpawnTextLen        = 1024
	maxSpawnListItems      = 64

	spawnModeInline = "inline"
	spawnModeTodo   = "todo"
)

type spawnInput struct {
	Mode           string      `json:"mode"`
	Role           string      `json:"role"`
	ID             string      `json:"id"`
	Prompt         string      `json:"prompt"`
	Content        string      `json:"content"`
	ExpectedOutput string      `json:"expected_output"`
	MaxSteps       int         `json:"max_steps"`
	TimeoutSec     int         `json:"timeout_sec"`
	AllowedTools   []string    `json:"allowed_tools"`
	AllowedPaths   []string    `json:"allowed_paths"`
	Items          []spawnItem `json:"items"`
}

type spawnItem struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	Dependencies []string `json:"dependencies,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Acceptance   []string `json:"acceptance,omitempty"`
	RetryLimit   int      `json:"retry_limit,omitempty"`
}

// Tool 定义 spawn_subagent 工具：默认即时执行子代理；仅在 mode=todo 时写入 executor=subagent 的 Todo。
type Tool struct{}

// New 返回 spawn_subagent 工具实例。
func New() *Tool {
	return &Tool{}
}

// Name 返回工具唯一名称。
func (t *Tool) Name() string {
	return tools.ToolNameSpawnSubAgent
}

// Description 返回工具描述。
func (t *Tool) Description() string {
	return "Run subagent immediately by default; optionally create executor=subagent todos with mode=todo."
}

// Schema 返回 spawn_subagent 的参数定义，同时支持 inline 与 todo 两种模式。
func (t *Tool) Schema() map[string]any {
	itemSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type": "string",
			},
			"content": map[string]any{
				"type": "string",
			},
			"dependencies": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"priority": map[string]any{
				"type": "integer",
			},
			"acceptance": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"retry_limit": map[string]any{
				"type": "integer",
			},
		},
		"required": []string{"id", "content"},
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{
				"type": "string",
				"enum": []string{spawnModeInline, spawnModeTodo},
			},
			"role": map[string]any{
				"type": "string",
				"enum": []string{"researcher", "coder", "reviewer"},
			},
			"id": map[string]any{
				"type": "string",
			},
			"prompt": map[string]any{
				"type": "string",
			},
			"expected_output": map[string]any{
				"type": "string",
			},
			"max_steps": map[string]any{
				"type": "integer",
			},
			"timeout_sec": map[string]any{
				"type": "integer",
			},
			"allowed_tools": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"allowed_paths": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"items": map[string]any{
				"type":  "array",
				"items": itemSchema,
			},
		},
	}
}

// MicroCompactPolicy 声明 spawn_subagent 结果默认参与 micro compact。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

// Execute 解析入参后执行 inline 或 todo 模式。
func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	input, err := parseSpawnInput(call.Arguments)
	if err != nil {
		result := tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), err.Error(), nil)
		result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
		return result, err
	}

	switch resolveSpawnMode(input) {
	case spawnModeTodo:
		return t.executeTodoMode(call, input)
	default:
		return t.executeInlineMode(ctx, call, input)
	}
}

// executeInlineMode 调用 runtime 注入的 SubAgentInvoker，在主循环内即时执行子代理并回灌结果。
func (t *Tool) executeInlineMode(
	ctx context.Context,
	call tools.ToolCallInput,
	input spawnInput,
) (tools.ToolResult, error) {
	if call.SubAgentInvoker == nil {
		err := errors.New("spawn_subagent: subagent invoker is unavailable")
		result := tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil)
		result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
		return result, err
	}

	role := subagent.Role(input.Role)
	if !role.Valid() {
		role = subagent.RoleCoder
	}
	taskID := strings.TrimSpace(input.ID)
	if taskID == "" {
		taskID = defaultInlineTaskID(input.Prompt)
	}

	runResult, runErr := call.SubAgentInvoker.Run(ctx, tools.SubAgentRunInput{
		CallerAgent:           strings.TrimSpace(call.AgentID),
		ParentCapabilityToken: call.CapabilityToken,
		Role:                  role,
		TaskID:                taskID,
		Goal:                  strings.TrimSpace(input.Prompt),
		ExpectedOut:           strings.TrimSpace(input.ExpectedOutput),
		Workdir:               strings.TrimSpace(call.Workdir),
		MaxSteps:              input.MaxSteps,
		Timeout:               time.Duration(input.TimeoutSec) * time.Second,
		AllowedTools:          append([]string(nil), input.AllowedTools...),
		AllowedPaths:          append([]string(nil), input.AllowedPaths...),
	})

	isError := runErr != nil || runResult.State == subagent.StateFailed || runResult.State == subagent.StateCanceled
	result := tools.ToolResult{
		Name:    t.Name(),
		Content: renderInlineSpawnResult(runResult, runErr),
		IsError: isError,
		Metadata: map[string]any{
			"mode":         spawnModeInline,
			"task_id":      runResult.TaskID,
			"role":         string(runResult.Role),
			"state":        string(runResult.State),
			"stop_reason":  string(runResult.StopReason),
			"step_count":   runResult.StepCount,
			"error":        strings.TrimSpace(runResult.Error),
			"artifact_cnt": len(runResult.Output.Artifacts),
		},
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
	return result, runErr
}

// executeTodoMode 保留基于 Todo DAG 的写入模式（mode=todo）。
func (t *Tool) executeTodoMode(call tools.ToolCallInput, input spawnInput) (tools.ToolResult, error) {
	if call.SessionMutator == nil {
		err := errors.New("spawn_subagent: session mutator is unavailable")
		result := tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil)
		result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
		return result, err
	}

	ordered, err := resolveSpawnOrder(call.SessionMutator.ListTodos(), input.Items)
	if err != nil {
		result := tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), err.Error(), nil)
		result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
		return result, err
	}

	created := make([]string, 0, len(ordered))
	for _, item := range ordered {
		todo := agentsession.TodoItem{
			ID:           item.ID,
			Content:      item.Content,
			Status:       agentsession.TodoStatusPending,
			Dependencies: append([]string(nil), item.Dependencies...),
			Priority:     item.Priority,
			Executor:     agentsession.TodoExecutorSubAgent,
			Acceptance:   append([]string(nil), item.Acceptance...),
			RetryLimit:   item.RetryLimit,
		}
		if err := call.SessionMutator.AddTodo(todo); err != nil {
			result := tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), err.Error(), nil)
			result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
			return result, err
		}
		created = append(created, item.ID)
	}

	result := tools.ToolResult{
		Name:    t.Name(),
		Content: renderTodoSpawnResult(created),
		Metadata: map[string]any{
			"mode":          spawnModeTodo,
			"created_count": len(created),
			"created_ids":   created,
		},
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
	return result, nil
}

// parseSpawnInput 负责解析并校验 spawn_subagent 输入。
func parseSpawnInput(raw []byte) (spawnInput, error) {
	if len(raw) == 0 {
		return spawnInput{}, errors.New("spawn_subagent: arguments is empty")
	}
	if len(raw) > maxSpawnArgumentsBytes {
		return spawnInput{}, fmt.Errorf(
			"spawn_subagent: arguments payload exceeds %d bytes",
			maxSpawnArgumentsBytes,
		)
	}

	var input spawnInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return spawnInput{}, fmt.Errorf("spawn_subagent: parse arguments: %w", err)
	}
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.ID = strings.TrimSpace(input.ID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Content = strings.TrimSpace(input.Content)
	if input.Prompt == "" {
		input.Prompt = input.Content
	}
	input.ExpectedOutput = strings.TrimSpace(input.ExpectedOutput)
	input.AllowedTools = normalizeStringList(input.AllowedTools)
	input.AllowedPaths = normalizeStringList(input.AllowedPaths)
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if input.Role != "" {
		role := subagent.Role(input.Role)
		if !role.Valid() {
			return spawnInput{}, fmt.Errorf("spawn_subagent: unsupported role %q", input.Role)
		}
	}

	mode := resolveSpawnMode(input)
	if mode == "" {
		return spawnInput{}, errors.New("spawn_subagent: either prompt or items is required")
	}
	if mode != spawnModeInline && mode != spawnModeTodo {
		return spawnInput{}, fmt.Errorf("spawn_subagent: unsupported mode %q", input.Mode)
	}
	if input.Mode != "" && input.Mode != mode {
		return spawnInput{}, fmt.Errorf("spawn_subagent: unsupported mode %q", input.Mode)
	}
	input.Mode = mode

	switch mode {
	case spawnModeInline:
		return validateInlineInput(input)
	case spawnModeTodo:
		return validateTodoInput(input)
	default:
		return spawnInput{}, fmt.Errorf("spawn_subagent: unsupported mode %q", mode)
	}
}

// resolveSpawnMode 在未显式指定时，根据入参自动判定 inline/todo 模式。
func resolveSpawnMode(input spawnInput) string {
	if input.Mode != "" {
		return input.Mode
	}
	if len(input.Items) > 0 && strings.TrimSpace(input.Prompt) == "" {
		return spawnModeTodo
	}
	if strings.TrimSpace(input.Prompt) != "" {
		return spawnModeInline
	}
	return ""
}

// validateInlineInput 校验即时执行模式入参。
func validateInlineInput(input spawnInput) (spawnInput, error) {
	if strings.TrimSpace(input.Prompt) == "" {
		return spawnInput{}, errors.New("spawn_subagent: prompt is empty")
	}
	if len(input.Prompt) > maxSpawnTextLen {
		return spawnInput{}, fmt.Errorf("spawn_subagent: prompt exceeds max length %d", maxSpawnTextLen)
	}
	if len(input.ID) > maxSpawnTextLen {
		return spawnInput{}, fmt.Errorf("spawn_subagent: id exceeds max length %d", maxSpawnTextLen)
	}
	if len(input.ExpectedOutput) > maxSpawnTextLen {
		return spawnInput{}, fmt.Errorf("spawn_subagent: expected_output exceeds max length %d", maxSpawnTextLen)
	}
	if len(input.AllowedTools) > maxSpawnListItems {
		return spawnInput{}, fmt.Errorf("spawn_subagent: allowed_tools exceeds max items %d", maxSpawnListItems)
	}
	if len(input.AllowedPaths) > maxSpawnListItems {
		return spawnInput{}, fmt.Errorf("spawn_subagent: allowed_paths exceeds max items %d", maxSpawnListItems)
	}
	if input.MaxSteps < 0 {
		return spawnInput{}, errors.New("spawn_subagent: max_steps must be >= 0")
	}
	if input.TimeoutSec < 0 {
		return spawnInput{}, errors.New("spawn_subagent: timeout_sec must be >= 0")
	}
	return input, nil
}

// validateTodoInput 校验并规整 mode=todo 的任务列表。
func validateTodoInput(input spawnInput) (spawnInput, error) {
	if len(input.Items) == 0 {
		return spawnInput{}, errors.New("spawn_subagent: items is empty")
	}
	if len(input.Items) > maxSpawnItems {
		return spawnInput{}, fmt.Errorf("spawn_subagent: items exceeds max length %d", maxSpawnItems)
	}

	for idx := range input.Items {
		item := &input.Items[idx]
		item.ID = strings.TrimSpace(item.ID)
		item.Content = strings.TrimSpace(item.Content)
		item.Dependencies = normalizeStringList(item.Dependencies)
		item.Acceptance = normalizeStringList(item.Acceptance)
		if item.ID == "" {
			return spawnInput{}, fmt.Errorf("spawn_subagent: items[%d].id is empty", idx)
		}
		if item.Content == "" {
			return spawnInput{}, fmt.Errorf("spawn_subagent: items[%d].content is empty", idx)
		}
		if len(item.ID) > maxSpawnTextLen {
			return spawnInput{}, fmt.Errorf("spawn_subagent: items[%d].id exceeds max length %d", idx, maxSpawnTextLen)
		}
		if len(item.Content) > maxSpawnTextLen {
			return spawnInput{}, fmt.Errorf("spawn_subagent: items[%d].content exceeds max length %d", idx, maxSpawnTextLen)
		}
		if len(item.Dependencies) > maxSpawnListItems {
			return spawnInput{}, fmt.Errorf(
				"spawn_subagent: items[%d].dependencies exceeds max items %d",
				idx,
				maxSpawnListItems,
			)
		}
		if len(item.Acceptance) > maxSpawnListItems {
			return spawnInput{}, fmt.Errorf(
				"spawn_subagent: items[%d].acceptance exceeds max items %d",
				idx,
				maxSpawnListItems,
			)
		}
		for depIdx := range item.Dependencies {
			if len(item.Dependencies[depIdx]) > maxSpawnTextLen {
				return spawnInput{}, fmt.Errorf(
					"spawn_subagent: items[%d].dependencies[%d] exceeds max length %d",
					idx,
					depIdx,
					maxSpawnTextLen,
				)
			}
		}
		for accIdx := range item.Acceptance {
			if len(item.Acceptance[accIdx]) > maxSpawnTextLen {
				return spawnInput{}, fmt.Errorf(
					"spawn_subagent: items[%d].acceptance[%d] exceeds max length %d",
					idx,
					accIdx,
					maxSpawnTextLen,
				)
			}
		}
		if item.RetryLimit < 0 {
			return spawnInput{}, fmt.Errorf("spawn_subagent: items[%d].retry_limit must be >= 0", idx)
		}
	}
	return input, nil
}

// resolveSpawnOrder 在校验依赖可达后，返回可安全写入会话的拓扑有序任务列表。
func resolveSpawnOrder(existing []agentsession.TodoItem, items []spawnItem) ([]spawnItem, error) {
	existingSet := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		existingSet[item.ID] = struct{}{}
	}

	itemsByID := make(map[string]spawnItem, len(items))
	inDegree := make(map[string]int, len(items))
	dependents := make(map[string][]string, len(items))
	for _, item := range items {
		if _, exists := existingSet[item.ID]; exists {
			return nil, fmt.Errorf("spawn_subagent: todo %q already exists", item.ID)
		}
		if _, exists := itemsByID[item.ID]; exists {
			return nil, fmt.Errorf("spawn_subagent: duplicate todo id %q", item.ID)
		}
		itemsByID[item.ID] = item
		inDegree[item.ID] = 0
	}

	for _, item := range items {
		for _, depID := range item.Dependencies {
			if depID == item.ID {
				return nil, fmt.Errorf("spawn_subagent: todo %q cannot depend on itself", item.ID)
			}
			if _, exists := existingSet[depID]; exists {
				continue
			}
			if _, exists := itemsByID[depID]; !exists {
				return nil, fmt.Errorf("spawn_subagent: todo %q references unknown dependency %q", item.ID, depID)
			}
			inDegree[item.ID]++
			dependents[depID] = append(dependents[depID], item.ID)
		}
	}

	ready := make([]string, 0, len(items))
	for id, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ordered := make([]spawnItem, 0, len(items))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, itemsByID[id])

		next := dependents[id]
		sort.Strings(next)
		for _, depID := range next {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				ready = append(ready, depID)
			}
		}
		sort.Strings(ready)
	}

	if len(ordered) != len(items) {
		return nil, errors.New("spawn_subagent: cyclic dependencies detected")
	}
	return ordered, nil
}

// normalizeStringList 统一清理字符串列表并去重，保持输入顺序稳定。
func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// defaultInlineTaskID 为 inline 模式生成稳定 task id，避免空 id 导致审计不可读。
func defaultInlineTaskID(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "spawn-subagent-inline"
	}
	sum := sha1.Sum([]byte(trimmed))
	return "spawn-inline-" + hex.EncodeToString(sum[:4])
}

// renderTodoSpawnResult 输出 mode=todo 的创建摘要。
func renderTodoSpawnResult(created []string) string {
	lines := []string{
		"spawn_subagent result",
		fmt.Sprintf("mode: %s", spawnModeTodo),
		fmt.Sprintf("created_count: %d", len(created)),
	}
	if len(created) == 0 {
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "created_ids:")
	for _, id := range created {
		lines = append(lines, "- "+id)
	}
	return strings.Join(lines, "\n")
}

// renderInlineSpawnResult 输出 inline 模式的即时执行结果。
func renderInlineSpawnResult(result tools.SubAgentRunResult, runErr error) string {
	lines := []string{
		"spawn_subagent result",
		fmt.Sprintf("mode: %s", spawnModeInline),
		"task_id: " + strings.TrimSpace(result.TaskID),
		"role: " + strings.TrimSpace(string(result.Role)),
		"state: " + strings.TrimSpace(string(result.State)),
		"stop_reason: " + strings.TrimSpace(string(result.StopReason)),
		fmt.Sprintf("step_count: %d", result.StepCount),
	}
	if text := strings.TrimSpace(result.Output.Summary); text != "" {
		lines = append(lines, "summary: "+text)
	}
	if len(result.Output.Findings) > 0 {
		lines = append(lines, "findings:")
		for _, finding := range result.Output.Findings {
			lines = append(lines, "- "+finding)
		}
	}
	if len(result.Output.Artifacts) > 0 {
		lines = append(lines, "artifacts:")
		for _, artifact := range result.Output.Artifacts {
			lines = append(lines, "- "+artifact)
		}
	}
	errText := strings.TrimSpace(result.Error)
	if errText == "" && runErr != nil {
		errText = strings.TrimSpace(runErr.Error())
	}
	if errText != "" {
		lines = append(lines, "error: "+errText)
	}
	return strings.Join(lines, "\n")
}
