package runtime

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// appendUserMessageAndSave 将用户消息追加到会话并立即持久化。
func (s *Service) appendUserMessageAndSave(ctx context.Context, state *runState, content string) error {
	message := providertypes.Message{
		Role:    providertypes.RoleUser,
		Content: content,
	}
	state.session.Messages = append(state.session.Messages, message)
	state.touchSession()
	if err := s.sessionStore.Save(ctx, &state.session); err != nil {
		return err
	}
	s.emitRunScoped(ctx, EventUserMessage, state, message)
	return nil
}

// appendAssistantMessageAndSave 将 assistant 消息和本轮模型元数据写回会话。
func (s *Service) appendAssistantMessageAndSave(
	ctx context.Context,
	state *runState,
	snapshot turnSnapshot,
	assistant providertypes.Message,
) error {
	metadataChanged := state.session.Provider != snapshot.providerConfig.Name || state.session.Model != snapshot.model
	state.session.Provider = snapshot.providerConfig.Name
	state.session.Model = snapshot.model

	if strings.TrimSpace(assistant.Content) != "" || len(assistant.ToolCalls) > 0 {
		state.session.Messages = append(state.session.Messages, assistant)
		state.touchSession()
		return s.sessionStore.Save(ctx, &state.session)
	}
	if metadataChanged {
		state.touchSession()
		return s.sessionStore.Save(ctx, &state.session)
	}
	return nil
}

// appendToolMessageAndSave 将工具原始结果写回会话，避免污染持久化对话内容。
func (s *Service) appendToolMessageAndSave(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	result tools.ToolResult,
) error {
	state.mu.Lock()
	toolMessage := providertypes.Message{
		Role:         providertypes.RoleTool,
		Content:      result.Content,
		ToolCallID:   call.ID,
		IsError:      result.IsError,
		ToolMetadata: tools.SanitizeToolMetadata(result.Name, result.Metadata),
	}
	state.session.Messages = append(state.session.Messages, toolMessage)
	state.touchSession()
	sessionSnapshot := cloneSessionForPersistence(state.session)
	state.mu.Unlock()
	return s.sessionStore.Save(ctx, &sessionSnapshot)
}

// cloneSessionForPersistence 复制会话快照，避免持久化阶段与并发写入共享可变切片/映射。
func cloneSessionForPersistence(session agentsession.Session) agentsession.Session {
	cloned := session
	cloned.Messages = cloneMessagesForPersistence(session.Messages)
	cloned.TaskState = session.TaskState.Clone()
	cloned.Todos = cloneTodosForPersistence(session.Todos)
	return cloned
}

// cloneMessagesForPersistence 深拷贝消息切片及其嵌套字段，确保 Save 期间读取稳定。
func cloneMessagesForPersistence(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]providertypes.Message, len(messages))
	for idx, message := range messages {
		next := message
		if len(message.ToolCalls) > 0 {
			next.ToolCalls = append([]providertypes.ToolCall(nil), message.ToolCalls...)
		} else {
			next.ToolCalls = nil
		}
		if len(message.ToolMetadata) > 0 {
			next.ToolMetadata = make(map[string]string, len(message.ToolMetadata))
			for key, value := range message.ToolMetadata {
				next.ToolMetadata[key] = value
			}
		} else {
			next.ToolMetadata = nil
		}
		cloned[idx] = next
	}
	return cloned
}

// cloneTodosForPersistence 深拷贝 Todo 列表，确保持久化快照不与运行态共享底层切片。
func cloneTodosForPersistence(todos []agentsession.TodoItem) []agentsession.TodoItem {
	if len(todos) == 0 {
		return nil
	}
	cloned := make([]agentsession.TodoItem, len(todos))
	for idx, item := range todos {
		cloned[idx] = item.Clone()
	}
	return cloned
}
