package service

import (
	"context"
	"fmt"
	"go-llm-demo/internal/server/infra/provider"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type chatServiceImpl struct {
	memorySvc  domain.MemoryService
	workingSvc domain.WorkingMemoryService
	roleSvc    domain.RoleService
	//provider   domain.ChatProvider
}

// NewChatService 使用记忆、角色和模型提供方依赖创建聊天服务。
func NewChatService(memorySvc domain.MemoryService, workingSvc domain.WorkingMemoryService, roleSvc domain.RoleService, provider domain.ChatProvider) domain.ChatGateway {
	return &chatServiceImpl{
		memorySvc:  memorySvc,
		workingSvc: workingSvc,
		roleSvc:    roleSvc,
		//provider:   provider,
	}
}

// Send 为消息补充角色和记忆上下文后发起流式回复。
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
	workingContext := ""
	if s.workingSvc != nil {
		workingContext, err = s.workingSvc.BuildContext(ctx, messages)
		if err != nil {
			return nil, err
		}
	}
	if userInput != "" {
		memoryContext, err := s.memorySvc.BuildContext(ctx, userInput)
		if err != nil {
			return nil, err
		}
		combinedContext := joinContextBlocks(workingContext, memoryContext)
		if combinedContext != "" {
			if rolePrompt != "" && len(messages) > 0 && messages[0].Role == "system" {
				messages[0].Content = rolePrompt + "\n\n" + combinedContext
			} else {
				messages = append([]domain.Message{{Role: "system", Content: combinedContext}}, messages...)
			}
		}
	} else if workingContext != "" {
		if rolePrompt != "" && len(messages) > 0 && messages[0].Role == "system" {
			messages[0].Content = rolePrompt + "\n\n" + workingContext
		} else {
			messages = append([]domain.Message{{Role: "system", Content: workingContext}}, messages...)
		}
	}

	chatProvider, err := provider.NewChatProvider(req.Model)
	if err != nil {
		return nil, err
	}

	out, err := chatProvider.Chat(ctx, messages)
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

		if s.latestUserInput(messages) != "" && replyBuilder.Len() > 0 {
			if s.workingSvc != nil {
				updatedMessages := append([]domain.Message{}, req.Messages...)
				updatedMessages = append(updatedMessages, domain.Message{Role: "assistant", Content: replyBuilder.String()})
				if err := s.workingSvc.Refresh(context.Background(), updatedMessages); err != nil {
					fmt.Printf("工作记忆刷新失败：%v\n", err)
				}
			}
			if err := s.memorySvc.Save(context.Background(), s.latestUserInput(messages), replyBuilder.String()); err != nil {
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

func joinContextBlocks(blocks ...string) string {
	filtered := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		filtered = append(filtered, block)
	}
	return strings.Join(filtered, "\n\n")
}
