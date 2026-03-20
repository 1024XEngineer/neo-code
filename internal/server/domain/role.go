package domain

import (
	"context"
	"time"
)

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RoleRepository interface {
	GetByID(ctx context.Context, id string) (*Role, error)
	GetByName(ctx context.Context, name string) (*Role, error)
	List(ctx context.Context) ([]Role, error)
	Save(ctx context.Context, role *Role) error
	Delete(ctx context.Context, id string) error
}

type RoleService interface {
	GetActivePrompt(ctx context.Context) (string, error)
	SetActive(ctx context.Context, roleID string) error
	List(ctx context.Context) ([]Role, error)
	Create(ctx context.Context, name, desc, prompt string) (*Role, error)
	Delete(ctx context.Context, id string) error
}
