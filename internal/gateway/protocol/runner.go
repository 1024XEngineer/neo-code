package protocol

import (
	"encoding/json"

	"neo-code/internal/security"
)

const (
	// MethodGatewayRegisterRunner 表示 runner 向网关注册。
	MethodGatewayRegisterRunner = "gateway.registerRunner"
	// MethodGatewayExecuteToolResult 表示 runner 回传工具执行结果。
	MethodGatewayExecuteToolResult = "gateway.executeToolResult"
	// MethodGatewayToolRequest 表示网关推送工具执行请求给 runner。
	MethodGatewayToolRequest = "gateway.toolRequest"
)

// RegisterRunnerParams 是 runner 注册时的参数。
type RegisterRunnerParams struct {
	RunnerID   string   `json:"runner_id"`
	Workdir    string   `json:"workdir"`
	RunnerName string   `json:"runner_name,omitempty"`
	Labels     []string `json:"labels,omitempty"`
}

// ExecuteToolResultParams 是 runner 回传工具执行结果的参数。
type ExecuteToolResultParams struct {
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id"`
	RunnerID   string `json:"runner_id"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// ToolRequestParams 是网关推送给 runner 的工具执行请求。
type ToolRequestParams struct {
	RequestID       string                   `json:"request_id"`
	SessionID       string                   `json:"session_id"`
	RunID           string                   `json:"run_id"`
	ToolCallID      string                   `json:"tool_call_id"`
	ToolName        string                   `json:"tool_name"`
	Arguments       json.RawMessage          `json:"arguments"`
	CapabilityToken *security.CapabilityToken `json:"capability_token,omitempty"`
}
