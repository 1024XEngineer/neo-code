package runner

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrCapabilityTokenRequired 表示 capability token 缺失。
	ErrCapabilityTokenRequired = errors.New("runner: capability token required")
	// ErrCapabilityTokenExpired 表示 capability token 已过期。
	ErrCapabilityTokenExpired = errors.New("runner: capability token expired")
	// ErrCapabilitySignatureInvalid 表示 capability token 签名无效。
	ErrCapabilitySignatureInvalid = errors.New("runner: capability token signature invalid")
	// ErrCapabilityToolNotAllowed 表示工具不在 capability token 允许列表中。
	ErrCapabilityToolNotAllowed = errors.New("runner: tool not allowed by capability token")
	// ErrCapabilityPathNotAllowed 表示路径不在 capability token 允许范围内。
	ErrCapabilityPathNotAllowed = errors.New("runner: path not allowed by capability token")
	// ErrRunnerStopped 表示 runner 已停止。
	ErrRunnerStopped = errors.New("runner: runner is stopped")
)

// ToolExecutionRequest 表示从网关收到的工具执行请求。
type ToolExecutionRequest struct {
	RequestID  string          `json:"request_id"`
	SessionID  string          `json:"session_id"`
	RunID      string          `json:"run_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Arguments  json.RawMessage `json:"arguments"`
}

// ToolExecutionResult 表示工具执行结果。
type ToolExecutionResult struct {
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id"`
	RunnerID   string `json:"runner_id"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

// HeartbeatConfig 包含心跳相关配置。
type HeartbeatConfig struct {
	Interval time.Duration
	Timeout  time.Duration
}

// ReconnectConfig 包含重连相关配置。
type ReconnectConfig struct {
	MinBackoff time.Duration
	MaxBackoff time.Duration
}
