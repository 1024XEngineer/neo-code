package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go-llm-demo/internal/server/domain"
)

type memoryServiceImpl struct {
	repo     domain.MemoryRepository
	topK     int
	minScore float64
}

func NewMemoryService(repo domain.MemoryRepository, topK int, minScore float64) domain.MemoryService {
	return &memoryServiceImpl{
		repo:     repo,
		topK:     topK,
		minScore: minScore,
	}
}

func (s *memoryServiceImpl) BuildContext(ctx context.Context, userInput string) (string, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return "", err
	}

	if len(items) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("以下是相关的历史记忆：\n")
	count := len(items)
	if count > s.topK {
		count = s.topK
	}
	for i := 0; i < count; i++ {
		item := items[i]
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(item.UserInput)
		builder.WriteString("\n")
	}

	return builder.String(), nil
}

func (s *memoryServiceImpl) Save(ctx context.Context, userInput, reply string) error {
	item := domain.MemoryItem{
		ID:             strconv.FormatInt(time.Now().UnixNano(), 10),
		UserInput:      userInput,
		AssistantReply: reply,
		Text:           userInput + "\n" + reply,
		CreatedAt:      time.Now().UTC(),
	}

	return s.repo.Add(ctx, item)
}

func (s *memoryServiceImpl) GetStats(ctx context.Context) (*domain.MemoryStats, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.MemoryStats{
		Count:    len(items),
		TopK:     s.topK,
		MinScore: s.minScore,
	}, nil
}

func (s *memoryServiceImpl) Clear(ctx context.Context) error {
	return s.repo.Clear(ctx)
}
