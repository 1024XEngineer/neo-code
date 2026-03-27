package provider

import "context"

type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error)
}

type StreamEventType string

const (
	StreamEventTextDelta StreamEventType = "text_delta"
)

type StreamEvent struct {
	Type StreamEventType
	Text string
}
