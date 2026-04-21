package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/partsrender"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/streaming"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

const (
	subAgentMaxStepTurnsDefault = 6
	// subAgentToolResultMaxRunes 定义子代理工具回灌给模型的更小文本上限，避免沿用全局 64KB。
	subAgentToolResultMaxRunes = 4 * 1024
	// subAgentMessageWindowMaxMessages 定义子代理单步内携带的最大消息条数窗口。
	subAgentMessageWindowMaxMessages = 18
	// subAgentMessageWindowMaxRunes 定义子代理单步内可携带的历史消息文本总量上限。
	subAgentMessageWindowMaxRunes = 12 * 1024
	// subAgentPinnedMessageMaxRunes 定义首条任务消息允许保留的最大文本长度。
	subAgentPinnedMessageMaxRunes = 3 * 1024
	// subAgentHistorySummaryReserveRunes 预留滚动摘要消息的预算，避免挤占最近窗口。
	subAgentHistorySummaryReserveRunes = 256
	// subAgentTextTruncatedSuffix 为子代理文本截断后附加标识。
	subAgentTextTruncatedSuffix = "\n...[truncated]"
)

var errSubAgentRuntimeUnavailable = errors.New("runtime: subagent runtime dependencies unavailable")

var subAgentOutputRequiredKeys = []string{
	"summary",
	"findings",
	"patches",
	"risks",
	"next_actions",
	"artifacts",
}

// runtimeSubAgentEngine 提供基于 runtime provider + tools 的子代理执行引擎。
type runtimeSubAgentEngine struct {
	service *Service
	role    subagent.Role
	policy  subagent.RolePolicy
}

// newRuntimeSubAgentEngineBuilder 创建绑定 Service 的子代理引擎构建器。
func newRuntimeSubAgentEngineBuilder(service *Service) subagent.EngineBuilder {
	return func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		return runtimeSubAgentEngine{
			service: service,
			role:    role,
			policy:  policy,
		}
	}
}

// RunStep 执行子代理单步推理，并在单步内完成工具调用闭环。
func (e runtimeSubAgentEngine) RunStep(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
	if err := ctx.Err(); err != nil {
		return subagent.StepOutput{}, err
	}

	runtimeConfig, model, toolTimeout, err := e.resolveSettings()
	if err != nil {
		if errors.Is(err, errSubAgentRuntimeUnavailable) {
			return subagent.StepOutput{}, err
		}
		return subagent.StepOutput{}, err
	}
	if input.Executor == nil {
		return subagent.StepOutput{}, errors.New("runtime: subagent tool executor is nil")
	}
	modelProvider, err := e.buildProvider(ctx, runtimeConfig)
	if err != nil {
		return subagent.StepOutput{}, err
	}

	allowedTools := resolveAllowedTools(input)
	allowedPaths := resolveAllowedPaths(input)
	toolSpecs, err := input.Executor.ListToolSpecs(ctx, subagent.ToolSpecListInput{
		SessionID:    input.SessionID,
		Role:         input.Role,
		AllowedTools: allowedTools,
	})
	if err != nil {
		return subagent.StepOutput{}, fmt.Errorf("runtime: list subagent tool specs: %w", err)
	}
	if input.Policy.ToolUseMode == subagent.ToolUseModeDisabled {
		toolSpecs = nil
	}

	systemPrompt := buildSubAgentSystemPrompt(input.Policy, allowedTools, allowedPaths)
	messages := buildSubAgentInitialMessages(input)
	totalToolCalls := 0
	maxTurns := resolveSubAgentMaxTurns(input.Policy.DefaultBudget.MaxSteps)

	for turn := 1; turn <= maxTurns; turn++ {
		messages = trimSubAgentMessageWindow(messages)
		outcome, err := e.generateStepMessage(ctx, modelProvider, model, systemPrompt, messages, toolSpecs)
		if err != nil {
			return subagent.StepOutput{}, err
		}
		assistant := outcome.message
		assistant = ensureAssistantRole(assistant)
		if !assistant.IsEmpty() {
			messages = append(messages, assistant)
		}

		if len(assistant.ToolCalls) == 0 {
			if input.Policy.ToolUseMode == subagent.ToolUseModeRequired && totalToolCalls == 0 {
				return subagent.StepOutput{}, errors.New("runtime: subagent policy requires at least one tool call")
			}
			output, err := parseSubAgentOutput(renderAssistantText(assistant))
			if err != nil {
				return subagent.StepOutput{}, err
			}
			return subagent.StepOutput{
				Delta:  fmt.Sprintf("subagent completed with %d tool call(s)", totalToolCalls),
				Done:   true,
				Output: output,
			}, nil
		}

		if input.Policy.ToolUseMode == subagent.ToolUseModeDisabled {
			return subagent.StepOutput{}, errors.New("runtime: subagent tool_use_mode is disabled but model requested tool calls")
		}

		remainingCalls := effectiveMaxToolCallsPerStep(input.Policy.MaxToolCallsPerStep) - totalToolCalls
		if remainingCalls <= 0 {
			return subagent.StepOutput{}, errors.New("runtime: subagent max_tool_calls_per_step exceeded")
		}
		if len(assistant.ToolCalls) > remainingCalls {
			return subagent.StepOutput{}, errors.New("runtime: subagent tool call batch exceeds remaining budget")
		}

		nextMessages, executedCalls, err := executeSubAgentToolCallBatch(ctx, e.service, input, assistant.ToolCalls, toolTimeout)
		if err != nil {
			return subagent.StepOutput{}, err
		}
		messages = append(messages, nextMessages...)
		totalToolCalls += executedCalls
	}
	return subagent.StepOutput{}, fmt.Errorf("runtime: subagent step exceeded max turns (%d)", maxTurns)
}

// ensureAssistantRole 确保模型输出消息具备 assistant 角色，避免后续流程依赖空角色值。
func ensureAssistantRole(message providertypes.Message) providertypes.Message {
	if strings.TrimSpace(message.Role) == "" {
		message.Role = providertypes.RoleAssistant
	}
	return message
}

// resolveSettings 读取当前 runtime 配置，并解析子代理调用 provider 所需参数。
func (e runtimeSubAgentEngine) resolveSettings() (config.ResolvedProviderConfig, string, time.Duration, error) {
	if e.service == nil || e.service.configManager == nil {
		return config.ResolvedProviderConfig{}, "", 0, runtimeUnavailableError("service or config manager is nil")
	}
	if e.service.providerFactory == nil {
		return config.ResolvedProviderConfig{}, "", 0, runtimeUnavailableError("provider factory is nil")
	}
	cfg := e.service.configManager.Get()
	resolvedProvider, err := config.ResolveSelectedProvider(cfg)
	if err != nil {
		return config.ResolvedProviderConfig{}, "", 0, fmt.Errorf("runtime: resolve selected provider: %w", err)
	}
	model := strings.TrimSpace(cfg.CurrentModel)
	if model == "" {
		model = strings.TrimSpace(resolvedProvider.Model)
	}
	if model == "" {
		return config.ResolvedProviderConfig{}, "", 0, errors.New("runtime: subagent model is empty")
	}
	timeout := time.Duration(cfg.ToolTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultSubAgentToolTimeout
	}
	return resolvedProvider, model, timeout, nil
}

// runtimeUnavailableError 封装 runtime 依赖缺失错误，保持错误类型与消息结构一致。
func runtimeUnavailableError(detail string) error {
	return fmt.Errorf("%w: %s", errSubAgentRuntimeUnavailable, strings.TrimSpace(detail))
}

// buildProvider 基于解析后的 provider 配置创建单步内复用的模型实例。
func (e runtimeSubAgentEngine) buildProvider(
	ctx context.Context,
	resolvedProvider config.ResolvedProviderConfig,
) (provider.Provider, error) {
	runtimeConfig, err := resolvedProvider.ToRuntimeConfig()
	if err != nil {
		return nil, fmt.Errorf("runtime: normalize subagent provider config: %w", err)
	}
	modelProvider, err := e.service.providerFactory.Build(ctx, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("runtime: build subagent provider: %w", err)
	}
	return modelProvider, nil
}

// generateStepMessage 发起一次 provider 调用并返回本轮 assistant 输出。
func (e runtimeSubAgentEngine) generateStepMessage(
	ctx context.Context,
	modelProvider provider.Provider,
	model string,
	systemPrompt string,
	messages []providertypes.Message,
	toolSpecs []providertypes.ToolSpec,
) (streamGenerateResult, error) {
	outcome := generateStreamingMessage(ctx, modelProvider, providertypes.GenerateRequest{
		Model:        model,
		SystemPrompt: systemPrompt,
		Messages:     messages,
		Tools:        toolSpecs,
	}, streamingHooksForSubAgent())
	if outcome.err != nil {
		return streamGenerateResult{}, fmt.Errorf("runtime: subagent provider generate: %w", outcome.err)
	}
	return outcome, nil
}

// executeSubAgentToolCallBatch 顺序执行本轮工具调用并转换为后续模型输入消息。
// 返回值中的计数仅统计真正执行成功且被允许的工具调用次数。
func executeSubAgentToolCallBatch(
	ctx context.Context,
	service *Service,
	stepInput subagent.StepInput,
	calls []providertypes.ToolCall,
	toolTimeout time.Duration,
) ([]providertypes.Message, int, error) {
	if len(calls) == 0 {
		return nil, 0, nil
	}
	allowedTools := normalizeToolAllowlist(resolveAllowedTools(stepInput))
	results := make([]providertypes.Message, 0, len(calls))
	executed := 0

	for index, call := range calls {
		if err := ctx.Err(); err != nil {
			return results, executed, err
		}
		normalizedCall := normalizeSubAgentToolCall(call, index)
		if !toolAllowed(allowedTools, normalizedCall.Name) {
			denied := subagentCapabilityDeniedResult(stepInput, normalizedCall)
			results = append(results, denied)
			emitCapabilityDeniedEvent(ctx, service, stepInput, normalizedCall.Name)
			continue
		}

		execResult, execErr := stepInput.Executor.ExecuteTool(ctx, subagent.ToolExecutionInput{
			RunID:           stepInput.RunID,
			SessionID:       stepInput.SessionID,
			TaskID:          stepInput.Task.ID,
			Role:            stepInput.Role,
			AgentID:         stepInput.AgentID,
			Workdir:         stepInput.Workdir,
			Timeout:         toolTimeout,
			Call:            normalizedCall,
			CapabilityToken: nil,
			Capability:      stepInput.Capability,
		})
		message := subAgentToolResultToMessage(normalizedCall, execResult)
		if execErr != nil && strings.TrimSpace(message.Parts[0].Text) == "" {
			message.Parts[0] = providertypes.NewTextPart(strings.TrimSpace(execErr.Error()))
		}
		if execErr != nil && !isRecoverableSubAgentToolError(execErr) {
			return results, executed, execErr
		}
		results = append(results, message)
		if execErr == nil {
			executed++
		}
	}
	return results, executed, nil
}

// buildSubAgentInitialMessages 组装子代理首轮输入消息，包含任务、上下文切片与历史 trace。
func buildSubAgentInitialMessages(input subagent.StepInput) []providertypes.Message {
	lines := []string{
		"task_id: " + strings.TrimSpace(input.Task.ID),
		"goal: " + strings.TrimSpace(input.Task.Goal),
	}
	if expected := strings.TrimSpace(input.Task.ExpectedOutput); expected != "" {
		lines = append(lines, "expected_output: "+expected)
	}
	if workdir := strings.TrimSpace(input.Workdir); workdir != "" {
		lines = append(lines, "workdir: "+workdir)
	}
	if allowedTools := resolveAllowedTools(input); len(allowedTools) > 0 {
		lines = append(lines, "allowed_tools: "+strings.Join(allowedTools, ", "))
	}
	if allowedPaths := resolveAllowedPaths(input); len(allowedPaths) > 0 {
		lines = append(lines, "allowed_paths:")
		for _, allowedPath := range allowedPaths {
			lines = append(lines, "- "+allowedPath)
		}
	}
	if renderedSlice := strings.TrimSpace(input.Task.ContextSlice.Render()); renderedSlice != "" {
		lines = append(lines, "", "context_slice:", renderedSlice)
	}
	if len(input.Trace) > 0 {
		lines = append(lines, "", "recent_trace:")
		for _, item := range input.Trace {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			lines = append(lines, "- "+trimmed)
		}
	}
	content, _ := truncateSubAgentText(strings.Join(lines, "\n"), subAgentPinnedMessageMaxRunes)
	return []providertypes.Message{{
		Role:  providertypes.RoleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(content)},
	}}
}

// buildSubAgentSystemPrompt 构建子代理策略提示词，约束工具决策、能力边界与输出契约。
func buildSubAgentSystemPrompt(policy subagent.RolePolicy, allowedTools []string, allowedPaths []string) string {
	maxToolCallsPerStep := effectiveMaxToolCallsPerStep(policy.MaxToolCallsPerStep)
	lines := []string{strings.TrimSpace(policy.SystemPrompt)}
	lines = append(lines,
		"你是子代理执行引擎的一部分，必须根据任务目标自主决定是否调用工具。",
		"当需要外部事实、文件状态或命令执行结果时必须调用工具；纯推理可直接完成。",
		"工具能力边界由 runtime 安全层强制执行，越权调用会收到 denied/tool error 结果，不允许绕过。",
		"如需文件访问，只能访问 allowed_paths 范围内路径；如需工具调用，只能使用 allowed_tools 列表。",
		"若父代理通过 spawn_subagent(mode=todo) 创建任务，你只处理当前 task，不直接驱动 todo 状态机。",
		"工具失败后优先换参数或换工具，若仍失败则在输出中明确风险与后续动作。",
		"最终输出必须是 JSON 对象，且必须包含键：summary, findings, patches, risks, next_actions, artifacts。",
		"字段类型约束：summary(string)、findings/patches/risks/next_actions/artifacts(string数组)。",
		"输出时只返回单个 JSON 对象，不要附加 Markdown 代码块、解释性前后缀或额外文本。",
		"该 JSON 将被 runtime 直接解析并回传父代理，任何非 JSON 噪声都可能导致任务失败。",
		fmt.Sprintf("tool_use_mode: %s", policy.ToolUseMode),
		fmt.Sprintf("max_tool_calls_per_step: %d", maxToolCallsPerStep),
	)
	if len(allowedTools) > 0 {
		lines = append(lines, "allowed_tools: "+strings.Join(allowedTools, ", "))
	} else {
		lines = append(lines, "allowed_tools: (none)")
	}
	if len(allowedPaths) > 0 {
		lines = append(lines, "allowed_paths:")
		for _, allowedPath := range allowedPaths {
			lines = append(lines, "- "+allowedPath)
		}
	} else {
		lines = append(lines, "allowed_paths: (none)")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// resolveAllowedTools 计算本步可用工具集合，优先使用 capability，回退到 policy。
func resolveAllowedTools(input subagent.StepInput) []string {
	if len(input.Capability.AllowedTools) > 0 {
		return append([]string(nil), input.Capability.AllowedTools...)
	}
	return append([]string(nil), input.Policy.AllowedTools...)
}

// resolveAllowedPaths 返回子代理当前步可访问的路径边界列表。
func resolveAllowedPaths(input subagent.StepInput) []string {
	if len(input.Capability.AllowedPaths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input.Capability.AllowedPaths))
	paths := make([]string, 0, len(input.Capability.AllowedPaths))
	for _, item := range input.Capability.AllowedPaths {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		paths = append(paths, trimmed)
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}

// resolveSubAgentMaxTurns 统一解析子代理单步内部最多可迭代的模型轮次。
func resolveSubAgentMaxTurns(maxSteps int) int {
	if maxSteps <= 0 {
		return subAgentMaxStepTurnsDefault
	}
	return maxSteps
}

// effectiveMaxToolCallsPerStep 解析每步工具调用上限，非法或未配置值按 0 处理（即不允许调用）。
func effectiveMaxToolCallsPerStep(limit int) int {
	if limit <= 0 {
		return 0
	}
	return limit
}

// parseSubAgentOutput 从 assistant 文本中提取并解析结构化 JSON 输出。
func parseSubAgentOutput(text string) (subagent.Output, error) {
	jsonText, err := extractSubAgentJSONObject(text)
	if err != nil {
		return subagent.Output{}, err
	}
	payload, err := parseSubAgentOutputPayload(jsonText)
	if err != nil {
		return subagent.Output{}, err
	}
	return payload, nil
}

// extractSubAgentJSONObject 从文本中提取最可能的输出 JSON，优先选择包含输出契约字段的对象。
func extractSubAgentJSONObject(text string) (string, error) {
	depth := 0
	inString := false
	escaped := false
	start := -1
	lastObject := ""
	contractObject := ""
	for index := 0; index < len(text); index++ {
		ch := text[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = index
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				candidate := strings.TrimSpace(text[start : index+1])
				lastObject = candidate
				if matchesSubAgentOutputContract(candidate) {
					contractObject = candidate
				}
				start = -1
			}
		}
	}
	if contractObject != "" {
		return contractObject, nil
	}
	if lastObject != "" {
		return "", errors.New("runtime: subagent output json object missing required contract keys")
	}
	if strings.Contains(text, "{") {
		return "", errors.New("runtime: subagent output contains incomplete json object")
	}
	return "", errors.New("runtime: subagent output does not contain json object")
}

// parseSubAgentOutputPayload 按严格契约解析输出字段，要求必需键存在且类型匹配。
func parseSubAgentOutputPayload(jsonText string) (subagent.Output, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonText), &payload); err != nil {
		return subagent.Output{}, fmt.Errorf("runtime: parse subagent output json: %w", err)
	}
	for _, key := range subAgentOutputRequiredKeys {
		if _, ok := payload[key]; !ok {
			return subagent.Output{}, fmt.Errorf("runtime: subagent output missing required key %q", key)
		}
	}

	var output subagent.Output
	if err := decodeSubAgentOutputString(payload, "summary", &output.Summary); err != nil {
		return subagent.Output{}, err
	}
	output.Summary = strings.TrimSpace(output.Summary)
	if err := decodeSubAgentOutputStringList(payload, "findings", &output.Findings); err != nil {
		return subagent.Output{}, err
	}
	if err := decodeSubAgentOutputStringList(payload, "patches", &output.Patches); err != nil {
		return subagent.Output{}, err
	}
	if err := decodeSubAgentOutputStringList(payload, "risks", &output.Risks); err != nil {
		return subagent.Output{}, err
	}
	if err := decodeSubAgentOutputStringList(payload, "next_actions", &output.NextActions); err != nil {
		return subagent.Output{}, err
	}
	if err := decodeSubAgentOutputStringList(payload, "artifacts", &output.Artifacts); err != nil {
		return subagent.Output{}, err
	}
	return output, nil
}

// decodeSubAgentOutputString 按键解析字符串字段并保留统一错误前缀。
func decodeSubAgentOutputString(payload map[string]json.RawMessage, key string, target *string) error {
	if err := json.Unmarshal(payload[key], target); err != nil {
		return fmt.Errorf("runtime: subagent output key %q must be string: %w", key, err)
	}
	return nil
}

// decodeSubAgentOutputStringList 按键解析字符串数组字段并保留统一错误前缀。
func decodeSubAgentOutputStringList(payload map[string]json.RawMessage, key string, target *[]string) error {
	var values []string
	if err := json.Unmarshal(payload[key], &values); err != nil {
		return fmt.Errorf("runtime: subagent output key %q must be []string: %w", key, err)
	}
	*target = values
	return nil
}

// matchesSubAgentOutputContract 判断 JSON 文本是否包含子代理输出契约必需字段。
func matchesSubAgentOutputContract(text string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return false
	}
	for _, key := range subAgentOutputRequiredKeys {
		if _, ok := payload[key]; !ok {
			return false
		}
	}
	return true
}

// renderAssistantText 将 assistant parts 渲染为统一文本，用于 JSON 输出解析。
func renderAssistantText(message providertypes.Message) string {
	return strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts))
}

// normalizeSubAgentToolCall 规整工具调用基础字段，并补齐空 call id。
func normalizeSubAgentToolCall(call providertypes.ToolCall, index int) providertypes.ToolCall {
	normalized := call
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Arguments = strings.TrimSpace(normalized.Arguments)
	normalized.ID = strings.TrimSpace(normalized.ID)
	if normalized.ID == "" {
		normalized.ID = fmt.Sprintf("subagent-call-%d", index+1)
	}
	return normalized
}

// normalizeToolAllowlist 规整工具白名单，便于大小写无关匹配。
func normalizeToolAllowlist(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

// toolAllowed 判断工具名是否在 allowlist 内；allowlist 为空时默认拒绝。
func toolAllowed(allowlist map[string]struct{}, toolName string) bool {
	if len(allowlist) == 0 {
		return false
	}
	_, ok := allowlist[strings.ToLower(strings.TrimSpace(toolName))]
	return ok
}

// isRecoverableSubAgentToolError 判断工具调用错误是否可回灌给模型继续推理。
func isRecoverableSubAgentToolError(err error) bool {
	if err == nil {
		return true
	}
	var permissionErr *tools.PermissionDecisionError
	if errors.As(err, &permissionErr) {
		return true
	}
	return isSubAgentPermissionDeniedError(err)
}

// isSubAgentPermissionDeniedError 判断错误是否属于权限拒绝语义（含 ask->reject 的文本错误）。
func isSubAgentPermissionDeniedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, tools.ErrPermissionDenied) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(err.Error()), permissionRejectedErrorMessage)
}

// subagentCapabilityDeniedResult 构造 capability 越权时回灌给模型的标准 tool 消息。
func subagentCapabilityDeniedResult(input subagent.StepInput, call providertypes.ToolCall) providertypes.Message {
	content := tools.FormatError(call.Name, "capability denied", "tool is not allowed in current subagent capability")
	return providertypes.Message{
		Role:         providertypes.RoleTool,
		ToolCallID:   call.ID,
		Parts:        []providertypes.ContentPart{providertypes.NewTextPart(content)},
		IsError:      true,
		ToolMetadata: map[string]string{"tool_name": call.Name, "decision": permissionDecisionDeny, "task_id": input.Task.ID},
	}
}

// emitCapabilityDeniedEvent 发射 capability 越权拒绝事件。
func emitCapabilityDeniedEvent(ctx context.Context, service *Service, input subagent.StepInput, toolName string) {
	if service == nil {
		return
	}
	_ = service.emit(ctx, EventSubAgentToolCallDenied, input.RunID, input.SessionID, SubAgentToolCallEventPayload{
		Role:      input.Role,
		TaskID:    input.Task.ID,
		ToolName:  strings.TrimSpace(toolName),
		Decision:  permissionDecisionDeny,
		ElapsedMS: 0,
		Truncated: false,
		Error:     "capability denied",
	})
}

// subAgentToolResultToMessage 把工具执行结果转换为 provider 可消费的 tool 消息。
func subAgentToolResultToMessage(call providertypes.ToolCall, result subagent.ToolExecutionResult) providertypes.Message {
	name := strings.TrimSpace(result.Name)
	if name == "" {
		name = strings.TrimSpace(call.Name)
	}
	content, contentTruncated := truncateSubAgentText(strings.TrimSpace(result.Content), subAgentToolResultMaxRunes)
	metadata := map[string]any{
		"tool_name": name,
		"decision":  strings.TrimSpace(result.Decision),
	}
	for key, value := range result.Metadata {
		metadata[key] = value
	}
	if contentTruncated {
		metadata["truncated"] = true
	}
	return providertypes.Message{
		Role:         providertypes.RoleTool,
		ToolCallID:   call.ID,
		Parts:        []providertypes.ContentPart{providertypes.NewTextPart(content)},
		IsError:      result.IsError,
		ToolMetadata: tools.SanitizeToolMetadata(name, metadata),
	}
}

// trimSubAgentMessageWindow 对子代理对话历史执行滚动裁剪，保留首条任务上下文与最近窗口，避免消息无限累加。
func trimSubAgentMessageWindow(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}
	if len(messages) <= subAgentMessageWindowMaxMessages && estimateSubAgentMessagesRunes(messages) <= subAgentMessageWindowMaxRunes {
		return messages
	}

	pinned := clampSubAgentPinnedMessage(messages[0], subAgentPinnedMessageMaxRunes)
	history := messages[1:]
	if len(history) == 0 {
		return []providertypes.Message{pinned}
	}

	availableRunes := subAgentMessageWindowMaxRunes - estimateSubAgentMessageRunes(pinned) - subAgentHistorySummaryReserveRunes
	if availableRunes < 0 {
		availableRunes = 0
	}
	maxRecentMessages := subAgentMessageWindowMaxMessages - 2
	if maxRecentMessages < 1 {
		maxRecentMessages = 1
	}

	selectedReversed := make([]providertypes.Message, 0, minInt(len(history), maxRecentMessages))
	selectedRunes := 0
	droppedCount := len(history)
	droppedRunes := estimateSubAgentMessagesRunes(history)

	for idx := len(history) - 1; idx >= 0; idx-- {
		msg := history[idx]
		msgRunes := estimateSubAgentMessageRunes(msg)
		if len(selectedReversed) >= maxRecentMessages || selectedRunes+msgRunes > availableRunes {
			break
		}
		selectedReversed = append(selectedReversed, msg)
		selectedRunes += msgRunes
		droppedCount = idx
		droppedRunes -= msgRunes
	}

	if len(selectedReversed) == 0 {
		latest := history[len(history)-1]
		selectedReversed = append(selectedReversed, latest)
		droppedCount = len(history) - 1
		droppedRunes = estimateSubAgentMessagesRunes(history[:len(history)-1])
	}

	selected := reverseMessages(selectedReversed)
	result := make([]providertypes.Message, 0, 1+len(selected)+1)
	result = append(result, pinned)
	if droppedCount > 0 {
		result = append(result, buildSubAgentHistorySummaryMessage(droppedCount, droppedRunes))
	}
	result = append(result, selected...)
	return result
}

// clampSubAgentPinnedMessage 对首条任务消息进行文本收敛，防止初始上下文过大导致请求被上游拒绝。
func clampSubAgentPinnedMessage(message providertypes.Message, maxRunes int) providertypes.Message {
	if maxRunes <= 0 {
		return message
	}
	text := strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts))
	if text == "" {
		return message
	}
	clampedText, truncated := truncateSubAgentText(text, maxRunes)
	if !truncated {
		return message
	}
	clamped := message
	clamped.Parts = []providertypes.ContentPart{providertypes.NewTextPart(clampedText)}
	return clamped
}

// buildSubAgentHistorySummaryMessage 生成历史裁剪摘要，提示模型当前窗口已滚动。
func buildSubAgentHistorySummaryMessage(droppedMessages int, droppedRunes int) providertypes.Message {
	text := fmt.Sprintf(
		"[subagent_history_trimmed] dropped_messages=%d dropped_chars~=%d; keep only recent window.",
		droppedMessages,
		maxInt(0, droppedRunes),
	)
	return providertypes.Message{
		Role:  providertypes.RoleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(text)},
	}
}

// estimateSubAgentMessagesRunes 统计消息切片的近似字符规模，用于窗口预算控制。
func estimateSubAgentMessagesRunes(messages []providertypes.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateSubAgentMessageRunes(message)
	}
	return total
}

// estimateSubAgentMessageRunes 估算单条消息在提示词中的字符规模。
func estimateSubAgentMessageRunes(message providertypes.Message) int {
	total := len([]rune(partsrender.RenderDisplayParts(message.Parts)))
	total += len([]rune(strings.TrimSpace(message.ToolCallID)))
	for _, call := range message.ToolCalls {
		total += len([]rune(strings.TrimSpace(call.ID)))
		total += len([]rune(strings.TrimSpace(call.Name)))
		total += len([]rune(strings.TrimSpace(call.Arguments)))
	}
	for key, value := range message.ToolMetadata {
		total += len([]rune(strings.TrimSpace(key))) + len([]rune(strings.TrimSpace(value)))
	}
	return total
}

// truncateSubAgentText 按字符数截断文本，超限时追加统一后缀。
func truncateSubAgentText(text string, maxRunes int) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if maxRunes <= 0 || trimmed == "" {
		return "", trimmed != ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed, false
	}
	suffix := []rune(subAgentTextTruncatedSuffix)
	keep := maxRunes - len(suffix)
	if keep < 0 {
		keep = 0
	}
	return string(runes[:keep]) + subAgentTextTruncatedSuffix, true
}

// reverseMessages 反转消息切片顺序，用于把“倒序选择”的消息恢复为时间正序。
func reverseMessages(messages []providertypes.Message) []providertypes.Message {
	reversed := make([]providertypes.Message, len(messages))
	for idx := range messages {
		reversed[len(messages)-1-idx] = messages[idx]
	}
	return reversed
}

// minInt 返回两个整数中的较小值。
func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

// maxInt 返回两个整数中的较大值。
func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

// streamingHooksForSubAgent 返回子代理生成阶段使用的默认流式钩子。
func streamingHooksForSubAgent() streaming.Hooks {
	return streaming.Hooks{}
}
