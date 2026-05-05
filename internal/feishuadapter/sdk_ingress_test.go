package feishuadapter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

type fakeSDKEventClient struct {
	startFn func(ctx context.Context) error
}

func (f *fakeSDKEventClient) Start(ctx context.Context) error {
	if f.startFn != nil {
		return f.startFn(ctx)
	}
	return nil
}

type captureIngressHandler struct {
	messages []FeishuMessageEvent
	cards    []FeishuCardActionEvent
}

func (c *captureIngressHandler) HandleMessage(_ context.Context, event FeishuMessageEvent) error {
	c.messages = append(c.messages, event)
	return nil
}

func (c *captureIngressHandler) HandleCardAction(_ context.Context, event FeishuCardActionEvent) error {
	c.cards = append(c.cards, event)
	return nil
}

func TestSDKIngressRunDispatchesMessageToHandler(t *testing.T) {
	originalFactory := newSDKEventClient
	t.Cleanup(func() { newSDKEventClient = originalFactory })

	handler := &captureIngressHandler{}
	newSDKEventClient = func(cfg Config, ingressHandler IngressHandler) sdkEventClient {
		return &fakeSDKEventClient{
			startFn: func(ctx context.Context) error {
				return ingressHandler.HandleMessage(ctx, FeishuMessageEvent{
					EventID:     "evt-1",
					MessageID:   "msg-1",
					ChatID:      "chat-1",
					ChatType:    "p2p",
					ContentText: "hello",
					HeaderAppID: cfg.AppID,
				})
			},
		}
	}

	ingress := NewSDKIngress(Config{
		IngressMode:         IngressModeSDK,
		AppID:               "app",
		AppSecret:           "secret",
		RequestTimeout:      3 * time.Second,
		IdempotencyTTL:      time.Minute,
		ReconnectBackoffMin: time.Second,
		ReconnectBackoffMax: 2 * time.Second,
		RebindInterval:      3 * time.Second,
	}, nil)
	if err := ingress.Run(context.Background(), handler); err != nil {
		t.Fatalf("run sdk ingress: %v", err)
	}
	if len(handler.messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(handler.messages))
	}
	if handler.messages[0].MessageID != "msg-1" {
		t.Fatalf("message id = %q, want msg-1", handler.messages[0].MessageID)
	}
}

func TestMapSDKMessageEventSupportsGroupMessage(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"header": map[string]any{
			"event_id":   "evt-group",
			"event_type": "im.message.receive_v1",
			"app_id":     "cli_app",
		},
		"event": map[string]any{
			"chat_type": "group",
			"message": map[string]any{
				"message_id": "msg-group",
				"chat_id":    "chat-group",
				"chat_type":  "group",
				"content":    `{"text":"<at user_id=\"ou_bot\">neo</at> hi"}`,
				"mentions": []map[string]any{
					{
						"id": map[string]any{
							"user_id": "ou_bot",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	event, ok := mapSDKMessageEvent(&larkevent.EventReq{Body: body})
	if !ok {
		t.Fatal("expected sdk event to parse")
	}
	if event.ChatType != "group" || event.ChatID != "chat-group" || event.MessageID != "msg-group" {
		t.Fatalf("unexpected sdk event: %#v", event)
	}
	if len(event.Mentions) != 1 || event.Mentions[0].UserID != "ou_bot" {
		t.Fatalf("unexpected mentions: %#v", event.Mentions)
	}
}

func TestHandleMessageSDKPrivateChatTriggersRun(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.IngressMode = IngressModeSDK
	err := adapter.HandleMessage(context.Background(), FeishuMessageEvent{
		EventID:     "evt-p2p",
		MessageID:   "msg-p2p",
		ChatID:      "chat-p2p",
		ChatType:    "p2p",
		ContentText: "hello sdk",
		HeaderAppID: "app",
	})
	if err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
	}
}

func TestHandleMessageSDKGroupOnlyBotMentionTriggersRun(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.IngressMode = IngressModeSDK
	err := adapter.HandleMessage(context.Background(), FeishuMessageEvent{
		EventID:     "evt-group-no",
		MessageID:   "msg-group-no",
		ChatID:      "chat-group-no",
		ChatType:    "group",
		ContentText: "@alice hello",
		HeaderAppID: "app",
		Mentions: []FeishuMention{
			{UserID: "ou_other"},
		},
	})
	if err != nil {
		t.Fatalf("handle non-bot mention: %v", err)
	}
	if adapterTestGateway(adapter).runCount != 0 {
		t.Fatalf("run count = %d, want 0", adapterTestGateway(adapter).runCount)
	}

	err = adapter.HandleMessage(context.Background(), FeishuMessageEvent{
		EventID:     "evt-group-yes",
		MessageID:   "msg-group-yes",
		ChatID:      "chat-group-yes",
		ChatType:    "group",
		ContentText: "@neo hello",
		HeaderAppID: "app",
		Mentions: []FeishuMention{
			{UserID: "ou_bot"},
		},
	})
	if err != nil {
		t.Fatalf("handle bot mention: %v", err)
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
	}
}

func TestHandleMessageSDKDuplicateEventOnlyRunsOnce(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.IngressMode = IngressModeSDK
	event := FeishuMessageEvent{
		EventID:     "evt-dup",
		MessageID:   "msg-dup",
		ChatID:      "chat-dup",
		ChatType:    "p2p",
		ContentText: "hello sdk",
		HeaderAppID: "app",
	}
	if err := adapter.HandleMessage(context.Background(), event); err != nil {
		t.Fatalf("handle message first: %v", err)
	}
	if err := adapter.HandleMessage(context.Background(), event); err != nil {
		t.Fatalf("handle message second: %v", err)
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
	}
}

func TestHandleMessageSDKRunFailureCanRetry(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.IngressMode = IngressModeSDK
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.runErr = assertErr("transient")
	gateway.runErrOnce = true
	gateway.mu.Unlock()

	event := FeishuMessageEvent{
		EventID:     "evt-retry-sdk",
		MessageID:   "msg-retry-sdk",
		ChatID:      "chat-retry-sdk",
		ChatType:    "p2p",
		ContentText: "hello sdk",
		HeaderAppID: "app",
	}
	_ = adapter.HandleMessage(context.Background(), event)
	if err := adapter.HandleMessage(context.Background(), event); err != nil {
		t.Fatalf("retry handle message: %v", err)
	}
	if adapterTestGateway(adapter).runCount != 2 {
		t.Fatalf("run count = %d, want 2", adapterTestGateway(adapter).runCount)
	}
}

func TestHandleMessageSDKRunFailureReturnsErrorForRetry(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.IngressMode = IngressModeSDK
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.runErr = assertErr("transient")
	gateway.mu.Unlock()

	err := adapter.HandleMessage(context.Background(), FeishuMessageEvent{
		EventID:     "evt-sdk-error",
		MessageID:   "msg-sdk-error",
		ChatID:      "chat-sdk-error",
		ChatType:    "group",
		ContentText: "hello sdk",
		Mentions: []FeishuMention{
			{UserID: "ou_bot"},
		},
	})
	if err == nil {
		t.Fatal("expected sdk handler error for retry")
	}
}
