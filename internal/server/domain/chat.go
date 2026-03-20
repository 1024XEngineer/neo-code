package domain

import (
	"context"
)

type ChatRequest struct {
	Messages []Message
	Model    string
}

type ChatGateway interface {
	Send(ctx context.Context, req *ChatRequest) (<-chan string, error)
}

type ChatProvider interface {
	GetModelName() string
	Chat(ctx context.Context, messages []Message) (<-chan string, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
