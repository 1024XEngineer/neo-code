package domain

import (
	"context"
	"time"
)

type MemoryItem struct {
	ID             string    `json:"id"`
	UserInput      string    `json:"user_input"`
	AssistantReply string    `json:"assistant_reply"`
	Text           string    `json:"text"`
	Embedding      []float64 `json:"embedding"`
	CreatedAt      time.Time `json:"created_at"`
}

type MemoryRepository interface {
	List(ctx context.Context) ([]MemoryItem, error)
	Add(ctx context.Context, item MemoryItem) error
	Clear(ctx context.Context) error
}
