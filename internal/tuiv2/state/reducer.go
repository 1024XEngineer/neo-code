package state

import (
	"fmt"
	"time"

	"neo-code/internal/tuiv2/gateway"
)

// Reduce 将 GatewayEvent 纯函数式地映射为新的 ViewState，不修改输入状态。
func Reduce(current *ViewState, event gateway.GatewayEvent) *ViewState {
	next := cloneViewState(current)
	switch event.Type {
	case gateway.EventAgentChunk, gateway.EventAssistantDelta:
		return reduceAgentChunk(next, event)
	case gateway.EventAgentMessageStart:
		return appendStream(next, streamEntry(event, "message", payloadString(event.Payload, "text", "content", "message")))
	case gateway.EventAgentMessageEnd:
		return reduceAgentMessageEnd(next, event)
	case gateway.EventToolStart, gateway.EventToolStarted:
		return reduceToolStart(next, event)
	case gateway.EventToolEnd, gateway.EventToolFinished:
		return reduceToolEnd(next, event)
	case gateway.EventToolOutput:
		return appendStream(next, streamEntry(event, "tool_output", payloadString(event.Payload, "text", "output", "content")))
	case gateway.EventPermissionRequested:
		return reducePermissionRequested(next, event)
	case gateway.EventPermissionResolved:
		next.Runtime.Phase = RuntimePhaseRunning
		next.Input.Mode = InputStateModeMessage
		next.Input.Prompt = ""
		next.Input.Options = nil
		return appendStream(next, streamEntry(event, "status", payloadString(event.Payload, "message", "decision", "status")))
	case gateway.EventAskUserQuestion, gateway.EventUserQuestionRequested:
		return reduceAskUserQuestion(next, event)
	case gateway.EventUserQuestionAnswered:
		next.Runtime.Phase = RuntimePhaseRunning
		next.Input.Mode = InputStateModeMessage
		next.Input.Text = ""
		next.Input.Cursor = 0
		next.Input.Prompt = ""
		next.Input.Options = nil
		return appendStream(next, streamEntry(event, "status", payloadString(event.Payload, "message", "answer", "text")))
	case gateway.EventPhaseChanged:
		next.Runtime.Phase = payloadString(event.Payload, "phase", "status")
	case gateway.EventRunStarted:
		next.Runtime.Phase = RuntimePhaseRunning
		next.Runtime.RunID = event.RunID
	case gateway.EventRunFinished:
		if next.Runtime.Phase != RuntimePhaseError && next.Runtime.Phase != RuntimePhaseCancelled {
			next.Runtime.Phase = RuntimePhaseIdle
		}
		next.Runtime.Tokens = tokenUsageFromPayload(event.Payload, next.Runtime.Tokens)
	case gateway.EventRunError, gateway.EventError:
		next.Runtime.Phase = RuntimePhaseError
		return appendStream(next, streamEntry(event, "error", payloadString(event.Payload, "message", "error", "text")))
	case gateway.EventRunCancelled:
		next.Runtime.Phase = RuntimePhaseCancelled
		next.Input.Mode = InputStateModeMessage
		next.Input.Prompt = ""
		next.Input.Options = nil
		return appendStream(next, streamEntry(event, "status", payloadString(event.Payload, "message", "phase", "status")))
	case gateway.EventTokenUsage:
		next.Runtime.Tokens = tokenUsageFromPayload(event.Payload, next.Runtime.Tokens)
	case gateway.EventSessionCreated:
		next.Gateway.Sessions = append(next.Gateway.Sessions, sessionFromPayload(event.Payload))
	case gateway.EventSessionDeleted:
		next.Gateway.Sessions = deleteSession(next.Gateway.Sessions, payloadString(event.Payload, "id", "session_id"))
	case gateway.EventSessionUpdated:
		next.Gateway.Sessions = upsertSession(next.Gateway.Sessions, sessionFromPayload(event.Payload))
	case gateway.EventModelChanged:
		next.Gateway.ActiveModel = payloadString(event.Payload, "model_id", "model", "id")
	case gateway.EventHealthChanged:
		next.Gateway.Connected = payloadBool(event.Payload, "connected", "ok")
	case gateway.EventGatewayOffline:
		next.Gateway.Connected = false
		next.Runtime.Phase = RuntimePhaseError
		return appendStream(next, streamEntry(event, "error", payloadString(event.Payload, "message", "error")))
	}
	return next
}

// reduceAgentChunk 合并助手流式文本，最后一条未完成消息可被增量追加。
func reduceAgentChunk(next *ViewState, event gateway.GatewayEvent) *ViewState {
	text := payloadString(event.Payload, "text", "delta", "content")
	if len(next.Stream) > 0 {
		last := next.Stream[len(next.Stream)-1]
		if last.Type == "message" && !streamEntryDone(last) {
			updated := last
			updated.Content += text
			updated.Metadata = cloneMetadata(last.Metadata)
			updated.Metadata["done"] = false
			if _, ok := updated.Metadata["role"].(string); !ok {
				updated.Metadata["role"] = "assistant"
			}
			stream := append([]StreamEntry(nil), next.Stream[:len(next.Stream)-1]...)
			stream = append(stream, updated)
			next.Stream = stream
			return next
		}
	}
	entry := streamEntry(event, "message", text)
	entry.Metadata["done"] = false
	entry.Metadata["role"] = "assistant"
	return appendStream(next, entry)
}

// reduceAgentMessageEnd 标记最后一条消息完成，并同步 token 用量。
func reduceAgentMessageEnd(next *ViewState, event gateway.GatewayEvent) *ViewState {
	if len(next.Stream) > 0 {
		last := next.Stream[len(next.Stream)-1]
		if last.Type == "message" {
			updated := last
			updated.Metadata = cloneMetadata(last.Metadata)
			updated.Metadata["done"] = true
			stream := append([]StreamEntry(nil), next.Stream[:len(next.Stream)-1]...)
			stream = append(stream, updated)
			next.Stream = stream
		}
	}
	next.Runtime.Tokens = tokenUsageFromPayload(event.Payload, next.Runtime.Tokens)
	return next
}

// reduceToolStart 追加工具开始条目，保留工具名和输入摘要。
func reduceToolStart(next *ViewState, event gateway.GatewayEvent) *ViewState {
	entry := streamEntry(event, "tool_start", payloadString(event.Payload, "command", "input", "text"))
	entry.ToolName = payloadString(event.Payload, "tool", "tool_name", "name")
	entry.ToolInput = payloadString(event.Payload, "input", "command")
	return appendStream(next, entry)
}

// reduceToolEnd 追加工具结束条目，记录输出或状态摘要。
func reduceToolEnd(next *ViewState, event gateway.GatewayEvent) *ViewState {
	entry := streamEntry(event, "tool_end", payloadString(event.Payload, "output", "content", "status", "text"))
	entry.ToolName = payloadString(event.Payload, "tool", "tool_name", "name")
	return appendStream(next, entry)
}

// reducePermissionRequested 进入权限等待态，并追加权限状态条目。
func reducePermissionRequested(next *ViewState, event gateway.GatewayEvent) *ViewState {
	next.Runtime.Phase = RuntimePhaseWaitingPermission
	next.Input.Mode = InputStateModePermissionResponse
	next.Input.Prompt = payloadString(event.Payload, "prompt", "message", "tool")
	return appendStream(next, streamEntry(event, "permission", next.Input.Prompt))
}

// reduceAskUserQuestion 进入用户问答态，并更新输入区提示和选项。
func reduceAskUserQuestion(next *ViewState, event gateway.GatewayEvent) *ViewState {
	next.Runtime.Phase = RuntimePhaseWaitingUser
	next.Input.Mode = InputStateModeQuestionAnswer
	next.Input.Prompt = payloadString(event.Payload, "question", "prompt", "message")
	next.Input.Options = payloadStringSlice(event.Payload, "options")
	return appendStream(next, streamEntry(event, "question", next.Input.Prompt))
}

// cloneViewState 复制 ViewState 的顶层结构和切片，避免 reducer 修改输入状态。
func cloneViewState(current *ViewState) *ViewState {
	if current == nil {
		return NewViewState()
	}
	next := *current
	next.Gateway.Sessions = append([]gateway.SessionSummary(nil), current.Gateway.Sessions...)
	next.Gateway.Models = append([]gateway.ModelInfo(nil), current.Gateway.Models...)
	if current.Gateway.ActiveSess != nil {
		active := *current.Gateway.ActiveSess
		next.Gateway.ActiveSess = &active
	}
	next.Stream = append([]StreamEntry(nil), current.Stream...)
	next.Input.Options = append([]string(nil), current.Input.Options...)
	next.Input.History = append([]string(nil), current.Input.History...)
	next.Search.Matches = append([]int(nil), current.Search.Matches...)
	return &next
}

// appendStream 使用新 slice 追加流条目，保持历史序列不可变。
func appendStream(next *ViewState, entry StreamEntry) *ViewState {
	next.Stream = append(append([]StreamEntry(nil), next.Stream...), entry)
	return next
}

// streamEntry 将 Gateway 事件转换为 StreamEntry 的通用构造。
func streamEntry(event gateway.GatewayEvent, entryType string, content string) StreamEntry {
	metadata := clonePayload(event.Payload)
	if entryType == "message" {
		if _, ok := metadata["role"].(string); !ok {
			metadata["role"] = "assistant"
		}
	}
	return StreamEntry{
		ID:        payloadString(event.Payload, "id", "entry_id", "message_id"),
		Type:      entryType,
		Timestamp: eventTime(event.At),
		Content:   content,
		Metadata:  metadata,
	}
}

// streamEntryDone 判断消息条目是否已完成，完成后不再合并 chunk。
func streamEntryDone(entry StreamEntry) bool {
	done, ok := entry.Metadata["done"].(bool)
	return ok && done
}

// tokenUsageFromPayload 从事件 payload 中提取 token 用量，缺省字段沿用旧值。
func tokenUsageFromPayload(payload map[string]any, fallback TokenUsage) TokenUsage {
	return TokenUsage{
		Input:  payloadInt(payload, fallback.Input, "input", "input_tokens"),
		Output: payloadInt(payload, fallback.Output, "output", "output_tokens"),
		Total:  payloadInt(payload, fallback.Total, "total", "total_tokens", "tokens"),
	}
}

// sessionFromPayload 从事件 payload 中提取会话摘要 DTO。
func sessionFromPayload(payload map[string]any) gateway.SessionSummary {
	return gateway.SessionSummary{
		ID:    payloadString(payload, "id", "session_id"),
		Title: payloadString(payload, "title", "name"),
		Mode:  payloadString(payload, "mode"),
		Model: payloadString(payload, "model", "model_id"),
	}
}

// deleteSession 返回移除指定会话后的新会话列表。
func deleteSession(sessions []gateway.SessionSummary, id string) []gateway.SessionSummary {
	next := make([]gateway.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if session.ID != id {
			next = append(next, session)
		}
	}
	return next
}

// upsertSession 返回更新或追加会话摘要后的新会话列表。
func upsertSession(sessions []gateway.SessionSummary, updated gateway.SessionSummary) []gateway.SessionSummary {
	if updated.ID == "" {
		return append([]gateway.SessionSummary(nil), sessions...)
	}
	next := append([]gateway.SessionSummary(nil), sessions...)
	for index, session := range next {
		if session.ID == updated.ID {
			next[index] = updated
			return next
		}
	}
	return append(next, updated)
}

// payloadString 按候选键从 payload 中读取字符串化值。
func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case fmt.Stringer:
			return typed.String()
		case nil:
			continue
		default:
			return fmt.Sprint(typed)
		}
	}
	return ""
}

// payloadInt 按候选键从 payload 中读取整数值。
func payloadInt(payload map[string]any, fallback int, keys ...string) int {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	return fallback
}

// payloadBool 按候选键从 payload 中读取布尔值。
func payloadBool(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if typed, ok := value.(bool); ok {
			return typed
		}
	}
	return false
}

// payloadStringSlice 从 payload 中读取字符串切片。
func payloadStringSlice(payload map[string]any, key string) []string {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

// clonePayload 复制事件 payload，避免 StreamEntry 持有共享 map。
func clonePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	clone := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}

// cloneMetadata 复制条目 metadata，用于合并流式文本时保留不可变语义。
func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

// eventTime 为缺少时间戳的事件补充当前时间。
func eventTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now()
	}
	return at
}
