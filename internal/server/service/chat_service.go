package service

import (
	"context"
	"fmt"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type chatServiceImpl struct {
	memorySvc    domain.MemoryService
	workingSvc   domain.WorkingMemoryService
	todoSvc      domain.TodoService
	roleSvc      domain.RoleService
	chatProvider domain.ChatProvider
	toolSchema   string
}

// NewChatService creates a chat service with memory, role, todo, tool schema,
// and chat provider dependencies.
func NewChatService(memorySvc domain.MemoryService, workingSvc domain.WorkingMemoryService, todoSvc domain.TodoService, roleSvc domain.RoleService, chatProvider domain.ChatProvider, toolSchema string) domain.ChatGateway {
	return &chatServiceImpl{
		memorySvc:    memorySvc,
		workingSvc:   workingSvc,
		todoSvc:      todoSvc,
		roleSvc:      roleSvc,
		chatProvider: chatProvider,
		toolSchema:   toolSchema,
	}
}

// Send enriches the request with role and runtime context before streaming.
func (s *chatServiceImpl) Send(ctx context.Context, req *domain.ChatRequest) (<-chan string, error) {
	messages := req.Messages

	rolePrompt, err := s.roleSvc.GetActivePrompt(ctx)
	if err != nil {
		fmt.Printf("获取角色提示失败：%v\n", err)
	} else if rolePrompt != "" {
		hasLeadingSystem := len(messages) > 0 && messages[0].Role == "system"
		if !hasLeadingSystem {
			// Keep the role prompt as the leading system message when the caller
			// did not already provide one at the front of the conversation.
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
	todoContext := ""
	if s.todoSvc != nil {
		todos, todoErr := s.todoSvc.ListTodos(ctx)
		if todoErr != nil {
			return nil, todoErr
		}
		todoContext = buildTodoContext(todos)
	}

	blocks := []string{s.toolSchema, workingContext, todoContext}
	if userInput != "" {
		memoryContext, ctxErr := s.memorySvc.BuildContext(ctx, userInput)
		if ctxErr != nil {
			return nil, ctxErr
		}
		blocks = append(blocks, memoryContext)
	}
	combinedContext := joinContextBlocks(blocks...)
	if rolePrompt != "" || combinedContext != "" {
		messages = injectSystemContext(messages, rolePrompt, combinedContext)
	}

	out, err := s.chatProvider.Chat(ctx, messages)
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
				// Refresh working memory from the original request messages so the
				// injected system context does not become conversation history.
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

func buildTodoContext(todos []domain.Todo) string {
	if len(todos) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[TODO_LIST]\n")
	for _, todo := range todos {
		sb.WriteString(fmt.Sprintf("- %s: %s (status: %s, priority: %s)\n", todo.ID, todo.Content, todo.Status, todo.Priority))
	}
	return sb.String()
}

func injectSystemContext(messages []domain.Message, rolePrompt, combinedContext string) []domain.Message {
	systemContext := joinContextBlocks(rolePrompt, combinedContext)
	if systemContext == "" {
		return messages
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		if rolePrompt != "" && strings.Contains(messages[0].Content, rolePrompt) {
			messages[0].Content = joinContextBlocks(messages[0].Content, combinedContext)
			return messages
		}
		messages[0].Content = joinContextBlocks(systemContext, messages[0].Content)
		return messages
	}
	return append([]domain.Message{{Role: "system", Content: systemContext}}, messages...)
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
