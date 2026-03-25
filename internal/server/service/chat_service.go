package service

import (
	"context"
	"fmt"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type chatServiceImpl struct {
	memorySvc        domain.MemoryService
	workingSvc       domain.WorkingMemoryService
	projectMemorySvc domain.ProjectMemoryService
	todoSvc          domain.TodoService
	roleSvc          domain.RoleService
	chatProvider     domain.ChatProvider
}

// NewChatService creates a chat service from role, memory, todo, and provider dependencies.
func NewChatService(
	memorySvc domain.MemoryService,
	workingSvc domain.WorkingMemoryService,
	projectMemorySvc domain.ProjectMemoryService,
	todoSvc domain.TodoService,
	roleSvc domain.RoleService,
	chatProvider domain.ChatProvider,
) domain.ChatGateway {
	return &chatServiceImpl{
		memorySvc:        memorySvc,
		workingSvc:       workingSvc,
		projectMemorySvc: projectMemorySvc,
		todoSvc:          todoSvc,
		roleSvc:          roleSvc,
		chatProvider:     chatProvider,
	}
}

// Send injects role, explicit project memory, working memory, and recalled memory before chatting.
func (s *chatServiceImpl) Send(ctx context.Context, req *domain.ChatRequest) (<-chan string, error) {
	messages := req.Messages

	rolePrompt, err := s.roleSvc.GetActivePrompt(ctx)
	if err != nil {
		fmt.Printf("failed to load role prompt: %v\n", err)
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
	projectContext := ""
	if s.projectMemorySvc != nil {
		projectContext, err = s.projectMemorySvc.BuildContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	workingContext := ""
	if s.workingSvc != nil {
		workingContext, err = s.workingSvc.BuildContext(ctx, messages)
		if err != nil {
			return nil, err
		}
	}

	todoContext := ""
	if s.todoSvc != nil {
		todos, _ := s.todoSvc.ListTodos(ctx)
		todoContext = buildTodoContext(todos)
	}

	blocks := []string{projectContext, workingContext, todoContext}
	if userInput != "" && s.memorySvc != nil {
		memoryContext, ctxErr := s.memorySvc.BuildContext(ctx, userInput)
		if ctxErr != nil {
			return nil, ctxErr
		}
		blocks = append(blocks, memoryContext)
	}

	combinedContext := joinContextBlocks(blocks...)
	if combinedContext != "" {
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

		latestInput := s.latestUserInput(messages)
		if latestInput == "" || replyBuilder.Len() == 0 {
			return
		}

		if s.workingSvc != nil {
			updatedMessages := append([]domain.Message{}, req.Messages...)
			updatedMessages = append(updatedMessages, domain.Message{Role: "assistant", Content: replyBuilder.String()})
			if err := s.workingSvc.Refresh(context.Background(), updatedMessages); err != nil {
				fmt.Printf("failed to refresh working memory: %v\n", err)
			}
		}
		if s.memorySvc != nil {
			if err := s.memorySvc.Save(context.Background(), latestInput, replyBuilder.String()); err != nil {
				fmt.Printf("failed to save memory: %v\n", err)
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
	if rolePrompt != "" && len(messages) > 0 && messages[0].Role == "system" {
		messages[0].Content = rolePrompt + "\n\n" + combinedContext
		return messages
	}
	return append([]domain.Message{{Role: "system", Content: combinedContext}}, messages...)
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
