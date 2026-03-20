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

type MemoryService interface {
	BuildContext(ctx context.Context, userInput string) (string, error)
	Save(ctx context.Context, userInput, reply string) error
	GetStats(ctx context.Context) (*MemoryStats, error)
	Clear(ctx context.Context) error
}

type MemoryStats struct {
	Count    int
	TopK     int
	MinScore float64
	Path     string
}
