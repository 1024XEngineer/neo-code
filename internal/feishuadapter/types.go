package feishuadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Config 描述 Feishu Adapter 的运行配置。
type Config struct {
	ListenAddress          string
	EventPath              string
	CardPath               string
	AppID                  string
	AppSecret              string
	VerifyToken            string
	SigningSecret          string
	InsecureSkipSignVerify bool
	RequestTimeout         time.Duration
	IdempotencyTTL         time.Duration
	ReconnectBackoffMin    time.Duration
	ReconnectBackoffMax    time.Duration
	RebindInterval         time.Duration
}

// Validate 校验 Feishu Adapter 配置最小可用性。
func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddress) == "" {
		return fmt.Errorf("listen address is required")
	}
	if strings.TrimSpace(c.EventPath) == "" {
		return fmt.Errorf("event path is required")
	}
	if strings.TrimSpace(c.CardPath) == "" {
		return fmt.Errorf("card path is required")
	}
	if strings.TrimSpace(c.AppID) == "" {
		return fmt.Errorf("app id is required")
	}
	if strings.TrimSpace(c.AppSecret) == "" {
		return fmt.Errorf("app secret is required")
	}
	if strings.TrimSpace(c.VerifyToken) == "" {
		return fmt.Errorf("verify token is required")
	}
	if !c.InsecureSkipSignVerify && strings.TrimSpace(c.SigningSecret) == "" {
		return fmt.Errorf("signing secret is required unless insecure skip signature verify is enabled")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be greater than zero")
	}
	if c.IdempotencyTTL <= 0 {
		return fmt.Errorf("idempotency ttl must be greater than zero")
	}
	if c.ReconnectBackoffMin <= 0 || c.ReconnectBackoffMax <= 0 {
		return fmt.Errorf("reconnect backoff must be greater than zero")
	}
	if c.ReconnectBackoffMin > c.ReconnectBackoffMax {
		return fmt.Errorf("reconnect backoff min cannot exceed max")
	}
	if c.RebindInterval <= 0 {
		return fmt.Errorf("rebind interval must be greater than zero")
	}
	return nil
}

// GatewayNotification 表示网关推送的原始通知。
type GatewayNotification struct {
	Method string
	Params json.RawMessage
}

// GatewayClient 定义 Feishu Adapter 所需的网关最小调用能力。
type GatewayClient interface {
	Authenticate(ctx context.Context) error
	BindStream(ctx context.Context, sessionID string, runID string) error
	Run(ctx context.Context, sessionID string, runID string, inputText string) error
	ResolvePermission(ctx context.Context, requestID string, decision string) error
	Ping(ctx context.Context) error
	Notifications() <-chan GatewayNotification
	Close() error
}

// Messenger 定义飞书消息发送器接口，便于测试替换。
type Messenger interface {
	SendText(ctx context.Context, chatID string, text string) error
	SendPermissionCard(ctx context.Context, chatID string, payload PermissionCardPayload) error
}

// PermissionCardPayload 表示最小审批卡片的关键字段。
type PermissionCardPayload struct {
	RequestID string
	Message   string
}

// inboundEnvelope 表示飞书回调统一信封。
type inboundEnvelope struct {
	Type      string          `json:"type,omitempty"`
	Token     string          `json:"token,omitempty"`
	Challenge string          `json:"challenge,omitempty"`
	Header    inboundHeader   `json:"header,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
}

// inboundHeader 表示飞书 schema 2.0 回调头字段。
type inboundHeader struct {
	EventID   string `json:"event_id,omitempty"`
	EventType string `json:"event_type,omitempty"`
	Token     string `json:"token,omitempty"`
	AppID     string `json:"app_id,omitempty"`
}

// inboundMessageEvent 表示消息回调事件最小结构。
type inboundMessageEvent struct {
	ChatType string         `json:"chat_type,omitempty"`
	Message  inboundMessage `json:"message,omitempty"`
}

// inboundMessage 表示消息体结构。
type inboundMessage struct {
	MessageID string           `json:"message_id,omitempty"`
	ChatID    string           `json:"chat_id,omitempty"`
	ChatType  string           `json:"chat_type,omitempty"`
	Content   string           `json:"content,omitempty"`
	Mentions  []inboundMention `json:"mentions,omitempty"`
}

// inboundMention 表示群聊消息中的 @ 信息。
type inboundMention struct {
	Name string           `json:"name,omitempty"`
	Key  string           `json:"key,omitempty"`
	ID   inboundMentionID `json:"id,omitempty"`
}

// inboundMentionID 表示消息 @ 目标身份信息，用于判断是否 @ 到当前机器人应用。
type inboundMentionID struct {
	OpenID  string `json:"open_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	UnionID string `json:"union_id,omitempty"`
	AppID   string `json:"app_id,omitempty"`
}

// inboundMessageContent 表示消息 JSON 内容中的文本字段。
type inboundMessageContent struct {
	Text string `json:"text,omitempty"`
}

// inboundCardCallback 表示卡片回调最小结构。
type inboundCardCallback struct {
	OpenMessageID string            `json:"open_message_id,omitempty"`
	Token         string            `json:"token,omitempty"`
	Action        inboundCardAction `json:"action,omitempty"`
	Header        inboundCardHeader `json:"header,omitempty"`
	Event         *inboundCardEvent `json:"event,omitempty"`
}

type inboundCardHeader struct {
	EventID string `json:"event_id,omitempty"`
	Token   string `json:"token,omitempty"`
}

type inboundCardEvent struct {
	Operator inboundCardOperator `json:"operator,omitempty"`
}

type inboundCardOperator struct {
	OpenID string `json:"open_id,omitempty"`
}

type inboundCardAction struct {
	Value map[string]string `json:"value,omitempty"`
}
