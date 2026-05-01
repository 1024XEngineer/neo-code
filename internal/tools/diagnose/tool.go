package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/tools"
)

const diagnoseToolName = tools.ToolNameDiagnose

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

// Tool 提供 gateway.executeSystemTool(diagnose) 的最小可用实现。
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

// MicroCompactPolicy 保留诊断结果，避免在短期压缩时丢失排障上下文。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 校验输入并返回 Mock 诊断结构，供 Phase1 链路联调。
func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(diagnoseToolName, tools.NormalizeErrorReason(diagnoseToolName, err), "", nil), err
	}
	input, err := parseDiagnoseInput(call.Arguments)
	if err != nil {
		return tools.NewErrorResult(diagnoseToolName, tools.NormalizeErrorReason(diagnoseToolName, err), "", nil), err
	}

	result := diagnoseOutput{
		Confidence: 0.62,
		RootCause:  "mock: diagnose tool wiring is active; please replace with real analyzer in next phase",
		FixCommands: []string{
			"neocode diag",
		},
		InvestigationCommands: []string{
			"pwd",
			"env | grep -E 'NEOCODE_DIAG_SOCKET|SHELL'",
		},
	}
	if strings.TrimSpace(input.CommandText) != "" {
		result.InvestigationCommands = append(result.InvestigationCommands, strings.TrimSpace(input.CommandText))
	}

	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		wrapped := fmt.Errorf("%s: encode mock result: %w", diagnoseToolName, marshalErr)
		return tools.NewErrorResult(diagnoseToolName, tools.NormalizeErrorReason(diagnoseToolName, wrapped), "", nil), wrapped
	}

	return tools.ToolResult{
		Name:    diagnoseToolName,
		Content: string(raw),
		Metadata: map[string]any{
			"mock":            true,
			"error_log_bytes": len(input.ErrorLog),
		},
	}, nil
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
