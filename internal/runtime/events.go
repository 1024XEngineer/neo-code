package runtime

// EventType identifies the kind of runtime event emitted during a run.
type EventType string

// RuntimeEvent is emitted by the runtime to report progress and terminal states
// for a specific run. RunID is provided by the caller and is echoed back on all
// events so upper layers can ignore stale events from older runs.
type RuntimeEvent struct {
	Type      EventType
	RunID     string
	SessionID string
	Payload   any
}

// PermissionRequestPayload 描述一次需要审批的权限请求上下文。
type PermissionRequestPayload struct {
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
}

// PermissionResolvedPayload 描述权限请求被运行时处理后的最终状态。
type PermissionResolvedPayload struct {
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
	ResolvedAs    string
}

const (
	// EventUserMessage is emitted after the user input has been accepted and saved.
	EventUserMessage EventType = "user_message"
	// EventAgentChunk carries streamed assistant text.
	EventAgentChunk EventType = "agent_chunk"
	// EventAgentDone is emitted when the assistant finishes normally.
	EventAgentDone EventType = "agent_done"
	// EventToolStart is emitted before a tool call begins execution.
	EventToolStart EventType = "tool_start"
	// EventToolResult is emitted after a tool call finishes and its result is saved.
	EventToolResult EventType = "tool_result"
	// EventToolChunk carries streamed tool output.
	EventToolChunk EventType = "tool_chunk"
	// EventRunCanceled is emitted once when the root run context is canceled.
	EventRunCanceled EventType = "run_canceled"
	// EventError is emitted for terminal runtime errors other than cancellation.
	EventError EventType = "error"
	// EventToolCallThinking is emitted when the model decides to call a tool,
	// before the tool execution begins. TUI can show a transitional indicator.
	EventToolCallThinking EventType = "tool_call_thinking"
	// EventProviderRetry is emitted when runtime retries a provider call due to
	// a retryable error (e.g. 429, 5xx). Payload is a human-readable message.
	EventProviderRetry EventType = "provider_retry"
	// EventPermissionRequest is emitted when a tool call hits an ask decision.
	EventPermissionRequest EventType = "permission_request"
	// EventPermissionResolved is emitted when runtime resolves a permission request or denial.
	EventPermissionResolved EventType = "permission_resolved"
	// EventCompactStart is emitted when a compact cycle starts.
	EventCompactStart EventType = "compact_start"
	// EventCompactDone is emitted when a compact cycle completes.
	EventCompactDone EventType = "compact_done"
	// EventCompactError is emitted when compact fails.
	EventCompactError EventType = "compact_error"
	// EventTokenUsage is emitted after each provider response with token statistics.
	EventTokenUsage EventType = "token_usage"
)

// TokenUsagePayload carries token usage statistics for a single provider turn.
type TokenUsagePayload struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	SessionInputTokens  int `json:"session_input_tokens"`
	SessionOutputTokens int `json:"session_output_tokens"`
}
