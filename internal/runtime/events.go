package runtime

import (
	"sync"
	"time"
)

const (
	EventSessionCreated = "session_created"
	EventUserMessage    = "user_message"
	EventAgentChunk     = "agent_chunk"
	EventAgentMessage   = "agent_message"
	EventToolStarted    = "tool_started"
	EventToolFinished   = "tool_finished"
	EventStatus         = "status"
	EventCompleted      = "agent_completed"
	EventError          = "error"
)

// Event is the runtime-to-TUI event envelope.
type Event struct {
	Type      string
	SessionID string
	Payload   any
	At        time.Time
}

// EventBus is a lightweight in-process pub/sub bus for runtime events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

// NewEventBus creates an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make([]chan Event, 0, 1),
	}
}

// Subscribe registers a buffered event channel.
func (b *EventBus) Subscribe(buffer int) <-chan Event {
	ch := make(chan Event, buffer)

	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()

	return ch
}

// Publish broadcasts an event to all subscribers without blocking runtime execution.
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}
