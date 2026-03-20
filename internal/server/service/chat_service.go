package service

import (
	"context"
	"fmt"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type chatServiceImpl struct {
	memorySvc domain.MemoryService
	roleSvc   domain.RoleService
	provider  domain.ChatProvider
}

func NewChatService(memorySvc domain.MemoryService, roleSvc domain.RoleService, provider domain.ChatProvider) domain.ChatGateway {
	return &chatServiceImpl{
		memorySvc: memorySvc,
		roleSvc:   roleSvc,
		provider:  provider,
	}
}

func (s *chatServiceImpl) Send(ctx context.Context, req *domain.ChatRequest) (<-chan string, error) {
	messages := req.Messages

	rolePrompt, err := s.roleSvc.GetActivePrompt(ctx)
	if err != nil {
		fmt.Printf("获取角色提示失败：%v\n", err)
	} else if rolePrompt != "" {
		hasSystem := false
		for _, msg := range messages {
			if msg.Role == "system" {
				hasSystem = true
				break
			}
		}

		if !hasSystem {
			messages = append([]domain.Message{{Role: "system", Content: rolePrompt}}, messages...)
		}
	}

	userInput := s.latestUserInput(messages)
	if userInput != "" {
		memoryContext, err := s.memorySvc.BuildContext(ctx, userInput)
		if err != nil {
			return nil, err
		}
		if memoryContext != "" {
			if rolePrompt != "" && len(messages) > 0 && messages[0].Role == "system" {
				messages[0].Content = rolePrompt + "\n\n" + memoryContext
			} else {
				messages = append([]domain.Message{{Role: "system", Content: memoryContext}}, messages...)
			}
		}
	}

	out, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return nil, err
	}

	resultChan := make(chan string)
	go func() {
		defer close(resultChan)

		var replyBuilder strings.Builder
		for chunk := range out {
			replyBuilder.WriteString(chunk)
			resultChan <- chunk
		}

		if userInput != "" && replyBuilder.Len() > 0 {
			if err := s.memorySvc.Save(context.Background(), userInput, replyBuilder.String()); err != nil {
				fmt.Printf("记忆保存失败：%v\n", err)
			}
		}
	}()

	return resultChan, nil
}

func (s *chatServiceImpl) latestUserInput(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}
