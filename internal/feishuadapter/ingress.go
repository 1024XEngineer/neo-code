package feishuadapter

import "context"

// Ingress 定义飞书事件入站接口，不同传输模式共享 Adapter Core 处理逻辑。
type Ingress interface {
	// Run 启动入站消费并将标准化事件回调到 handler。
	Run(ctx context.Context, handler IngressHandler) error
}

// IngressHandler 定义标准化飞书事件处理接口，由 Adapter Core 实现。
type IngressHandler interface {
	// HandleMessage 处理标准化后的飞书消息事件。
	HandleMessage(ctx context.Context, event FeishuMessageEvent) error
	// HandleCardAction 处理标准化后的审批动作事件。
	HandleCardAction(ctx context.Context, event FeishuCardActionEvent) error
}

// FeishuMessageEvent 表示标准化后的飞书消息事件。
type FeishuMessageEvent struct {
	EventID     string
	MessageID   string
	ChatID      string
	ChatType    string
	ContentText string
	HeaderAppID string
	Mentions    []FeishuMention
}

// FeishuMention 表示消息中的单个 @ 目标身份信息。
type FeishuMention struct {
	AppID   string
	UserID  string
	OpenID  string
	UnionID string
}

// FeishuCardActionEvent 表示标准化后的审批动作事件。
type FeishuCardActionEvent struct {
	EventID   string
	RequestID string
	Decision  string
}
