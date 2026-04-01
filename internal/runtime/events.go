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
)
