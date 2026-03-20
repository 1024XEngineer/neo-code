package memory

import (
	"context"
	"time"
)

type Item struct {
	ID             string    `json:"id"`
	UserInput      string    `json:"user_input"`
	AssistantReply string    `json:"assistant_reply"`
	Text           string    `json:"text"`
	Embedding      []float64 `json:"embedding"`
	CreatedAt      time.Time `json:"created_at"`
}

type Store interface {
	List(ctx context.Context) ([]Item, error)
	Add(ctx context.Context, item Item) error
	Clear(ctx context.Context) error
}
