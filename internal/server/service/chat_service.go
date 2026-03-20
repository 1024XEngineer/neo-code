package service

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/reposity"
)

type chatService struct {
	embeddingProvider provider.EmbeddingProvider
	store             domain.MemoryRepository
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

func Chat(ctx context.Context, messages []provider.Message, model string) (<-chan string, error) {
	service, err := getChatService()
	if err != nil {
		return nil, err
	}

	chatProvider, err := provider.NewChatProviderFromEnv(model)
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
			augmentedMessages = append([]provider.Message{{Role: "system", Content: memoryContext}}, messages...)
		}
	}

	upstream, err := chatProvider.Chat(ctx, augmentedMessages)
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
			fmt.Printf("\n记忆保存失败：%v\n", err)
		}
	}()

	return out, nil
}

func getChatService() (*chatService, error) {
	serviceOnce.Do(func() {
		embeddingProvider, err := provider.NewEmbeddingProviderFromEnv()
		if err != nil {
			serviceErr = err
			return
		}

		storePath := envString("MEMORY_FILE_PATH", "./data/memory.json")
		serviceInst = &chatService{
			embeddingProvider: embeddingProvider,
			store:             reposity.NewFileStore(storePath, envInt("MEMORY_MAX_ITEMS", 1000)),
			topK:              envInt("MEMORY_TOP_K", 5),
			minScore:          envFloat("MEMORY_MIN_SCORE", 0.75),
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
		Path:     envString("MEMORY_FILE_PATH", "./data/memory.json"),
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

	matches := search(items, queryEmbedding, s.topK, s.minScore)
	if len(matches) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("以下是与当前问题相关的历史记忆，请在回答时结合参考，但不要逐字复述：\n")
	for i, match := range matches {
		builder.WriteString(fmt.Sprintf("记忆 %d (score=%.3f)\n", i+1, match.Score))
		builder.WriteString("用户：")
		builder.WriteString(match.Item.UserInput)
		builder.WriteString("\n助手：")
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

	item := domain.MemoryItem{
		ID:             strconv.FormatInt(time.Now().UnixNano(), 10),
		UserInput:      userInput,
		AssistantReply: assistantReply,
		Text:           text,
		Embedding:      embedding,
		CreatedAt:      time.Now().UTC(),
	}

	return s.store.Add(ctx, item)
}

func latestUserInput(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func buildMemoryText(userInput, assistantReply string) string {
	return "用户：" + strings.TrimSpace(userInput) + "\n助手：" + strings.TrimSpace(assistantReply)
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

type Match struct {
	Item  domain.MemoryItem
	Score float64
}

func search(items []domain.MemoryItem, query []float64, topK int, minScore float64) []Match {
	if topK <= 0 || len(query) == 0 {
		return nil
	}

	matches := make([]Match, 0, len(items))
	for _, item := range items {
		score := cosineSimilarity(query, item.Embedding)
		if score < minScore {
			continue
		}
		matches = append(matches, Match{Item: item, Score: score})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > topK {
		matches = matches[:topK]
	}

	return matches
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}

	var dot float64
	var normA float64
	var normB float64

	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return -1
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
