package feishuadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type sdkEventClient interface {
	Start(ctx context.Context) error
}

var newSDKEventClient = func(cfg Config, handler IngressHandler) sdkEventClient {
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnCustomizedEvent("im.message.receive_v1", func(ctx context.Context, event *larkevent.EventReq) error {
			msg, ok := mapSDKMessageEvent(event)
			if !ok {
				return nil
			}
			return handler.HandleMessage(ctx, msg)
		}).
		OnCustomizedEvent("card.action.trigger", func(ctx context.Context, event *larkevent.EventReq) error {
			cardEvent, ok := mapSDKCardActionEvent(event)
			if !ok {
				return nil
			}
			return handler.HandleCardAction(ctx, cardEvent)
		})

	client := larkws.NewClient(cfg.AppID, cfg.AppSecret, larkws.WithEventHandler(eventHandler))
	return client
}

// SDKIngress 基于飞书 SDK 长连接接收事件，适合本地无公网接入场景。
type SDKIngress struct {
	cfg    Config
	logger func(string, ...any)
}

// NewSDKIngress 创建 SDK 长连接入站实现。
func NewSDKIngress(cfg Config, logger func(string, ...any)) Ingress {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	return &SDKIngress{
		cfg:    cfg,
		logger: logger,
	}
}

// Run 建立 SDK 长连接并将接收到的事件转发到统一 handler。
func (s *SDKIngress) Run(ctx context.Context, handler IngressHandler) error {
	client := newSDKEventClient(s.cfg, handler)
	if client == nil {
		return fmt.Errorf("create sdk event client: nil client")
	}
	err := client.Start(ctx)
	if err != nil && err != context.Canceled {
		s.logger("feishu sdk ingress stopped with error: %v", err)
	}
	return err
}

// mapSDKMessageEvent 将 SDK 消息事件转换为标准化消息事件。
func mapSDKMessageEvent(event *larkevent.EventReq) (FeishuMessageEvent, bool) {
	if event == nil || len(event.Body) == 0 {
		return FeishuMessageEvent{}, false
	}
	var envelope inboundEnvelope
	if err := json.Unmarshal(event.Body, &envelope); err != nil {
		return FeishuMessageEvent{}, false
	}
	if strings.TrimSpace(envelope.Header.EventType) != "im.message.receive_v1" {
		return FeishuMessageEvent{}, false
	}
	var payload inboundMessageEvent
	if err := json.Unmarshal(envelope.Event, &payload); err != nil {
		return FeishuMessageEvent{}, false
	}
	messageID := strings.TrimSpace(payload.Message.MessageID)
	chatID := strings.TrimSpace(payload.Message.ChatID)
	if messageID == "" || chatID == "" {
		return FeishuMessageEvent{}, false
	}
	return FeishuMessageEvent{
		EventID:     strings.TrimSpace(envelope.Header.EventID),
		MessageID:   messageID,
		ChatID:      chatID,
		ChatType:    firstNonEmpty(strings.TrimSpace(payload.Message.ChatType), strings.TrimSpace(payload.ChatType)),
		ContentText: extractSDKMessageText(strings.TrimSpace(payload.Message.Content)),
		HeaderAppID: strings.TrimSpace(envelope.Header.AppID),
		Mentions:    convertMentions(payload.Message.Mentions),
	}, true
}

// mapSDKCardActionEvent 尝试从 SDK 自定义事件中提取审批动作。
func mapSDKCardActionEvent(event *larkevent.EventReq) (FeishuCardActionEvent, bool) {
	if event == nil || len(event.Body) == 0 {
		return FeishuCardActionEvent{}, false
	}
	var payload struct {
		Header struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Action struct {
				Value map[string]string `json:"value"`
			} `json:"action"`
		} `json:"event"`
	}
	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return FeishuCardActionEvent{}, false
	}
	requestID := strings.TrimSpace(payload.Event.Action.Value["request_id"])
	decision := strings.TrimSpace(strings.ToLower(payload.Event.Action.Value["decision"]))
	if requestID == "" || (decision != "allow_once" && decision != "reject") {
		return FeishuCardActionEvent{}, false
	}
	return FeishuCardActionEvent{
		EventID:   strings.TrimSpace(payload.Header.EventID),
		RequestID: requestID,
		Decision:  decision,
	}, true
}

// extractSDKMessageText 从 SDK 消息 content JSON 里提取 text。
func extractSDKMessageText(content string) string {
	text, err := decodeMessageText(content)
	if err == nil {
		return text
	}
	return strings.TrimSpace(content)
}
