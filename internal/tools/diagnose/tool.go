package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

const (
	diagnoseToolName = tools.ToolNameDiagnose

	diagnoseSubAgentTaskID  = "terminal-diagnose"
	diagnoseSubAgentTimeout = 25 * time.Second
	diagnoseSubAgentGrace   = 5 * time.Second
	diagnoseSubAgentSteps   = 4

	diagnoseGoalLogMaxRunes     = 6000
	diagnoseGoalCommandMaxRunes = 512
)

var confidencePattern = regexp.MustCompile(`(?i)\bconfidence\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)\b`)

type diagnoseInput struct {
	ErrorLog    string            `json:"error_log"`
	OSEnv       map[string]string `json:"os_env"`
	CommandText string            `json:"command_text"`
	ExitCode    int               `json:"exit_code"`
}

type diagnoseOutput struct {
	Confidence            float64  `json:"confidence"`
	RootCause             string   `json:"root_cause"`
	FixCommands           []string `json:"fix_commands"`
	InvestigationCommands []string `json:"investigation_commands"`
}

// Tool 提供 gateway.executeSystemTool(diagnose) 的真实诊断实现。
type Tool struct{}

// New 创建 diagnose 工具实例。
func New() *Tool {
	return &Tool{}
}

// Name 返回工具唯一名称。
func (t *Tool) Name() string {
	return diagnoseToolName
}

// Description 返回工具描述信息。
func (t *Tool) Description() string {
	return "Diagnose terminal failures from recent shell output and environment context."
}

// Schema 返回 diagnose 参数结构定义。
func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error_log": map[string]any{
				"type": "string",
			},
			"os_env": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
			"command_text": map[string]any{
				"type": "string",
			},
			"exit_code": map[string]any{
				"type": "integer",
			},
		},
		"required": []string{"error_log", "os_env"},
	}
}

// MicroCompactPolicy 保留诊断结果，避免短期压缩时丢失排障上下文。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 校验输入并通过 SpawnSubAgent 能力链路执行真实诊断，失败时静默降级。
func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(diagnoseToolName, tools.NormalizeErrorReason(diagnoseToolName, err), "", nil), err
	}
	input, err := parseDiagnoseInput(call.Arguments)
	if err != nil {
		return tools.NewErrorResult(diagnoseToolName, tools.NormalizeErrorReason(diagnoseToolName, err), "", nil), err
	}

	output, metadata := runDiagnoseWithSpawnSubAgent(ctx, call, input)
	output = normalizeDiagnosisOutput(output)

	raw, marshalErr := json.Marshal(output)
	if marshalErr != nil {
		fallback := normalizeDiagnosisOutput(buildFallbackDiagnosis(input, "diagnose output marshal failed"))
		raw, _ = json.Marshal(fallback)
		metadata["degraded"] = true
		metadata["degraded_reason"] = "marshal_failed"
	}

	result := tools.ToolResult{
		Name:     diagnoseToolName,
		Content:  string(raw),
		IsError:  false,
		Metadata: metadata,
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
	return result, nil
}

// runDiagnoseWithSpawnSubAgent 复用 runtime 注入的 SpawnSubAgent 执行桥接完成真实诊断。
func runDiagnoseWithSpawnSubAgent(
	ctx context.Context,
	call tools.ToolCallInput,
	input diagnoseInput,
) (diagnoseOutput, map[string]any) {
	metadata := map[string]any{
		"backend":      "spawn_subagent",
		"backend_tool": tools.ToolNameSpawnSubAgent,
	}
	if call.SubAgentInvoker == nil {
		metadata["degraded"] = true
		metadata["degraded_reason"] = "subagent_invoker_unavailable"
		return buildFallbackDiagnosis(input, "subagent invoker unavailable"), metadata
	}

	workdir := resolveDiagnoseWorkdir(call.Workdir, input.OSEnv)
	allowedPaths := normalizePathList(workdir)
	runResult, runErr := runDiagnoseSubAgent(ctx, call.SubAgentInvoker, tools.SubAgentRunInput{
		CallerAgent:           strings.TrimSpace(call.AgentID),
		ParentCapabilityToken: call.CapabilityToken,
		Role:                  subagent.RoleReviewer,
		TaskType:              subagent.TaskTypeReview,
		ToolUseMode:           subagent.ToolUseModeDisabled,
		TaskID:                diagnoseSubAgentTaskID,
		Goal:                  buildDiagnoseGoal(input, workdir),
		ExpectedOut:           buildDiagnoseExpectedOutput(),
		Workdir:               workdir,
		MaxSteps:              diagnoseSubAgentSteps,
		Timeout:               diagnoseSubAgentTimeout,
		AllowedTools:          []string{},
		AllowedPaths:          allowedPaths,
	})
	if runErr != nil {
		metadata["degraded"] = true
		metadata["degraded_reason"] = "subagent_run_error"
		metadata["subagent_error"] = strings.TrimSpace(runErr.Error())
		return buildFallbackDiagnosis(input, runErr.Error()), metadata
	}
	if runResult.State != subagent.StateSucceeded {
		metadata["degraded"] = true
		metadata["degraded_reason"] = "subagent_state_not_succeeded"
		metadata["subagent_state"] = string(runResult.State)
		metadata["subagent_stop_reason"] = string(runResult.StopReason)
		metadata["subagent_error"] = strings.TrimSpace(runResult.Error)
		return buildFallbackDiagnosis(input, runResult.Error), metadata
	}

	parsed, parseErr := parseSubAgentDiagnosis(runResult.Output)
	if parseErr != nil {
		metadata["degraded"] = true
		metadata["degraded_reason"] = "subagent_output_unparseable"
		metadata["subagent_error"] = strings.TrimSpace(parseErr.Error())
		return buildFallbackDiagnosis(input, parseErr.Error()), metadata
	}
	metadata["degraded"] = false
	metadata["subagent_state"] = string(runResult.State)
	metadata["subagent_stop_reason"] = string(runResult.StopReason)
	metadata["subagent_step_count"] = runResult.StepCount
	return parsed, metadata
}

// runDiagnoseSubAgent 以预算超时为上限执行子代理，超时时回退为降级结果而不是阻塞外层调用。
func runDiagnoseSubAgent(
	parent context.Context,
	invoker tools.SubAgentInvoker,
	input tools.SubAgentRunInput,
) (tools.SubAgentRunResult, error) {
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = diagnoseSubAgentTimeout
	}
	runCtx, cancel := context.WithTimeout(parent, timeout+diagnoseSubAgentGrace)
	defer cancel()

	done := make(chan struct {
		result tools.SubAgentRunResult
		err    error
	}, 1)
	go func() {
		result, err := invoker.Run(runCtx, input)
		done <- struct {
			result tools.SubAgentRunResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-runCtx.Done():
		return tools.SubAgentRunResult{}, runCtx.Err()
	case out := <-done:
		return out.result, out.err
	}
}

// buildDiagnoseGoal 构造发送给子代理的任务文本，限制上下文规模并强调输出约束。
func buildDiagnoseGoal(input diagnoseInput, workdir string) string {
	goal := []string{
		"你是终端故障诊断代理。请只基于给定日志和环境做根因判断，禁止臆测不存在的文件内容。",
		"输出必须可执行、可落地，命令尽量短小；不确定时要在 risks 中说明。",
	}

	commandText := truncateRunes(strings.TrimSpace(input.CommandText), diagnoseGoalCommandMaxRunes)
	if commandText != "" {
		goal = append(goal, "失败命令: "+commandText)
	}
	goal = append(goal, fmt.Sprintf("退出码: %d", input.ExitCode))
	if strings.TrimSpace(workdir) != "" {
		goal = append(goal, "工作目录: "+strings.TrimSpace(workdir))
	}
	if shell := strings.TrimSpace(input.OSEnv["shell"]); shell != "" {
		goal = append(goal, "Shell: "+shell)
	}
	if osName := strings.TrimSpace(input.OSEnv["os"]); osName != "" {
		goal = append(goal, "OS: "+osName)
	}

	goal = append(goal, "", "错误日志片段:")
	goal = append(goal, truncateRunes(strings.TrimSpace(input.ErrorLog), diagnoseGoalLogMaxRunes))
	return strings.Join(goal, "\n")
}

// buildDiagnoseExpectedOutput 声明子代理 contract 字段如何映射到 diagnose JSON 契约。
func buildDiagnoseExpectedOutput() string {
	return strings.Join([]string{
		"请返回 subagent 标准 JSON 对象（report/findings/patches/risks/next_actions/artifacts）。",
		"字段映射要求：",
		"1) report: 必填，写一条根因结论（root_cause）。",
		"2) findings: 第一条必须写成 confidence=<0~1>，其余写证据。",
		"3) patches: 仅放修复命令（fix_commands）。",
		"4) next_actions: 仅放进一步排查命令（investigation_commands）。",
		"5) 若信息不足，confidence 要降低并在 risks 解释。",
	}, "\n")
}

// parseSubAgentDiagnosis 将子代理输出映射为 diagnose 的固定 JSON 契约结构。
func parseSubAgentDiagnosis(output subagent.Output) (diagnoseOutput, error) {
	summary := strings.TrimSpace(output.Summary)
	if summary == "" {
		summary = strings.TrimSpace(output.Report)
	}
	if summary == "" {
		return diagnoseOutput{}, errors.New("empty summary/report")
	}

	// 优先兼容“summary 内直接输出 diagnose JSON”的情况。
	if decoded, ok := decodeDiagnosisJSON(summary); ok {
		return normalizeDiagnosisOutput(decoded), nil
	}

	parsed := diagnoseOutput{
		Confidence:            parseConfidence(output.Findings),
		RootCause:             summary,
		FixCommands:           normalizeCommandList(output.Patches),
		InvestigationCommands: normalizeCommandList(output.NextActions),
	}
	if parsed.Confidence <= 0 {
		parsed.Confidence = 0.56
	}
	if len(parsed.InvestigationCommands) == 0 {
		parsed.InvestigationCommands = normalizeCommandList(output.Findings)
	}
	return normalizeDiagnosisOutput(parsed), nil
}

// decodeDiagnosisJSON 解析 summary 中潜在的 diagnose JSON 对象并校验关键字段。
func decodeDiagnosisJSON(raw string) (diagnoseOutput, bool) {
	var output diagnoseOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return diagnoseOutput{}, false
	}
	if strings.TrimSpace(output.RootCause) == "" {
		return diagnoseOutput{}, false
	}
	return normalizeDiagnosisOutput(output), true
}

// parseConfidence 从 findings 中提取 confidence=0.0~1.0，缺失时返回 0。
func parseConfidence(findings []string) float64 {
	for _, finding := range findings {
		match := confidencePattern.FindStringSubmatch(strings.TrimSpace(finding))
		if len(match) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		return clampConfidence(value)
	}
	return 0
}

// buildFallbackDiagnosis 构造静默降级结果，保证输出契约完整且无 panic。
func buildFallbackDiagnosis(input diagnoseInput, reason string) diagnoseOutput {
	rootCause := "当前诊断服务不可用，已降级为保守建议。"
	if normalizedReason := normalizeDiagnoseFallbackReason(reason); normalizedReason != "" {
		rootCause = rootCause + " (" + normalizedReason + ")"
	}
	investigation := []string{
		"pwd",
		"echo $SHELL",
	}
	if cmd := strings.TrimSpace(input.CommandText); cmd != "" {
		investigation = append(investigation, cmd)
	}
	return normalizeDiagnosisOutput(diagnoseOutput{
		Confidence:            0.18,
		RootCause:             rootCause,
		FixCommands:           []string{},
		InvestigationCommands: investigation,
	})
}

// normalizeDiagnoseFallbackReason 规整降级原因文案，避免直接暴露难以理解的底层细节。
func normalizeDiagnoseFallbackReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	if strings.Contains(lowered, "subagent output json object missing required contract keys") ||
		strings.Contains(lowered, "subagent output missing required key") ||
		strings.Contains(lowered, "subagent output does not contain json object") ||
		strings.Contains(lowered, "subagent output contains incomplete json object") {
		return "诊断模型返回格式不符合契约"
	}
	return trimmed
}

// normalizeDiagnosisOutput 统一收敛字段内容，确保 JSON 契约稳定可解析。
func normalizeDiagnosisOutput(output diagnoseOutput) diagnoseOutput {
	output.Confidence = clampConfidence(output.Confidence)
	output.RootCause = strings.TrimSpace(output.RootCause)
	if output.RootCause == "" {
		output.RootCause = "未获得有效根因，请先执行 investigation_commands 收集更多信息。"
	}
	output.FixCommands = normalizeCommandList(output.FixCommands)
	output.InvestigationCommands = normalizeCommandList(output.InvestigationCommands)
	if output.FixCommands == nil {
		output.FixCommands = []string{}
	}
	if output.InvestigationCommands == nil {
		output.InvestigationCommands = []string{}
	}
	return output
}

// normalizeCommandList 清洗命令列表并去重，保留首个出现顺序。
func normalizeCommandList(commands []string) []string {
	if len(commands) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(commands))
	normalized := make([]string, 0, len(commands))
	for _, command := range commands {
		trimmed := strings.TrimSpace(command)
		if trimmed == "" {
			continue
		}
		if _, duplicated := seen[trimmed]; duplicated {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// resolveDiagnoseWorkdir 统一诊断工作目录来源，优先调用方再回退到 os_env.cwd。
func resolveDiagnoseWorkdir(callWorkdir string, osEnv map[string]string) string {
	if trimmed := strings.TrimSpace(callWorkdir); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(osEnv["cwd"]); trimmed != "" {
		return trimmed
	}
	return ""
}

// normalizePathList 规整 allowed_paths 参数，空值时返回 nil。
func normalizePathList(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	return []string{trimmed}
}

// clampConfidence 将置信度约束在 [0,1] 区间内。
func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// truncateRunes 按 rune 长度截断文本，避免上下文过大导致子代理超时。
func truncateRunes(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if maxRunes <= 0 || trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "\n...[truncated]"
}

// parseDiagnoseInput 解析并校验 diagnose 工具输入参数。
func parseDiagnoseInput(arguments []byte) (diagnoseInput, error) {
	trimmed := strings.TrimSpace(string(arguments))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return diagnoseInput{}, errors.New("diagnose: error_log is required")
	}

	var input diagnoseInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return diagnoseInput{}, fmt.Errorf("diagnose: invalid arguments: %w", err)
	}
	input.ErrorLog = strings.TrimSpace(input.ErrorLog)
	input.CommandText = strings.TrimSpace(input.CommandText)

	if input.ErrorLog == "" {
		return diagnoseInput{}, errors.New("diagnose: error_log is required")
	}
	if len(input.OSEnv) == 0 {
		return diagnoseInput{}, errors.New("diagnose: os_env is required")
	}
	return input, nil
}
