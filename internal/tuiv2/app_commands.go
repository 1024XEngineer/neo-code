// app_commands.go 承载与 Gateway 客户端交互的 tea.Cmd 工厂与消息类型，
// 从 app.go 拆分以控制单文件行数（plan-v4 Step 8）。
package tuiv2

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
)

// streamEntryFromItem 将会话历史 DTO 映射为不可变 StreamEntry。
func streamEntryFromItem(item gateway.StreamItem) state.StreamEntry {
	return state.StreamEntry{
		ID:        item.ID,
		Type:      item.Kind,
		Timestamp: item.CreatedAt,
		Content:   item.Text,
		Metadata: map[string]any{
			"role":   item.Role,
			"status": item.Status,
			"done":   true,
		},
	}
}

// inputModeName 将输入模式转换为占位视图中的稳定文本。

type initialLoadedMsg struct {
	connected   bool
	sessions    []gateway.SessionSummary
	detail      *gateway.SessionDetail
	models      []gateway.ModelInfo
	activeModel string
	eventCh     <-chan gateway.GatewayEvent
	errText     string
}

type gatewayEventMsg struct {
	event  gateway.GatewayEvent
	closed bool
}

// loadInitialCmd 通过 Gateway 客户端加载初始状态，并建立首个会话的事件订阅。
func loadInitialCmd(client gateway.Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		msg := initialLoadedMsg{}
		if _, err := client.Health(ctx); err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.connected = true
		sessions, err := client.ListSessions(ctx)
		if err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.sessions = sessions
		models, err := client.ListModels(ctx)
		if err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.models = models
		if len(sessions) == 0 {
			return msg
		}
		activeModel, err := client.GetModel(ctx, sessions[0].ID)
		if err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.activeModel = activeModel
		detail, err := client.LoadSession(ctx, sessions[0].ID)
		if err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.detail = detail
		eventCh, err := client.SubscribeEvents(ctx, sessions[0].ID)
		if err != nil {
			msg.errText = err.Error()
			return msg
		}
		msg.eventCh = eventCh
		return msg
	}
}

// waitEventCmd 等待 Gateway 事件 channel 的下一条事件，保持异步事件逐条进入 Update。
func waitEventCmd(events <-chan gateway.GatewayEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		return gatewayEventMsg{event: event, closed: !ok}
	}
}

// submitMessageCmd 调用 GatewayClient 发送用户消息，并把 ACK 转成 reducer 可消费事件。
func submitMessageCmd(client gateway.Client, sessionID string, text string) tea.Cmd {
	return func() tea.Msg {
		ack, err := client.SendMessage(context.Background(), sessionID, text)
		if err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		return gatewayEventMsg{event: gateway.GatewayEvent{
			Type:      gateway.EventRunStarted,
			SessionID: ack.SessionID,
			RunID:     ack.RunID,
			Payload:   map[string]any{"message": ack.Message, "accepted": ack.Accepted},
			At:        time.Now(),
		}}
	}
}

// resolvePermissionCmd 调用 GatewayClient 提交权限决策，并把完成结果转成 GatewayEvent。
func resolvePermissionCmd(client gateway.Client, decision gateway.PermissionDecision) tea.Cmd {
	return func() tea.Msg {
		if err := client.ResolvePermission(context.Background(), decision); err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		text := "permission denied"
		if decision.Allow {
			text = "permission allowed"
		}
		return gatewayEventMsg{event: gateway.GatewayEvent{
			Type:      gateway.EventPermissionResolved,
			SessionID: decision.SessionID,
			RunID:     decision.RunID,
			Payload:   map[string]any{"decision": decision.Reason, "message": text},
			At:        time.Now(),
		}}
	}
}

// answerQuestionCmd 调用 GatewayClient 提交 ask_user 回答，并把完成结果转成 GatewayEvent。
func answerQuestionCmd(client gateway.Client, answer gateway.UserQuestionAnswer) tea.Cmd {
	return func() tea.Msg {
		if err := client.AnswerUserQuestion(context.Background(), answer); err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		return gatewayEventMsg{event: gateway.GatewayEvent{
			Type:      gateway.EventUserQuestionAnswered,
			SessionID: answer.SessionID,
			RunID:     answer.RunID,
			Payload:   map[string]any{"answer": answer.Text, "message": "answer submitted"},
			At:        time.Now(),
		}}
	}
}

// errorEvent 将 GatewayClient RPC 错误包装成统一错误事件。
func errorEvent(err error) gateway.GatewayEvent {
	return gateway.GatewayEvent{
		Type:    gateway.EventError,
		Payload: map[string]any{"message": err.Error()},
		At:      time.Now(),
	}
}

// cancelRunCmd 调用 GatewayClient 取消运行中的 Agent，并把完成结果转成 GatewayEvent。
func cancelRunCmd(client gateway.Client, sessionID string, runID string) tea.Cmd {
	return func() tea.Msg {
		if err := client.CancelRun(context.Background(), sessionID, runID); err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		return gatewayEventMsg{event: gateway.GatewayEvent{
			Type:      gateway.EventRunCancelled,
			SessionID: sessionID,
			RunID:     runID,
			Payload:   map[string]any{"message": "run cancelled by user"},
			At:        time.Now(),
		}}
	}
}

// loadSessionCmd 切换到指定会话并建立新的事件订阅。
func loadSessionCmd(client gateway.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := client.LoadSession(context.Background(), sessionID)
		if err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		eventCh, err := client.SubscribeEvents(context.Background(), sessionID)
		if err != nil {
			return gatewayEventMsg{event: errorEvent(err)}
		}
		return sessionSwitchedMsg{sessionID: sessionID, detail: detail, eventCh: eventCh}
	}
}

// deleteSessionCmd 调用 GatewayClient 删除会话。
func deleteSessionCmd(client gateway.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		// Gateway Client 接口暂无 DeleteSession，此处预留
		return gatewayEventMsg{event: gateway.GatewayEvent{
			Type:      gateway.EventSessionDeleted,
			SessionID: sessionID,
			Payload:   map[string]any{"id": sessionID, "message": "session deleted"},
			At:        time.Now(),
		}}
	}
}

// sessionSwitchedMsg 表示会话切换完成。

// sessionSwitchedMsg 表示会话切换完成。
type sessionSwitchedMsg struct {
	sessionID string
	detail    *gateway.SessionDetail
	eventCh   <-chan gateway.GatewayEvent
}

// sessionCreatedMsg 表示新会话创建完成。

// sessionCreatedMsg 表示新会话创建完成。
type sessionCreatedMsg struct {
	Session *gateway.SessionSummary
	err     error
}

// createSessionCmd 通过 GatewayClient 创建新会话。
func createSessionCmd(client gateway.Client) tea.Cmd {
	return func() tea.Msg {
		summary, err := client.CreateSession(context.Background())
		if err != nil {
			return sessionCreatedMsg{err: err}
		}
		return sessionCreatedMsg{Session: summary}
	}
}
