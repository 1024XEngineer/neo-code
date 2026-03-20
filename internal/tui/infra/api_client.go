package infra

import (
	"context"
	"os"
	"strconv"
	"strings"

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
}

type MemoryStats struct {
	Items    int
	TopK     int
	MinScore float64
	Path     string
}

type localChatClient struct {
	chatSvc     domain.ChatGateway
	memorySvc   domain.MemoryService
	memoryStats *MemoryStats
}

func NewLocalChatClient() (ChatClient, error) {
	storePath := envString("MEMORY_FILE_PATH", "./data/memory.json")
	memoryRepo := repository.NewFileMemoryStore(storePath, envInt("MEMORY_MAX_ITEMS", 1000))
	memorySvc := service.NewMemoryService(memoryRepo, envInt("MEMORY_TOP_K", 5), envFloat("MEMORY_MIN_SCORE", 0.75))

	roleRepo := repository.NewFileRoleStore(envString("ROLE_FILE_PATH", "./data/roles.json"))
	roleSvc := service.NewRoleService(roleRepo, envString("PERSONA_FILE_PATH", ""))

	chatProvider, err := provider.NewChatProviderFromEnv(envString("AI_MODEL", ""))
	if err != nil {
		return nil, err
	}

	// 直接使用，无需适配器
	chatGateway := service.NewChatService(memorySvc, roleSvc, chatProvider)

	stats, err := memorySvc.GetStats(context.Background())
	if err != nil {
		return nil, err
	}

	return &localChatClient{
		chatSvc:   chatGateway,
		memorySvc: memorySvc,
		memoryStats: &MemoryStats{
			Items:    stats.Count,
			TopK:     stats.TopK,
			MinScore: stats.MinScore,
			Path:     storePath,
		},
	}, nil
}

func (c *localChatClient) Chat(ctx context.Context, messages []Message, model string) (<-chan string, error) {
	return c.chatSvc.Send(ctx, &domain.ChatRequest{
		Messages: messages,
		Model:    model,
	})
}

func (c *localChatClient) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	return c.memoryStats, nil
}

func (c *localChatClient) ClearMemory(ctx context.Context) error {
	return c.memorySvc.Clear(ctx)
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
