package runtime

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const toolNameMetadataKey = "tool_name"

// appendUserMessageAndSave 将用户消息追加到会话，并立即落盘为一条增量消息。
func (s *Service) appendUserMessageAndSave(ctx context.Context, state *runState, parts []providertypes.ContentPart) error {
	message := providertypes.Message{
		Role:  providertypes.RoleUser,
		Parts: parts,
	}
	state.session.Messages = append(state.session.Messages, message)
	state.touchSession()
	if err := s.sessionStore.AppendMessages(ctx, agentsession.AppendMessagesInput{
		SessionID: state.session.ID,
		Messages:  []providertypes.Message{message},
		UpdatedAt: state.session.UpdatedAt,
		Provider:  state.session.Provider,
		Model:     state.session.Model,
		Workdir:   state.session.Workdir,
	}); err != nil {
		return err
	}
	s.emitRunScoped(ctx, EventUserMessage, state, message)
	return nil
}

// appendAssistantMessageAndSave 将 assistant 消息和本轮 token/provider/model 增量写回会话。
func (s *Service) appendAssistantMessageAndSave(
	ctx context.Context,
	state *runState,
	snapshot turnSnapshot,
	assistant providertypes.Message,
	inputTokens int,
	outputTokens int,
) error {
	metadataChanged := state.session.Provider != snapshot.providerConfig.Name || state.session.Model != snapshot.model
	state.session.Provider = snapshot.providerConfig.Name
	state.session.Model = snapshot.model
	state.recordUsage(inputTokens, outputTokens)

	if !assistant.IsEmpty() {
		state.session.Messages = append(state.session.Messages, assistant)
		state.touchSession()
		return s.sessionStore.AppendMessages(ctx, agentsession.AppendMessagesInput{
			SessionID:        state.session.ID,
			Messages:         []providertypes.Message{assistant},
			UpdatedAt:        state.session.UpdatedAt,
			Provider:         state.session.Provider,
			Model:            state.session.Model,
			Workdir:          state.session.Workdir,
			TokenInputDelta:  inputTokens,
			TokenOutputDelta: outputTokens,
		})
	}

	if metadataChanged || inputTokens != 0 || outputTokens != 0 {
		state.touchSession()
		return s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session))
	}
	return nil
}

// appendToolMessageAndSave 将工具原始结果写回会话，持久化时仅追加一条 tool message。
func (s *Service) appendToolMessageAndSave(
	ctx context.Context,
	state *runState,
	call providertypes.ToolCall,
	result tools.ToolResult,
) error {
	state.mu.Lock()
	toolMessage := normalizeToolMessageForPersistence(call, result)
	state.session.Messages = append(state.session.Messages, toolMessage)
	state.touchSession()
	input := agentsession.AppendMessagesInput{
		SessionID: state.session.ID,
		Messages:  []providertypes.Message{toolMessage},
		UpdatedAt: state.session.UpdatedAt,
		Provider:  state.session.Provider,
		Model:     state.session.Model,
		Workdir:   state.session.Workdir,
	}
	state.mu.Unlock()
	return s.sessionStore.AppendMessages(ctx, input)
}

// normalizeToolMessageForPersistence 负责在写入会话前收敛工具结果，避免成功结果落成完全空语义消息。
func normalizeToolMessageForPersistence(call providertypes.ToolCall, result tools.ToolResult) providertypes.Message {
	toolName := strings.TrimSpace(result.Name)
	if toolName == "" {
		toolName = strings.TrimSpace(call.Name)
	}

	sanitizedMetadata := tools.SanitizeToolMetadata(toolName, result.Metadata)
	content := result.Content
	if !result.IsError && strings.TrimSpace(content) == "" && !hasNonToolNameToolMetadata(sanitizedMetadata) {
		content = "ok"
	}

	return providertypes.Message{
		Role:         providertypes.RoleTool,
		Parts:        []providertypes.ContentPart{providertypes.NewTextPart(content)},
		ToolCallID:   call.ID,
		IsError:      result.IsError,
		ToolMetadata: sanitizedMetadata,
	}
}

// hasNonToolNameToolMetadata 判断 metadata 中是否存在除 tool_name 外的语义字段。
func hasNonToolNameToolMetadata(metadata map[string]string) bool {
	for key := range metadata {
		if key != toolNameMetadataKey {
			return true
		}
	}
	return false
}

// createSessionInputFromSession 将运行态 session 转为建库时使用的会话头输入。
func createSessionInputFromSession(session agentsession.Session) agentsession.CreateSessionInput {
	return agentsession.CreateSessionInput{
		ID:               session.ID,
		Title:            session.Title,
		CreatedAt:        session.CreatedAt,
		UpdatedAt:        session.UpdatedAt,
		Provider:         session.Provider,
		Model:            session.Model,
		Workdir:          session.Workdir,
		TaskState:        session.TaskState.Clone(),
		ActivatedSkills:  agentsessionCloneSkillActivations(session.ActivatedSkills),
		Todos:            cloneTodosForPersistence(session.Todos),
		TokenInputTotal:  session.TokenInputTotal,
		TokenOutputTotal: session.TokenOutputTotal,
	}
}

// sessionStateInputFromSession 将运行态 session 映射为只更新会话头的持久化输入。
func sessionStateInputFromSession(session agentsession.Session) agentsession.UpdateSessionStateInput {
	return agentsession.UpdateSessionStateInput{
		SessionID:        session.ID,
		Title:            session.Title,
		UpdatedAt:        session.UpdatedAt,
		Provider:         session.Provider,
		Model:            session.Model,
		Workdir:          session.Workdir,
		TaskState:        session.TaskState.Clone(),
		ActivatedSkills:  agentsessionCloneSkillActivations(session.ActivatedSkills),
		Todos:            cloneTodosForPersistence(session.Todos),
		TokenInputTotal:  session.TokenInputTotal,
		TokenOutputTotal: session.TokenOutputTotal,
	}
}

// replaceTranscriptInputFromSession 将完整 session 映射为 transcript 原子替换输入。
func replaceTranscriptInputFromSession(session agentsession.Session) agentsession.ReplaceTranscriptInput {
	return agentsession.ReplaceTranscriptInput{
		SessionID:        session.ID,
		Messages:         cloneMessagesForPersistence(session.Messages),
		UpdatedAt:        session.UpdatedAt,
		Provider:         session.Provider,
		Model:            session.Model,
		Workdir:          session.Workdir,
		TaskState:        session.TaskState.Clone(),
		ActivatedSkills:  agentsessionCloneSkillActivations(session.ActivatedSkills),
		Todos:            cloneTodosForPersistence(session.Todos),
		TokenInputTotal:  session.TokenInputTotal,
		TokenOutputTotal: session.TokenOutputTotal,
	}
}

// cloneSessionForPersistence 复制会话快照，避免并发读写共享底层切片和映射。
func cloneSessionForPersistence(session agentsession.Session) agentsession.Session {
	cloned := session
	cloned.Messages = cloneMessagesForPersistence(session.Messages)
	cloned.TaskState = session.TaskState.Clone()
	cloned.ActivatedSkills = agentsessionCloneSkillActivations(session.ActivatedSkills)
	cloned.Todos = cloneTodosForPersistence(session.Todos)
	return cloned
}

// agentsessionCloneSkillActivations 深拷贝会话中的 skill 激活列表，避免共享底层切片。
func agentsessionCloneSkillActivations(items []agentsession.SkillActivation) []agentsession.SkillActivation {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]agentsession.SkillActivation, len(items))
	for idx, item := range items {
		cloned[idx] = item.Clone()
	}
	return cloned
}

// cloneMessagesForPersistence 深拷贝消息切片及其嵌套字段，确保持久化和测试读取稳定。
func cloneMessagesForPersistence(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]providertypes.Message, len(messages))
	for idx, message := range messages {
		next := message
		next.Parts = providertypes.CloneParts(message.Parts)
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

// cloneTodosForPersistence 深拷贝 Todo 列表，确保会话快照与运行态隔离。
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
