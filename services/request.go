package services

import (
	"context"
	"fmt"
	"neo-code/config"
	"neo-code/memory"
	"strconv"
	"strings"
	"sync"
	"time"

	"neo-code/ai"
)

type chatService struct {
	embeddingProvider ai.EmbeddingProvider
	store             memory.Store
	topK              int
	minScore          float64
}

type MemoryStats struct {
	Items    int
	TopK     int
	MinScore float64
	Path     string
}

var (
	serviceOnce sync.Once
	serviceInst *chatService
	serviceErr  error
)

func Chat(ctx context.Context, messages []ai.Message, model string) (<-chan string, error) {
	service, err := getChatService()
	if err != nil {
		return nil, err
	}

	provider, err := ai.NewChatProviderFromEnv(model)
	if err != nil {
		return nil, err
	}

	userInput := latestUserInput(messages)
	augmentedMessages := messages
	if userInput != "" {
		memoryContext, err := service.buildMemoryContext(ctx, userInput)
		if err != nil {
			return nil, err
		}
		if memoryContext != "" {
			augmentedMessages = append([]ai.Message{{Role: "system", Content: memoryContext}}, messages...)
		}
	}

	upstream, err := provider.Chat(ctx, augmentedMessages)
	if err != nil {
		return nil, err
	}

	out := make(chan string)
	go func() {
		defer close(out)

		var replyBuilder strings.Builder
		for chunk := range upstream {
			replyBuilder.WriteString(chunk)
			select {
			case <-ctx.Done():
				return
			case out <- chunk:
			}
		}

		if userInput == "" || replyBuilder.Len() == 0 {
			return
		}

		if err := service.saveMemory(context.Background(), userInput, replyBuilder.String()); err != nil {
			fmt.Printf("\nMemory save failed: %v\n", err)
		}
	}()

	return out, nil
}

func getChatService() (*chatService, error) {
	serviceOnce.Do(func() {
		embeddingProvider, err := ai.NewEmbeddingProviderFromEnv()
		if err != nil {
			serviceErr = err
			return
		}

		serviceInst = &chatService{
			embeddingProvider: embeddingProvider,
			store:             memory.NewFileStore(memoryFilePath(), memoryMaxItems()),
			topK:              memoryTopK(),
			minScore:          memoryMinScore(),
		}
	})

	return serviceInst, serviceErr
}

func GetMemoryStats(ctx context.Context) (MemoryStats, error) {
	service, err := getChatService()
	if err != nil {
		return MemoryStats{}, err
	}

	items, err := service.store.List(ctx)
	if err != nil {
		return MemoryStats{}, err
	}

	return MemoryStats{
		Items:    len(items),
		TopK:     service.topK,
		MinScore: service.minScore,
		Path:     memoryFilePath(),
	}, nil
}

func ClearMemory(ctx context.Context) error {
	service, err := getChatService()
	if err != nil {
		return err
	}

	return service.store.Clear(ctx)
}

func (s *chatService) buildMemoryContext(ctx context.Context, userInput string) (string, error) {
	queryEmbedding, err := s.embeddingProvider.Embed(ctx, userInput)
	if err != nil {
		return "", err
	}

	items, err := s.store.List(ctx)
	if err != nil {
		return "", err
	}

	matches := memory.Search(items, queryEmbedding, s.topK, s.minScore)
	if len(matches) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("The following memories may be relevant. Use them as reference, but do not quote them verbatim:\n")
	for i, match := range matches {
		builder.WriteString(fmt.Sprintf("Memory %d (score=%.3f)\n", i+1, match.Score))
		builder.WriteString("User: ")
		builder.WriteString(match.Item.UserInput)
		builder.WriteString("\nAssistant: ")
		builder.WriteString(match.Item.AssistantReply)
		builder.WriteString("\n")
	}

	return builder.String(), nil
}

func (s *chatService) saveMemory(ctx context.Context, userInput, assistantReply string) error {
	text := buildMemoryText(userInput, assistantReply)
	embedding, err := s.embeddingProvider.Embed(ctx, text)
	if err != nil {
		return err
	}

	item := memory.Item{
		ID:             strconv.FormatInt(time.Now().UnixNano(), 10),
		UserInput:      userInput,
		AssistantReply: assistantReply,
		Text:           text,
		Embedding:      embedding,
		CreatedAt:      time.Now().UTC(),
	}

	return s.store.Add(ctx, item)
}

func latestUserInput(messages []ai.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func buildMemoryText(userInput, assistantReply string) string {
	return "User: " + strings.TrimSpace(userInput) + "\nAssistant: " + strings.TrimSpace(assistantReply)
}

func memoryFilePath() string {
	if config.GlobalAppConfig != nil && strings.TrimSpace(config.GlobalAppConfig.Memory.FilePath) != "" {
		return strings.TrimSpace(config.GlobalAppConfig.Memory.FilePath)
	}
	return "./data/memory.json"
}

func memoryTopK() int {
	if config.GlobalAppConfig != nil && config.GlobalAppConfig.Memory.TopK > 0 {
		return config.GlobalAppConfig.Memory.TopK
	}
	return 5
}

func memoryMaxItems() int {
	if config.GlobalAppConfig != nil && config.GlobalAppConfig.Memory.MaxItems > 0 {
		return config.GlobalAppConfig.Memory.MaxItems
	}
	return 1000
}

func memoryMinScore() float64 {
	if config.GlobalAppConfig != nil && config.GlobalAppConfig.Memory.MinScore > 0 {
		return config.GlobalAppConfig.Memory.MinScore
	}
	return 0.75
}
