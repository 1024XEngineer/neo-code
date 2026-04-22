package runtime

import (
	"context"
	"math"
	"strconv"
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
	if len(metadata) == 0 {
		return false
	}
	if raw, exists := metadata["ok"]; exists {
		if ok, resolved := parseToolResultOK(raw); resolved {
			return !ok
		}
	}
	if rawExitCode, exists := metadata["exit_code"]; exists {
		if exitCode, resolved := parseToolResultExitCode(rawExitCode); resolved {
			return exitCode != 0
		}
	}
	return false
}

// parseToolResultOK 解析工具元数据里的 ok 字段，兼容 bool/数字/字符串等常见序列化形态。
func parseToolResultOK(raw any) (bool, bool) {
	switch value := raw.(type) {
	case bool:
		return value, true
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(value))
		switch trimmed {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n":
			return false, true
		default:
			return false, false
		}
	case int:
		return value != 0, true
	case int8:
		return value != 0, true
	case int16:
		return value != 0, true
	case int32:
		return value != 0, true
	case int64:
		return value != 0, true
	case uint:
		return value != 0, true
	case uint8:
		return value != 0, true
	case uint16:
		return value != 0, true
	case uint32:
		return value != 0, true
	case uint64:
		return value != 0, true
	case float32:
		return value != 0, true
	case float64:
		return value != 0, true
	default:
		return false, false
	}
}

// parseToolResultExitCode 解析工具元数据里的 exit_code 字段，兼容数字和字符串。
func parseToolResultExitCode(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case uint:
		return int(value), true
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		return int(value), true
	case uint64:
		return int(value), true
	case float32:
		return parseFloatExitCode(float64(value))
	case float64:
		return parseFloatExitCode(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// parseFloatExitCode 将浮点退出码折叠为稳定整数，避免 0<|x|<1 被截断为 0。
func parseFloatExitCode(value float64) (int, bool) {
	if value == 0 {
		return 0, true
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	parsed := int(value)
	if parsed == 0 {
		if value > 0 {
			return 1, true
		}
		return -1, true
	}
	return parsed, true
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
