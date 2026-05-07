package feishuadapter

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

type fakeGatewayClient struct {
	mu            sync.Mutex
	calls         []string
	notifications chan GatewayNotification
	runCount      int
	resolveCount  int
	authCount     int
	pingErr       error
	authErr       error
	bindErr       error
	resolveErr    error
	runErr        error
	runErrOnce    bool
}

func newFakeGatewayClient() *fakeGatewayClient {
	return &fakeGatewayClient{notifications: make(chan GatewayNotification, 16)}
}

func (f *fakeGatewayClient) Authenticate(context.Context) error {
	f.record("authenticate")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authCount++
	return f.authErr
}
func (f *fakeGatewayClient) BindStream(_ context.Context, sessionID string, runID string) error {
	f.record("bind:" + sessionID + ":" + runID)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bindErr
}
func (f *fakeGatewayClient) Run(_ context.Context, sessionID string, runID string, inputText string) error {
	f.record("run:" + sessionID + ":" + runID + ":" + inputText)
	f.mu.Lock()
	f.runCount++
	runErr := f.runErr
	if f.runErrOnce {
		f.runErr = nil
		f.runErrOnce = false
	}
	f.mu.Unlock()
	return runErr
}
func (f *fakeGatewayClient) ResolvePermission(_ context.Context, requestID string, decision string) error {
	f.record("resolve:" + requestID + ":" + decision)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveCount++
	return f.resolveErr
}
func (f *fakeGatewayClient) Ping(context.Context) error {
	f.record("ping")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pingErr
}
func (f *fakeGatewayClient) Notifications() <-chan GatewayNotification { return f.notifications }
func (f *fakeGatewayClient) Close() error {
	close(f.notifications)
	return nil
}
func (f *fakeGatewayClient) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}
func (f *fakeGatewayClient) snapshotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

type sentMessage struct {
	chatID  string
	kind    string
	text    string
	card    PermissionCardPayload
	runCard StatusCardPayload
	cardID  string
}

type fakeMessenger struct {
	mu            sync.Mutex
	messages      []sentMessage
	nextID        int
	sendTextErr   error
	sendCardErr   error
	updateCardErr error
}

func (m *fakeMessenger) SendText(_ context.Context, chatID string, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "text", text: text})
	return m.sendTextErr
}

func (m *fakeMessenger) SendPermissionCard(_ context.Context, chatID string, payload PermissionCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "card", card: payload})
	return nil
}

func (m *fakeMessenger) SendStatusCard(_ context.Context, chatID string, payload StatusCardPayload) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cardID := fmt.Sprintf("card-%d", m.nextID)
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "status_card", runCard: payload, cardID: cardID})
	return cardID, m.sendCardErr
}

func (m *fakeMessenger) UpdateCard(_ context.Context, cardID string, payload StatusCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{kind: "update_card", runCard: payload, cardID: cardID})
	return m.updateCardErr
}

func (m *fakeMessenger) snapshot() []sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sentMessage, len(m.messages))
	copy(out, m.messages)
	return out
}

func TestBuildIDsStable(t *testing.T) {
	sessionA := BuildSessionID("chat-1")
	sessionB := BuildSessionID("chat-1")
	if sessionA == "" || sessionA != sessionB {
		t.Fatalf("expected stable session id, got %q and %q", sessionA, sessionB)
	}
	runA := BuildRunID("msg-1")
	runB := BuildRunID("msg-1")
	if runA == "" || runA != runB {
		t.Fatalf("expected stable run id, got %q and %q", runA, runB)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	cfg := Config{
		ListenAddress:       "127.0.0.1:18080",
		EventPath:           "/feishu/events",
		CardPath:            "/feishu/cards",
		AppID:               "app",
		AppSecret:           "secret",
		VerifyToken:         "verify",
		SigningSecret:       "sign-secret",
		RequestTimeout:      time.Second,
		IdempotencyTTL:      time.Minute,
		ReconnectBackoffMin: 100 * time.Millisecond,
		ReconnectBackoffMax: time.Second,
		RebindInterval:      time.Second,
	}
	if _, err := New(cfg, nil, &fakeMessenger{}, nil); err == nil {
		t.Fatal("expected missing gateway error")
	}
	if _, err := New(cfg, newFakeGatewayClient(), nil, nil); err == nil {
		t.Fatal("expected missing messenger error")
	}
}

func TestRunReturnsAuthenticateFailure(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.authErr = assertErr("auth failed")
	gateway.mu.Unlock()

	err := adapter.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authenticate gateway") {
		t.Fatalf("run error = %v, want authenticate failure", err)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.cfg.ListenAddress = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- adapter.Run(ctx)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("run error = %v, want nil or context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for adapter shutdown")
	}
}

func TestHandleFeishuEventChallenge(t *testing.T) {
	adapter := newTestAdapter(t)
	body := `{"type":"url_verification","challenge":"abc","token":"verify"}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"challenge":"abc"`) {
		t.Fatalf("response = %s, want challenge", recorder.Body.String())
	}
}

func TestHandleFeishuEventRejectsInvalidSignature(t *testing.T) {
	adapter := newTestAdapter(t)
	request := httptest.NewRequest(http.MethodPost, "/feishu/events", strings.NewReader(`{"type":"url_verification","challenge":"abc"}`))
	request.Header.Set(headerLarkTimestamp, strconvTimestamp(time.Now().UTC()))
	request.Header.Set(headerLarkNonce, "nonce")
	request.Header.Set(headerLarkSignature, "invalid")
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestHandleFeishuEventCoversValidationFailures(t *testing.T) {
	adapter := newTestAdapter(t)
	testCases := []struct {
		name string
		body string
		want int
	}{
		{name: "invalid json", body: `{`, want: http.StatusBadRequest},
		{name: "ignored event", body: `{"header":{"event_type":"other","token":"verify"}}`, want: http.StatusOK},
		{name: "invalid token", body: `{"header":{"event_type":"im.message.receive_v1","token":"bad"},"event":{}}`, want: http.StatusUnauthorized},
		{name: "invalid event body", body: `{"header":{"event_type":"im.message.receive_v1","token":"verify"},"event":"oops"}`, want: http.StatusBadRequest},
		{name: "missing ids", body: `{"header":{"event_type":"im.message.receive_v1","token":"verify"},"event":{"message":{"message_id":"","chat_id":""}}}`, want: http.StatusBadRequest},
		{name: "invalid content", body: "{\"header\":{\"event_id\":\"evt-invalid-content\",\"event_type\":\"im.message.receive_v1\",\"token\":\"verify\"},\"event\":{\"message\":{\"message_id\":\"msg-invalid-content\",\"chat_id\":\"chat-invalid-content\",\"content\":\"{\"}}}", want: http.StatusBadRequest},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := signedRequest(t, adapter.cfg.SigningSecret, testCase.body)
			recorder := httptest.NewRecorder()
			adapter.handleFeishuEvent(recorder, request)
			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d body=%s", recorder.Code, testCase.want, recorder.Body.String())
			}
		})
	}
}

func TestMessageEventDedupeOnlyRunsOnce(t *testing.T) {
	adapter := newTestAdapter(t)
	body := messageEventBody("evt-1", "msg-1", "chat-1", "hello")
	for i := 0; i < 2; i++ {
		request := signedRequest(t, adapter.cfg.SigningSecret, body)
		recorder := httptest.NewRecorder()
		adapter.handleFeishuEvent(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
	}
}

func TestMessageEventRetryAfterRunFailure(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.runErr = assertErr("transient")
	gateway.runErrOnce = true
	gateway.mu.Unlock()

	body := messageEventBody("evt-retry", "msg-retry", "chat-retry", "hello")
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want 500", recorder.Code)
	}
	request = signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder = httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).runCount != 2 {
		t.Fatalf("run count = %d, want 2", adapterTestGateway(adapter).runCount)
	}
}

func TestRunFailureCleansTrackedRunBinding(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.runErr = assertErr("reject")
	gateway.mu.Unlock()

	err := adapter.bindThenRun(context.Background(), "session-fail", "run-fail", "chat-fail", "hello")
	if err == nil {
		t.Fatal("expected bindThenRun error")
	}
	adapter.mu.RLock()
	_, exists := adapter.activeRuns[runBindingKey("session-fail", "run-fail")]
	adapter.mu.RUnlock()
	if exists {
		t.Fatal("expected failed run binding to be cleaned")
	}
}

func TestGroupMessageWithoutMentionIgnored(t *testing.T) {
	adapter := newTestAdapter(t)
	body := messageEventBodyWithChatType("evt-group", "msg-group", "chat-group", "hello group", "group")
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).runCount != 0 {
		t.Fatalf("run count = %d, want 0", adapterTestGateway(adapter).runCount)
	}
}

func TestGroupMessageWithMentionAccepted(t *testing.T) {
	adapter := newTestAdapter(t)
	content, _ := json.Marshal(map[string]string{"text": "<at user_id=\"app\">neo</at> hi"})
	payload := map[string]any{
		"header": map[string]any{
			"event_id":   "evt-group-mention",
			"event_type": "im.message.receive_v1",
			"token":      "verify",
			"app_id":     "app",
		},
		"event": map[string]any{
			"message": map[string]any{
				"message_id": "msg-group-mention",
				"chat_id":    "chat-group-mention",
				"chat_type":  "group",
				"content":    string(content),
				"mentions": []map[string]any{
					{
						"name": "neo",
						"id": map[string]any{
							"user_id": "ou_bot",
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	request := signedRequest(t, adapter.cfg.SigningSecret, string(raw))
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
	}
}

func TestGroupMessageWithNonBotMentionIgnored(t *testing.T) {
	adapter := newTestAdapter(t)
	content, _ := json.Marshal(map[string]string{"text": "<at user_id=\"ou_other\">alice</at> hi"})
	payload := map[string]any{
		"header": map[string]any{
			"event_id":   "evt-group-non-bot-mention",
			"event_type": "im.message.receive_v1",
			"token":      "verify",
			"app_id":     "app",
		},
		"event": map[string]any{
			"message": map[string]any{
				"message_id": "msg-group-non-bot-mention",
				"chat_id":    "chat-group-non-bot-mention",
				"chat_type":  "group",
				"content":    string(content),
				"mentions": []map[string]any{
					{
						"name": "alice",
						"id": map[string]any{
							"user_id": "ou_other",
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	request := signedRequest(t, adapter.cfg.SigningSecret, string(raw))
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).runCount != 0 {
		t.Fatalf("run count = %d, want 0", adapterTestGateway(adapter).runCount)
	}
}

func TestCallOrderAuthenticateBindRun(t *testing.T) {
	adapter := newTestAdapter(t)
	body := messageEventBody("evt-2", "msg-2", "chat-2", "run it")
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)

	calls := adapterTestGateway(adapter).snapshotCalls()
	joined := strings.Join(calls, "|")
	authIndex := strings.Index(joined, "authenticate")
	bindIndex := strings.Index(joined, "bind:")
	runIndex := strings.Index(joined, "run:")
	if !(authIndex >= 0 && bindIndex > authIndex && runIndex > bindIndex) {
		t.Fatalf("unexpected call order: %v", calls)
	}
}

func TestGatewayEventsMappedToMessagesAndPermissionCard(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	adapter.trackSession(BuildSessionID("chat-x"), BuildRunID("msg-x"), "chat-x", "chat-x task")
	pushGatewayEvent(t, adapterTestGateway(adapter), BuildSessionID("chat-x"), BuildRunID("msg-x"), "run_done", map[string]any{
		"runtime_event_type": "agent_done",
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), BuildSessionID("chat-x"), BuildRunID("msg-x"), "run_error", map[string]any{
		"runtime_event_type": "error",
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), BuildSessionID("chat-x"), BuildRunID("msg-x"), "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-1",
			"reason":     "need approval",
		},
	})
	time.Sleep(30 * time.Millisecond)
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) < 1 {
		t.Fatalf("messages = %#v, want >=1", msgs)
	}
	foundCard := false
	for _, message := range msgs {
		if message.kind == "card" && message.card.RequestID == "perm-1" {
			foundCard = true
		}
	}
	if !foundCard {
		t.Fatalf("expected permission card message, got %#v", msgs)
	}
}

func TestBindThenRunCreatesStatusCard(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-card", "run-card", "chat-card", "编写发布说明"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	found := false
	for _, message := range msgs {
		if message.kind == "status_card" && message.runCard.TaskName == "编写发布说明" && message.runCard.Status == "thinking" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected status card message, got %#v", msgs)
	}
}

func TestGatewayEventsUpdateStatusCard(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-progress", "run-progress", "chat-progress", "整理计划"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-progress", "run-progress", "run_progress", map[string]any{
		"runtime_event_type": "phase_changed",
		"payload": map[string]any{
			"to": "plan",
		},
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-progress", "run-progress", "run_progress", map[string]any{
		"runtime_event_type": "hook_notification",
		"payload": map[string]any{
			"summary": "已收到异步回灌摘要",
			"reason":  "async_rewake",
		},
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-progress", "run-progress", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-status",
			"reason":     "需要确认是否执行命令",
		},
	})
	time.Sleep(30 * time.Millisecond)
	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-status",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("handle card action: %v", err)
	}
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-progress", "run-progress", "run_done", map[string]any{
		"runtime_event_type": "agent_done",
		"payload": map[string]any{
			"content": "任务完成",
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	foundPlanning := false
	foundApproved := false
	foundSuccess := false
	for _, message := range msgs {
		if message.kind != "update_card" {
			continue
		}
		if message.runCard.Status == "planning" {
			foundPlanning = true
		}
		if message.runCard.ApprovalStatus == "approved" {
			foundApproved = true
		}
		if message.runCard.Result == "success" && strings.Contains(message.runCard.Summary, "任务完成") {
			foundSuccess = true
		}
	}
	if !foundPlanning || !foundApproved || !foundSuccess {
		t.Fatalf("unexpected card updates: %#v", msgs)
	}
}

func TestRunTerminalEventUntracksActiveRun(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-cleanup")
	runID := BuildRunID("msg-cleanup")
	adapter.trackSession(sessionID, runID, "chat-cleanup", "chat-cleanup task")

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_done", map[string]any{
		"runtime_event_type": "agent_done",
		"payload": map[string]any{
			"content": "done",
		},
	})
	time.Sleep(30 * time.Millisecond)

	adapter.mu.RLock()
	_, exists := adapter.activeRuns[runBindingKey(sessionID, runID)]
	adapter.mu.RUnlock()
	if exists {
		t.Fatalf("expected run binding cleaned after terminal event")
	}
}

func TestRunDonePrefersAssistantTextForUserFacingReply(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-done-text")
	runID := BuildRunID("msg-done-text")
	adapter.trackSession(sessionID, runID, "chat-done-text", "chat-done-text task")
	_ = adapter.ensureRunCard(context.Background(), sessionID, runID)

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_done", map[string]any{
		"runtime_event_type": "agent_done",
		"payload": map[string]any{
			"parts": []map[string]any{
				{"type": "text", "text": "这是最终回复"},
			},
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 {
		t.Fatalf("expected at least one message")
	}
	last := msgs[len(msgs)-1]
	if last.kind != "update_card" || !strings.Contains(last.runCard.Summary, "这是最终回复") {
		t.Fatalf("expected card update with summary text, got %#v", last)
	}
}

func TestRunProgressInternalEventsAreNotUserFacing(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-throttle")
	runID := BuildRunID("msg-throttle")
	adapter.trackSession(sessionID, runID, "chat-throttle", "chat-throttle task")

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_progress", map[string]any{
		"runtime_event_type": "agent_chunk",
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_progress", map[string]any{
		"runtime_event_type": "agent_chunk",
	})
	time.Sleep(30 * time.Millisecond)

	textCount := 0
	for _, message := range adapterTestMessenger(adapter).snapshot() {
		if message.kind == "text" && strings.Contains(message.text, "运行进度") {
			textCount++
		}
	}
	if textCount != 0 {
		t.Fatalf("progress message count = %d, want 0", textCount)
	}
}

func TestCardCallbackDedupeResolveOnce(t *testing.T) {
	adapter := newTestAdapter(t)
	body := `{"action":{"value":{"request_id":"perm-2","decision":"allow_once"}},"token":"verify"}`
	for i := 0; i < 2; i++ {
		request := signedRequest(t, adapter.cfg.SigningSecret, body)
		recorder := httptest.NewRecorder()
		adapter.handleCardCallback(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	}
	if adapterTestGateway(adapter).resolveCount != 1 {
		t.Fatalf("resolve count = %d, want 1", adapterTestGateway(adapter).resolveCount)
	}
}

func TestCardCallbackResolveFailureReturns500(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.resolveErr = assertErr("deny")
	gateway.mu.Unlock()

	body := `{"action":{"value":{"request_id":"perm-3","decision":"reject"}},"token":"verify"}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestCardCallbackUrlVerificationAccepted(t *testing.T) {
	adapter := newTestAdapter(t)
	body := `{"type":"url_verification","challenge":"card-challenge","token":"verify","header":{"token":"verify"}}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"challenge":"card-challenge"`) {
		t.Fatalf("response = %s, want challenge", recorder.Body.String())
	}
}

func TestHandleCardCallbackValidationFailures(t *testing.T) {
	adapter := newTestAdapter(t)
	testCases := []struct {
		name string
		body string
		want int
	}{
		{name: "invalid token", body: `{"token":"bad","header":{"token":"bad"}}`, want: http.StatusUnauthorized},
		{name: "invalid json", body: `{`, want: http.StatusBadRequest},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := signedRequest(t, adapter.cfg.SigningSecret, testCase.body)
			recorder := httptest.NewRecorder()
			adapter.handleCardCallback(recorder, request)
			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.want)
			}
		})
	}
}

func TestCardCallbackProbeWithoutActionReturnsOK(t *testing.T) {
	adapter := newTestAdapter(t)
	body := `{"token":"verify","header":{"token":"verify"}}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).resolveCount != 0 {
		t.Fatalf("resolve count = %d, want 0", adapterTestGateway(adapter).resolveCount)
	}
}

func TestReconnectRebindActiveSessions(t *testing.T) {
	adapter := newTestAdapter(t)
	gw := adapterTestGateway(adapter)
	gw.pingErr = assertErr("dial failed")
	adapter.trackSession("session-a", "run-a", "chat-a", "task-a")

	ctx, cancel := context.WithCancel(context.Background())
	go adapter.reconnectAndRebindLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	gw.mu.Lock()
	gw.pingErr = nil
	gw.mu.Unlock()
	waitForCalls(t, gw, func(calls string) bool {
		return strings.Contains(calls, "bind:session-a:run-a")
	})
	cancel()
	time.Sleep(20 * time.Millisecond)

	calls := strings.Join(gw.snapshotCalls(), "|")
	if !strings.Contains(calls, "bind:session-a:run-a") {
		t.Fatalf("expected rebind call in %v", calls)
	}
}

func TestReconnectRebindTracksMultipleRunsPerSession(t *testing.T) {
	adapter := newTestAdapter(t)
	gw := adapterTestGateway(adapter)
	gw.pingErr = assertErr("dial failed")
	adapter.trackSession("session-x", "run-a", "chat-x", "task-a")
	adapter.trackSession("session-x", "run-b", "chat-x", "task-b")

	ctx, cancel := context.WithCancel(context.Background())
	go adapter.reconnectAndRebindLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	gw.mu.Lock()
	gw.pingErr = nil
	gw.mu.Unlock()
	waitForCalls(t, gw, func(calls string) bool {
		return strings.Contains(calls, "bind:session-x:run-a") &&
			strings.Contains(calls, "bind:session-x:run-b")
	})
	cancel()
	time.Sleep(20 * time.Millisecond)

	calls := strings.Join(gw.snapshotCalls(), "|")
	if !strings.Contains(calls, "bind:session-x:run-a") {
		t.Fatalf("expected run-a rebind call in %v", calls)
	}
	if !strings.Contains(calls, "bind:session-x:run-b") {
		t.Fatalf("expected run-b rebind call in %v", calls)
	}
}

func TestReconnectHealthyPathDoesNotRebind(t *testing.T) {
	adapter := newTestAdapter(t)
	gw := adapterTestGateway(adapter)
	adapter.trackSession("session-steady", "run-steady", "chat-steady", "steady")

	ctx, cancel := context.WithCancel(context.Background())
	go adapter.reconnectAndRebindLoop(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	calls := strings.Join(gw.snapshotCalls(), "|")
	if strings.Contains(calls, "bind:session-steady:run-steady") {
		t.Fatalf("did not expect steady-state rebind call in %v", calls)
	}
}

func TestRetryAuthenticateAndRebindHandlesAuthFailure(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.authErr = assertErr("re-auth failed")
	gateway.mu.Unlock()
	adapter.trackSession("session-auth-fail", "run-auth-fail", "chat-auth-fail", "task")

	if ok := adapter.retryAuthenticateAndRebind(context.Background(), time.Millisecond); !ok {
		t.Fatal("expected retry loop to continue after auth failure")
	}
	calls := strings.Join(gateway.snapshotCalls(), "|")
	if strings.Contains(calls, "bind:session-auth-fail:run-auth-fail") {
		t.Fatalf("did not expect rebind after auth failure: %v", calls)
	}
}

func TestRetryAuthenticateAndRebindStopsWhenContextCanceled(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok := adapter.retryAuthenticateAndRebind(ctx, time.Hour); ok {
		t.Fatal("expected retry to stop when context is canceled")
	}
}

func TestBuildIngressSelectsConfiguredMode(t *testing.T) {
	adapter := newTestAdapter(t)
	if _, ok := adapter.buildIngress().(*WebhookIngress); !ok {
		t.Fatalf("expected webhook ingress by default, got %T", adapter.buildIngress())
	}

	adapter.cfg.IngressMode = IngressModeSDK
	if _, ok := adapter.buildIngress().(*SDKIngress); !ok {
		t.Fatalf("expected sdk ingress, got %T", adapter.buildIngress())
	}
}

func TestRefreshActiveCardsUpdatesExistingCard(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-refresh")
	runID := BuildRunID("msg-refresh")
	adapter.trackSession(sessionID, runID, "chat-refresh", "refresh task")

	adapter.mu.Lock()
	binding := adapter.activeRuns[runBindingKey(sessionID, runID)]
	binding.CardID = "card-refresh"
	binding.RunStartTime = time.Now().Add(-3 * time.Second)
	adapter.activeRuns[runBindingKey(sessionID, runID)] = binding
	adapter.mu.Unlock()

	adapter.refreshActiveCards(context.Background())

	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) != 1 || msgs[0].kind != "update_card" || msgs[0].cardID != "card-refresh" {
		t.Fatalf("unexpected refresh updates: %#v", msgs)
	}
	if msgs[0].runCard.Elapsed == "" {
		t.Fatalf("expected elapsed time in payload: %#v", msgs[0].runCard)
	}
}

func TestRefreshActiveCardsSkipsBindingsWithoutCardID(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-no-card")
	runID := BuildRunID("msg-no-card")
	adapter.trackSession(sessionID, runID, "chat-no-card", "no card task")

	adapter.refreshActiveCards(context.Background())

	if msgs := adapterTestMessenger(adapter).snapshot(); len(msgs) != 0 {
		t.Fatalf("expected no card refresh messages, got %#v", msgs)
	}
}

func TestEnsureRunCardUpdatesExistingCard(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-existing-card")
	runID := BuildRunID("msg-existing-card")
	adapter.trackSession(sessionID, runID, "chat-existing-card", "existing task")

	adapter.mu.Lock()
	binding := adapter.activeRuns[runBindingKey(sessionID, runID)]
	binding.CardID = "card-existing"
	adapter.activeRuns[runBindingKey(sessionID, runID)] = binding
	adapter.mu.Unlock()

	if err := adapter.ensureRunCard(context.Background(), sessionID, runID); err != nil {
		t.Fatalf("ensure existing run card: %v", err)
	}

	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) != 1 || msgs[0].kind != "update_card" || msgs[0].cardID != "card-existing" {
		t.Fatalf("unexpected card update: %#v", msgs)
	}
}

func TestTryHandleTextPermissionHandlesApprovalCommands(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.trackSession("session-approve", "run-approve", "chat-approve", "approve task")
	adapter.mu.Lock()
	adapter.requestRuns["perm-approve"] = "session-approve|run-approve"
	adapter.mu.Unlock()

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-approve", "允许 perm-approve")
	if err != nil || !handled {
		t.Fatalf("allow command = handled:%v err:%v", handled, err)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 || msgs[len(msgs)-1].text != "审批已提交：允许一次。" {
		t.Fatalf("unexpected approval reply: %#v", msgs)
	}
	if adapterTestGateway(adapter).resolveCount != 1 {
		t.Fatalf("resolve count = %d, want 1", adapterTestGateway(adapter).resolveCount)
	}
}

func TestTryHandleTextPermissionHandlesRejectCommand(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.trackSession("session-reject-ok", "run-reject-ok", "chat-reject-ok", "reject task")
	adapter.mu.Lock()
	adapter.requestRuns["perm-reject-ok"] = "session-reject-ok|run-reject-ok"
	adapter.mu.Unlock()

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-reject-ok", "拒绝 perm-reject-ok")
	if err != nil || !handled {
		t.Fatalf("reject command = handled:%v err:%v", handled, err)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 || msgs[len(msgs)-1].text != "审批已提交：拒绝。" {
		t.Fatalf("unexpected reject reply: %#v", msgs)
	}
}

func TestTryHandleTextPermissionRejectFailureRepliesRetryable(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.trackSession("session-reject", "run-reject", "chat-reject", "reject task")
	adapter.mu.Lock()
	adapter.requestRuns["perm-reject"] = "session-reject|run-reject"
	adapter.activeRuns["session-reject|run-reject"] = sessionBinding{
		SessionID:      "session-reject",
		RunID:          "run-reject",
		ChatID:         "chat-reject",
		TaskName:       "reject task",
		Status:         "running",
		ApprovalStatus: "pending",
		Result:         "pending",
	}
	adapter.mu.Unlock()
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.resolveErr = assertErr("boom")
	gateway.mu.Unlock()

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-reject", "拒绝 perm-reject")
	if err == nil || !handled {
		t.Fatalf("reject command = handled:%v err:%v", handled, err)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 || msgs[len(msgs)-1].text != "审批提交失败，请稍后重试。" {
		t.Fatalf("unexpected failure reply: %#v", msgs)
	}
}

func TestTryHandleTextPermissionHandlesEmptyAndUnknownCommands(t *testing.T) {
	adapter := newTestAdapter(t)
	for _, testCase := range []struct {
		text        string
		wantHandled bool
	}{
		{text: "", wantHandled: false},
		{text: "允许", wantHandled: false},
		{text: "拒绝", wantHandled: false},
		{text: "hello", wantHandled: false},
	} {
		handled, err := adapter.tryHandleTextPermission(context.Background(), "chat", testCase.text)
		if err != nil {
			t.Fatalf("text %q error: %v", testCase.text, err)
		}
		if handled != testCase.wantHandled {
			t.Fatalf("text %q handled=%v, want %v", testCase.text, handled, testCase.wantHandled)
		}
	}
}

func TestBindThenRunCoversFailureAndFallbackPaths(t *testing.T) {
	t.Run("authenticate failure", func(t *testing.T) {
		adapter := newTestAdapter(t)
		gateway := adapterTestGateway(adapter)
		gateway.mu.Lock()
		gateway.authErr = assertErr("auth failed")
		gateway.mu.Unlock()
		if err := adapter.bindThenRun(context.Background(), "session-auth", "run-auth", "chat-auth", "task"); err == nil {
			t.Fatal("expected authenticate failure")
		}
	})

	t.Run("bind failure", func(t *testing.T) {
		adapter := newTestAdapter(t)
		gateway := adapterTestGateway(adapter)
		gateway.mu.Lock()
		gateway.bindErr = assertErr("bind failed")
		gateway.mu.Unlock()
		if err := adapter.bindThenRun(context.Background(), "session-bind", "run-bind", "chat-bind", "task"); err == nil {
			t.Fatal("expected bind failure")
		}
	})

	t.Run("status card fallback text", func(t *testing.T) {
		adapter := newTestAdapter(t)
		messenger := adapterTestMessenger(adapter)
		messenger.mu.Lock()
		messenger.sendCardErr = assertErr("card failed")
		messenger.mu.Unlock()
		if err := adapter.bindThenRun(context.Background(), "session-card", "run-card", "chat-card", "task"); err != nil {
			t.Fatalf("bind then run: %v", err)
		}
		msgs := messenger.snapshot()
		if len(msgs) == 0 || msgs[len(msgs)-1].text != "任务已受理，正在执行。" {
			t.Fatalf("unexpected fallback messages: %#v", msgs)
		}
	})
}

func TestReadAndVerifyRequestRejectsNonPost(t *testing.T) {
	ingress := &WebhookIngress{
		cfg: Config{
			SigningSecret:  "sign-secret",
			IngressMode:    IngressModeWebhook,
			RequestTimeout: 200 * time.Millisecond,
			IdempotencyTTL: 2 * time.Minute,
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/feishu/events", nil)
	recorder := httptest.NewRecorder()
	if body, ok := ingress.readAndVerifyRequest(recorder, request); ok || body != nil {
		t.Fatalf("expected non-post request rejection, body=%q ok=%v", string(body), ok)
	}
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestNewWebhookIngressProvidesDefaultClockAndTokenValidation(t *testing.T) {
	ingress, ok := NewWebhookIngress(Config{VerifyToken: "verify"}, nil).(*WebhookIngress)
	if !ok {
		t.Fatalf("expected webhook ingress, got %T", ingress)
	}
	if ingress.nowFn == nil {
		t.Fatal("expected default clock")
	}
	if !ingress.verifyCallbackToken("", "verify") {
		t.Fatal("expected header token to be accepted")
	}
	ingress.cfg.VerifyToken = ""
	if ingress.verifyCallbackToken("verify", "verify") {
		t.Fatal("expected empty configured token to reject callback")
	}
}

func TestFormatElapsedCoversRanges(t *testing.T) {
	if got := formatElapsed(time.Time{}); got != "" {
		t.Fatalf("zero elapsed = %q, want empty", got)
	}
	if got := formatElapsed(time.Now().Add(-500 * time.Millisecond)); got != "刚刚开始" {
		t.Fatalf("sub-second elapsed = %q", got)
	}
	if got := formatElapsed(time.Now().Add(-5 * time.Second)); got != "5s" {
		t.Fatalf("seconds elapsed = %q", got)
	}
	if got := formatElapsed(time.Now().Add(-65 * time.Second)); got != "1m 5s" {
		t.Fatalf("minutes elapsed = %q", got)
	}
	if got := formatElapsed(time.Now().Add(-(time.Hour + 2*time.Minute + 3*time.Second))); got != "1h 2m 3s" {
		t.Fatalf("hours elapsed = %q", got)
	}
}

func TestBuildTaskNameTruncatesLongFirstLine(t *testing.T) {
	text := "12345678901234567890123456789012345678901\nsecond line"
	if got := buildTaskName(text); got != "1234567890123456789012345678901234567890..." {
		t.Fatalf("task name = %q", got)
	}
}

func TestExtractHookNotificationSummaryAndHintFallbacks(t *testing.T) {
	if summary := extractHookNotificationSummary(map[string]any{
		"payload": map[string]any{"notification": "notify"},
	}); summary != "notify" {
		t.Fatalf("summary = %q, want notify", summary)
	}
	if summary := extractHookNotificationSummary(map[string]any{
		"payload": map[string]any{"message": "message"},
	}); summary != "message" {
		t.Fatalf("summary = %q, want message", summary)
	}
	if hint := extractHookNotificationHint(map[string]any{
		"payload": map[string]any{"status": "async"},
	}); hint != "async" {
		t.Fatalf("hint = %q, want async", hint)
	}
}

func TestDeriveRunStatusAdditionalBranches(t *testing.T) {
	if status := deriveRunStatus("phase_changed", map[string]any{
		"payload": map[string]any{"to": "execute"},
	}, "thinking"); status != "running" {
		t.Fatalf("status = %q, want running", status)
	}
	if status := deriveRunStatus("hook_notification", map[string]any{}, "planning"); status != "running" {
		t.Fatalf("status = %q, want running", status)
	}
	if status := deriveRunStatus("unknown", map[string]any{}, ""); status != "thinking" {
		t.Fatalf("status = %q, want thinking", status)
	}
}

func TestIsMentionCurrentBotMatchesContentMarkupAndOpenID(t *testing.T) {
	cfg := Config{BotUserID: "ou_bot", BotOpenID: "ou_open_bot"}
	if !isMentionCurrentBot(FeishuMessageEvent{
		ChatType:    "group",
		ContentText: `<at user_id="ou_bot"></at> hi`,
	}, cfg) {
		t.Fatal("expected content markup to match bot user id")
	}
	if !isMentionCurrentBot(FeishuMessageEvent{
		ChatType: "group",
		Mentions: []FeishuMention{{OpenID: "ou_open_bot"}},
	}, cfg) {
		t.Fatal("expected mention open id to match bot")
	}
}

func TestExtractUserVisibleDoneTextHandlesTextFieldAndTypedParts(t *testing.T) {
	if text := extractUserVisibleDoneText(map[string]any{
		"payload": map[string]any{"text": "plain done"},
	}); text != "plain done" {
		t.Fatalf("done text = %q, want plain done", text)
	}
	if text := extractUserVisibleDoneText(map[string]any{
		"payload": map[string]any{
			"parts": []any{
				map[string]any{"type": "image", "text": "ignore"},
				map[string]any{"type": "text", "content": "keep"},
			},
		},
	}); text != "keep" {
		t.Fatalf("parts text = %q, want keep", text)
	}
}

func TestConsumeGatewayEventsIgnoresNonGatewayNotifications(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		adapter.consumeGatewayEvents(ctx)
		close(done)
	}()

	adapterTestGateway(adapter).notifications <- GatewayNotification{Method: "other.method", Params: json.RawMessage(`{}`)}
	cancel()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("consumeGatewayEvents did not stop after cancellation")
	}
	if msgs := adapterTestMessenger(adapter).snapshot(); len(msgs) != 0 {
		t.Fatalf("expected no messages for non-gateway event, got %#v", msgs)
	}
}

func TestReadAndVerifyRequestRejectsUnreadableBody(t *testing.T) {
	ingress := &WebhookIngress{
		cfg: Config{
			SigningSecret:  "sign-secret",
			IngressMode:    IngressModeWebhook,
			RequestTimeout: 200 * time.Millisecond,
			IdempotencyTTL: 2 * time.Minute,
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/feishu/events", errReader{})
	request.Header.Set(headerLarkTimestamp, strconvTimestamp(time.Now().UTC()))
	request.Header.Set(headerLarkNonce, "nonce")
	request.Header.Set(headerLarkSignature, "sig")
	recorder := httptest.NewRecorder()
	if _, ok := ingress.readAndVerifyRequest(recorder, request); ok {
		t.Fatal("expected unreadable body to fail")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestShouldEmitProgressThrottlesRapidDuplicates(t *testing.T) {
	adapter := newTestAdapter(t)
	now := time.Now().UTC()
	adapter.nowFn = func() time.Time { return now }
	if !adapter.shouldEmitProgress("session", "run", "agent_chunk") {
		t.Fatal("expected first progress event to emit")
	}
	if adapter.shouldEmitProgress("session", "run", "agent_chunk") {
		t.Fatal("expected duplicate progress event to be throttled")
	}
	adapter.nowFn = func() time.Time { return now.Add(defaultProgressNotifyInterval + time.Millisecond) }
	if !adapter.shouldEmitProgress("session", "run", "agent_chunk") {
		t.Fatal("expected event after interval to emit")
	}
}

func TestHelperFunctionsCoverFallbackBranches(t *testing.T) {
	if text, err := decodeMessageText(""); err != nil || text != "" {
		t.Fatalf("decode empty text = %q, %v", text, err)
	}
	if _, err := decodeMessageText("{"); err == nil {
		t.Fatal("expected invalid message content error")
	}
	requestID, reason := extractPermissionRequest(nil)
	if requestID != "" || reason == "" {
		t.Fatalf("unexpected permission extraction: request=%q reason=%q", requestID, reason)
	}
	if text := extractUserVisibleDoneText(map[string]any{
		"payload": map[string]any{"content": "done"},
	}); text != "done" {
		t.Fatalf("done text = %q, want direct content", text)
	}
	if text := extractUserVisibleErrorText(map[string]any{
		"payload": map[string]any{"error": "boom"},
	}); text != "任务失败：boom" {
		t.Fatalf("error text = %q, want fallback error", text)
	}
	if text := extractUserVisibleErrorText(nil); text != "" {
		t.Fatalf("error text = %q, want empty", text)
	}
	if delay := nextBackoff(time.Second, 1500*time.Millisecond); delay != 1500*time.Millisecond {
		t.Fatalf("next backoff = %s, want capped max", delay)
	}
	if delay := delayWithJitter(0); delay != 200*time.Millisecond {
		t.Fatalf("jitter delay = %s, want default fallback", delay)
	}
	if taskName := buildTaskName(""); taskName != "未命名任务" {
		t.Fatalf("task name = %q, want unnamed fallback", taskName)
	}
	if status := deriveRunStatus("phase_changed", map[string]any{
		"payload": map[string]any{"to": "plan"},
	}, "thinking"); status != "planning" {
		t.Fatalf("status = %q, want planning", status)
	}
	safeLogAdapter := &Adapter{}
	safeLogAdapter.safeLog("ignored")
}

func TestIsMentionCurrentBotMatchesConfiguredBotIDs(t *testing.T) {
	cfg := Config{AppID: "cli_app", BotUserID: "ou_bot", BotOpenID: "ou_open_bot"}
	event := FeishuMessageEvent{
		ChatType: "group",
		Mentions: []FeishuMention{
			{UserID: "ou_bot"},
		},
	}
	if !isMentionCurrentBot(event, cfg) {
		t.Fatal("expected mention match by bot_user_id")
	}
}

func TestIsMentionCurrentBotDoesNotTreatAppIDAsUserID(t *testing.T) {
	cfg := Config{AppID: "cli_app"}
	event := FeishuMessageEvent{
		ChatType: "group",
		Mentions: []FeishuMention{
			{UserID: "cli_app"},
		},
	}
	if isMentionCurrentBot(event, cfg) {
		t.Fatal("expected no match when only user_id equals app_id")
	}
}

func TestIsMentionCurrentBotMatchesMentionAppID(t *testing.T) {
	cfg := Config{AppID: "cli_app"}
	event := FeishuMessageEvent{
		ChatType: "group",
		Mentions: []FeishuMention{
			{AppID: "cli_app"},
		},
	}
	if !isMentionCurrentBot(event, cfg) {
		t.Fatal("expected mention match by mention.app_id")
	}
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	gateway := newFakeGatewayClient()
	messenger := &fakeMessenger{}
	adapter, err := New(Config{
		ListenAddress:       "127.0.0.1:18080",
		EventPath:           "/feishu/events",
		CardPath:            "/feishu/cards",
		AppID:               "app",
		AppSecret:           "secret",
		BotUserID:           "ou_bot",
		BotOpenID:           "ou_open_bot",
		VerifyToken:         "verify",
		SigningSecret:       "sign-secret",
		RequestTimeout:      200 * time.Millisecond,
		IdempotencyTTL:      2 * time.Minute,
		ReconnectBackoffMin: 10 * time.Millisecond,
		ReconnectBackoffMax: 20 * time.Millisecond,
		RebindInterval:      20 * time.Millisecond,
	}, gateway, messenger, nil)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return adapter
}

func adapterTestGateway(adapter *Adapter) *fakeGatewayClient {
	return adapter.gateway.(*fakeGatewayClient)
}

func adapterTestMessenger(adapter *Adapter) *fakeMessenger {
	return adapter.messenger.(*fakeMessenger)
}

func messageEventBody(eventID string, messageID string, chatID string, text string) string {
	return messageEventBodyWithChatType(eventID, messageID, chatID, text, "")
}

func messageEventBodyWithChatType(eventID string, messageID string, chatID string, text string, chatType string) string {
	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]any{
		"header": map[string]any{
			"event_id":   eventID,
			"event_type": "im.message.receive_v1",
			"token":      "verify",
		},
		"event": map[string]any{
			"message": map[string]any{
				"message_id": messageID,
				"chat_id":    chatID,
				"chat_type":  chatType,
				"content":    string(content),
			},
		},
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func signedRequest(t *testing.T, secret string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewBufferString(body))
	timestamp := strconvTimestamp(time.Now().UTC())
	nonce := "nonce"
	raw := timestamp + nonce + body
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(raw))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	request.Header.Set(headerLarkTimestamp, timestamp)
	request.Header.Set(headerLarkNonce, nonce)
	request.Header.Set(headerLarkSignature, signature)
	return request
}

func strconvTimestamp(now time.Time) string {
	return fmt.Sprintf("%d", now.Unix())
}

func pushGatewayEvent(t *testing.T, gw *fakeGatewayClient, sessionID string, runID string, eventType string, envelope map[string]any) {
	t.Helper()
	frame := map[string]any{
		"session_id": sessionID,
		"run_id":     runID,
		"payload": map[string]any{
			"event_type": eventType,
			"payload":    envelope,
		},
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	gw.notifications <- GatewayNotification{Method: protocol.MethodGatewayEvent, Params: data}
}

func waitForCalls(t *testing.T, gw *fakeGatewayClient, match func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		calls := strings.Join(gw.snapshotCalls(), "|")
		if match(calls) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within timeout, calls=%v", gw.snapshotCalls())
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, assertErr("read failed")
}
