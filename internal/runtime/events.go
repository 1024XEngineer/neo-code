package runtime

type EventType string

type RuntimeEvent struct {
	Type      EventType
	SessionID string
	Payload   any
}

const (
	EventUserMessage EventType = "user_message"
	EventAgentChunk  EventType = "agent_chunk"
	EventAgentDone   EventType = "agent_done"
	EventToolStart   EventType = "tool_start"
	EventToolResult  EventType = "tool_result"
	EventToolChunk   EventType = "tool_chunk"
	EventError       EventType = "error"
)

const (
	EventToolStarted   = EventToolStart
	EventToolFinished  = EventToolResult
	EventAgentComplete = EventAgentDone
)
