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
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
	inputTokens int,
	outputTokens int,
) error {
	if err := s.persistAssistantTurnUsageAndMetadata(ctx, state, snapshot, inputTokens, outputTokens); err != nil {
		return err
	}
	if assistant.IsEmpty() {
		return nil
	}
	return s.appendAssistantMessageOnlyAndSave(ctx, state, assistant)
}

// persistAssistantTurnUsageAndMetadata 仅持久化 provider/model 与 token 使用量，不写入 assistant 文本。
func (s *Service) persistAssistantTurnUsageAndMetadata(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	inputTokens int,
	outputTokens int,
) error {
	metadataChanged := state.session.Provider != snapshot.ProviderConfig.Name || state.session.Model != snapshot.Model
	unknownUsageChanged := false
	state.session.Provider = snapshot.ProviderConfig.Name
	state.session.Model = snapshot.Model
	previousUnknownUsage := state.session.HasUnknownUsage
	state.recordUsage(inputTokens, outputTokens)
	if state.session.HasUnknownUsage != previousUnknownUsage {
		unknownUsageChanged = true
	}

	if metadataChanged || unknownUsageChanged || inputTokens != 0 || outputTokens != 0 {
		state.touchSession()
		return s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session))
	}
	return nil
}

// appendAssistantMessageOnlyAndSave 仅把 assistant 消息写入 transcript，不重复累计 token 与元数据。
func (s *Service) appendAssistantMessageOnlyAndSave(
	ctx context.Context,
	state *runState,
	assistant providertypes.Message,
) error {
	if state == nil || assistant.IsEmpty() {
		return nil
	}
	state.session.Messages = append(state.session.Messages, assistant)
	state.touchSession()
	return s.sessionStore.AppendMessages(ctx, agentsession.AppendMessagesInput{
		SessionID:       state.session.ID,
		Messages:        []providertypes.Message{assistant},
		UpdatedAt:       state.session.UpdatedAt,
		Provider:        state.session.Provider,
		Model:           state.session.Model,
		Workdir:         state.session.Workdir,
		HasUnknownUsage: state.session.HasUnknownUsage,
	})
}

// appendSystemMessageAndSave 将系统提醒消息追加到会话并持久化。
func (s *Service) appendSystemMessageAndSave(ctx context.Context, state *runState, text string) error {
	if state == nil {
		return nil
	}
	message := providertypes.Message{
		Role: providertypes.RoleSystem,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart(strings.TrimSpace(text)),
		},
	}
	state.session.Messages = append(state.session.Messages, message)
	state.touchSession()
	return s.sessionStore.AppendMessages(ctx, agentsession.AppendMessagesInput{
		SessionID:       state.session.ID,
		Messages:        []providertypes.Message{message},
		UpdatedAt:       state.session.UpdatedAt,
		Provider:        state.session.Provider,
		Model:           state.session.Model,
		Workdir:         state.session.Workdir,
		HasUnknownUsage: state.session.HasUnknownUsage,
	})
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
		SessionID:       state.session.ID,
		Messages:        []providertypes.Message{toolMessage},
		UpdatedAt:       state.session.UpdatedAt,
		Provider:        state.session.Provider,
		Model:           state.session.Model,
		Workdir:         state.session.Workdir,
		HasUnknownUsage: state.session.HasUnknownUsage,
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
	isError := result.IsError || toolResultMarkedFailed(result.Metadata)
	if !isError && strings.TrimSpace(content) == "" && !hasNonToolNameToolMetadata(sanitizedMetadata) {
		content = "ok"
	}
	if isError && strings.TrimSpace(content) == "" {
		content = "tool execution failed (ok=false)"
	}

	return providertypes.Message{
		Role:         providertypes.RoleTool,
		Parts:        []providertypes.ContentPart{providertypes.NewTextPart(content)},
		ToolCallID:   call.ID,
		IsError:      isError,
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

// toolResultMarkedFailed 根据工具元数据中的 ok 字段判断是否应强制标记为失败。
func toolResultMarkedFailed(metadata map[string]any) bool {
	return tools.ToolResultMetadataMarksFailure(metadata)
}

// createSessionInputFromSession 将运行态 session 转为建库时使用的会话头输入。
func createSessionInputFromSession(session agentsession.Session) agentsession.CreateSessionInput {
	return agentsession.CreateSessionInput{
		ID:        session.ID,
		Title:     session.Title,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Head:      session.HeadSnapshot(),
	}
}

// sessionStateInputFromSession 将运行态 session 映射为只更新会话头的持久化输入。
func sessionStateInputFromSession(session agentsession.Session) agentsession.UpdateSessionStateInput {
	return agentsession.UpdateSessionStateInput{
		SessionID: session.ID,
		Title:     session.Title,
		UpdatedAt: session.UpdatedAt,
		Head:      session.HeadSnapshot(),
	}
}

// replaceTranscriptInputFromSession 将完整 session 映射为 transcript 原子替换输入。
func replaceTranscriptInputFromSession(session agentsession.Session) agentsession.ReplaceTranscriptInput {
	return agentsession.ReplaceTranscriptInput{
		SessionID: session.ID,
		Messages:  cloneMessagesForPersistence(session.Messages),
		UpdatedAt: session.UpdatedAt,
		Head:      session.HeadSnapshot(),
	}
}

// cloneSessionForPersistence 复制会话快照，避免并发读写共享底层切片和映射。
func cloneSessionForPersistence(session agentsession.Session) agentsession.Session {
	cloned := session
	cloned.Messages = cloneMessagesForPersistence(session.Messages)
	cloned.TaskState = session.TaskState.Clone()
	cloned.ActivatedSkills = agentsessionCloneSkillActivations(session.ActivatedSkills)
	cloned.Todos = cloneTodosForPersistence(session.Todos)
	cloned.CurrentPlan = session.CurrentPlan.Clone()
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
