package infra

import (
	"context"
	"strings"

	"go-llm-demo/config"
	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/repository"
	"go-llm-demo/internal/server/service"
)

type Message = domain.Message

type ChatClient interface {
	Chat(ctx context.Context, messages []Message, model string) (<-chan string, error)
	GetMemoryStats(ctx context.Context) (*MemoryStats, error)
	ClearMemory(ctx context.Context) error
	ClearSessionMemory(ctx context.Context) error
	ListModels() []string
	DefaultModel() string
}

type MemoryStats struct {
	PersistentItems int
	SessionItems    int
	TotalItems      int
	TopK            int
	MinScore        float64
	Path            string
	ByType          map[string]int
}

type localChatClient struct {
	roleSvc   domain.RoleService
	memorySvc domain.MemoryService
	config    *config.AppConfiguration
}

func NewLocalChatClient() (ChatClient, error) {
	cfg := config.GlobalAppConfig
	if cfg == nil {
		return nil, context.Canceled
	}

	storePath := strings.TrimSpace(cfg.Memory.StoragePath)
	if storePath == "" {
		storePath = "./data/memory_rules.json"
	}
	maxItems := cfg.Memory.MaxItems
	if maxItems <= 0 {
		maxItems = 1000
	}
	persistentRepo := repository.NewFileMemoryStore(storePath, maxItems)
	sessionRepo := repository.NewSessionMemoryStore(maxItems)
	memorySvc := service.NewMemoryService(
		persistentRepo,
		sessionRepo,
		cfg.Memory.TopK,
		cfg.Memory.MinMatchScore,
		cfg.Memory.MaxPromptChars,
		storePath,
		cfg.Memory.PersistTypes,
	)

	roleRepo := repository.NewFileRoleStore("./data/roles.json")
	roleSvc := service.NewRoleService(roleRepo, strings.TrimSpace(cfg.Persona.FilePath))

	return &localChatClient{roleSvc: roleSvc, memorySvc: memorySvc, config: cfg}, nil
}

func (c *localChatClient) Chat(ctx context.Context, messages []Message, model string) (<-chan string, error) {
	chatProvider, err := provider.NewChatProvider(model)
	if err != nil {
		return nil, err
	}
	chatSvc := service.NewChatService(c.memorySvc, c.roleSvc, chatProvider)
	return chatSvc.Send(ctx, &domain.ChatRequest{Messages: messages, Model: model})
}

func (c *localChatClient) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	stats, err := c.memorySvc.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return &MemoryStats{
		PersistentItems: stats.PersistentItems,
		SessionItems:    stats.SessionItems,
		TotalItems:      stats.TotalItems,
		TopK:            stats.TopK,
		MinScore:        stats.MinScore,
		Path:            stats.Path,
		ByType:          stats.ByType,
	}, nil
}

func (c *localChatClient) ClearMemory(ctx context.Context) error {
	return c.memorySvc.Clear(ctx)
}

func (c *localChatClient) ClearSessionMemory(ctx context.Context) error {
	return c.memorySvc.ClearSession(ctx)
}

func (c *localChatClient) ListModels() []string {
	return provider.SupportedModels()
}

func (c *localChatClient) DefaultModel() string {
	if c.config != nil && strings.TrimSpace(c.config.AI.Model) != "" {
		return strings.TrimSpace(c.config.AI.Model)
	}
	return provider.DefaultModel()
}
