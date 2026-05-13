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
	cancelCount   int
	authCount     int
	pingErr       error
	authErr       error
	bindErr       error
	resolveErr    error
	cancelErr     error
	runErr        error
	runErrOnce    bool
	cancelResult  bool
	resolveHook   func(requestID string, decision string) error
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
func (f *fakeGatewayClient) CancelRun(_ context.Context, sessionID string, runID string) (bool, error) {
	f.record("cancel:" + sessionID + ":" + runID)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCount++
	if f.cancelErr != nil {
		return false, f.cancelErr
	}
	return f.cancelResult, nil
}
func (f *fakeGatewayClient) ResolvePermission(_ context.Context, requestID string, decision string) error {
	f.record("resolve:" + requestID + ":" + decision)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveCount++
	if f.resolveHook != nil {
		return f.resolveHook(requestID, decision)
	}
	return f.resolveErr
}
func (f *fakeGatewayClient) ResolveUserQuestion(
	_ context.Context,
	requestID string,
	status string,
	values []string,
	message string,
) error {
	f.record("resolve_user_question:" + requestID + ":" + status + ":" + strings.Join(values, ",") + ":" + strings.TrimSpace(message))
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
	chatID               string
	kind                 string
	text                 string
	card                 PermissionCardPayload
	updatedPendingCard   *PermissionCardPayload
	userQuestionCard     UserQuestionCardPayload
	runCard              StatusCardPayload
	cardID               string
	resolvedCard         *ResolvedPermissionCardPayload
	resolvedUserQuestion *ResolvedUserQuestionCardPayload
}

type fakeMessenger struct {
	mu            sync.Mutex
	messages      []sentMessage
	nextID        int
	sendTextErr   error
	sendCardErr   error
	updateCardErr error
	deleteCardErr error
}

func (m *fakeMessenger) SendText(_ context.Context, chatID string, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "text", text: text})
	return m.sendTextErr
}

func (m *fakeMessenger) SendPermissionCard(_ context.Context, chatID string, payload PermissionCardPayload) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cardID := fmt.Sprintf("perm-card-%d", m.nextID)
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "card", card: payload, cardID: cardID})
	return cardID, nil
}

func (m *fakeMessenger) UpdatePermissionCard(_ context.Context, cardID string, payload ResolvedPermissionCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: cardID, kind: "update_perm_card", resolvedCard: &payload})
	return nil
}

func (m *fakeMessenger) UpdatePendingPermissionCard(_ context.Context, cardID string, payload PermissionCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: cardID, kind: "update_pending_perm_card", updatedPendingCard: &payload})
	return nil
}

func (m *fakeMessenger) DeleteMessage(_ context.Context, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: messageID, kind: "delete_card"})
	return m.deleteCardErr
}

func (m *fakeMessenger) SendUserQuestionCard(_ context.Context, chatID string, payload UserQuestionCardPayload) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cardID := fmt.Sprintf("ask-card-%d", m.nextID)
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "ask_card", userQuestionCard: payload, cardID: cardID})
	return cardID, nil
}

func (m *fakeMessenger) UpdateUserQuestionCard(_ context.Context, cardID string, payload ResolvedUserQuestionCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: cardID, kind: "update_ask_card", resolvedUserQuestion: &payload})
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

func TestGroupMessageWithoutMentionAccepted(t *testing.T) {
	adapter := newTestAdapter(t)
	body := messageEventBodyWithChatType("evt-group", "msg-group", "chat-group", "hello group", "group")
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleFeishuEvent(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
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

func TestGroupMessageWithNonBotMentionAccepted(t *testing.T) {
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
	if adapterTestGateway(adapter).runCount != 1 {
		t.Fatalf("run count = %d, want 1", adapterTestGateway(adapter).runCount)
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

func TestGatewayEventsIgnoreStalePermissionRequestedAfterTerminal(t *testing.T) {
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
	for _, message := range msgs {
		if message.kind == "card" && message.card.RequestID == "perm-1" {
			t.Fatalf("unexpected stale permission card after run terminal: %#v", msgs)
		}
	}
}

func TestGatewayUserQuestionRequestedSingleChoiceSendsCard(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-ask-card")
	runID := BuildRunID("msg-ask-card")
	adapter.trackSession(sessionID, runID, "chat-ask-card", "ask task")

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_progress", map[string]any{
		"runtime_event_type": "user_question_requested",
		"payload": map[string]any{
			"request_id":  "ask-card-1",
			"question_id": "q1",
			"title":       "选择部署环境",
			"kind":        "single_choice",
			"allow_skip":  true,
			"options": []any{
				map[string]any{"label": "测试环境"},
				map[string]any{"label": "生产环境"},
			},
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	found := false
	for _, message := range msgs {
		if message.kind == "ask_card" && message.userQuestionCard.RequestID == "ask-card-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ask_user card message, got %#v", msgs)
	}
}

func TestGatewayUserQuestionRequestedTextFallsBackToInstructionText(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-ask-text")
	runID := BuildRunID("msg-ask-text")
	adapter.trackSession(sessionID, runID, "chat-ask-text", "ask text task")

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_progress", map[string]any{
		"runtime_event_type": "user_question_requested",
		"payload": map[string]any{
			"request_id":  "ask-text-1",
			"question_id": "q-text-1",
			"title":       "请输入备注",
			"kind":        "text",
			"allow_skip":  false,
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	foundInstruction := false
	for _, message := range msgs {
		if message.kind == "text" && strings.Contains(message.text, "回答 ask-text-1") {
			foundInstruction = true
			break
		}
	}
	if !foundInstruction {
		t.Fatalf("expected ask_user instruction text, got %#v", msgs)
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

func TestPermissionApprovalDoesNotRevertToPendingAndKeepsResolvedCard(t *testing.T) {
	adapter := newTestAdapter(t)
	adapter.permissionCardDismissDelay = 5 * time.Millisecond
	if err := adapter.bindThenRun(context.Background(), "session-approval", "run-approval", "chat-approval", "执行审批任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-approval", "run-approval", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-revert-1",
			"reason":     "需要审批",
		},
	})
	time.Sleep(20 * time.Millisecond)

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-revert-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("handle card action: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// 网关重复推送同一 permission_requested 时，不应把已审批状态回滚成 pending，也不应重发审批卡片。
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-approval", "run-approval", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-revert-1",
			"reason":     "重复事件",
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	cardSendCount := 0
	updatedResolved := false
	deletedResolvedCard := false
	lastApprovalStatus := ""
	for _, message := range msgs {
		if message.kind == "card" && message.card.RequestID == "perm-revert-1" {
			cardSendCount++
		}
		if message.kind == "update_perm_card" && message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-revert-1" &&
			message.resolvedCard.Approved {
			updatedResolved = true
		}
		if message.kind == "delete_card" {
			deletedResolvedCard = true
		}
		if message.kind == "update_card" {
			lastApprovalStatus = message.runCard.ApprovalStatus
		}
	}
	if cardSendCount != 1 {
		t.Fatalf("permission card send count = %d, want 1; msgs=%#v", cardSendCount, msgs)
	}
	if !updatedResolved {
		t.Fatalf("expected resolved permission card update, msgs=%#v", msgs)
	}
	if deletedResolvedCard {
		t.Fatalf("resolved permission card should stay visible in queue mode, msgs=%#v", msgs)
	}
	if lastApprovalStatus != "approved" {
		t.Fatalf("last approval status = %q, want approved; msgs=%#v", lastApprovalStatus, msgs)
	}
}

func TestPermissionApprovalsDoNotAutoPushQueuedCardOnResolve(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-queue", "run-queue", "chat-queue", "执行审批队列任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-queue", "run-queue", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-queue-1",
			"reason":     "审批一",
		},
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-queue", "run-queue", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-queue-2",
			"reason":     "审批二",
		},
	})
	time.Sleep(40 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	cardSendBeforeResolve := 0
	for _, message := range msgs {
		if message.kind == "card" {
			cardSendBeforeResolve++
		}
	}
	if cardSendBeforeResolve != 1 {
		t.Fatalf("permission cards before resolve = %d, want 1; msgs=%#v", cardSendBeforeResolve, msgs)
	}

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-queue-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("handle first card action: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	msgs = adapterTestMessenger(adapter).snapshot()
	totalCardSend := 0
	resolvedUpdates := 0
	pendingUpdates := 0
	for _, message := range msgs {
		if message.kind == "card" {
			totalCardSend++
		}
		if message.kind == "update_perm_card" && message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-queue-1" {
			resolvedUpdates++
		}
		if message.kind == "update_pending_perm_card" && message.updatedPendingCard != nil &&
			message.updatedPendingCard.RequestID == "perm-queue-2" {
			pendingUpdates++
		}
	}
	if totalCardSend != 1 {
		t.Fatalf("permission cards total after first resolve = %d, want 1; msgs=%#v", totalCardSend, msgs)
	}
	if resolvedUpdates == 0 {
		t.Fatalf("expected resolved state visible before switching to next pending, msgs=%#v", msgs)
	}
	if pendingUpdates == 0 {
		t.Fatalf("expected pending card remains focused on newer request, msgs=%#v", msgs)
	}
}

func TestPermissionCardReusedAcrossSequentialRequests(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-reuse", "run-reuse", "chat-reuse", "执行审批复用任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-reuse", "run-reuse", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-reuse-1",
			"reason":     "审批一",
		},
	})
	time.Sleep(30 * time.Millisecond)

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-reuse-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("handle first card action: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-reuse", "run-reuse", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-reuse-2",
			"reason":     "审批二",
		},
	})
	time.Sleep(40 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	cardSends := 0
	pendingUpdates := 0
	resolvedUpdates := 0
	lastPendingRequestID := ""
	for _, message := range msgs {
		switch message.kind {
		case "card":
			cardSends++
		case "update_pending_perm_card":
			pendingUpdates++
			if message.updatedPendingCard != nil {
				lastPendingRequestID = message.updatedPendingCard.RequestID
			}
		case "update_perm_card":
			resolvedUpdates++
		}
	}
	if cardSends != 1 {
		t.Fatalf("permission card sends = %d, want 1; msgs=%#v", cardSends, msgs)
	}
	if pendingUpdates < 1 {
		t.Fatalf("expected pending permission card update for second request, msgs=%#v", msgs)
	}
	if resolvedUpdates < 1 {
		t.Fatalf("expected resolved permission card update for first request, msgs=%#v", msgs)
	}
	if lastPendingRequestID != "perm-reuse-2" {
		t.Fatalf("last pending request id = %q, want perm-reuse-2; msgs=%#v", lastPendingRequestID, msgs)
	}
}

func TestPermissionQueuedRequestDoesNotOverrideActiveCardBeforeResolve(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-queue-order", "run-queue-order", "chat-queue-order", "执行审批时序任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-queue-order", "run-queue-order", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-order-1",
			"reason":     "审批一",
		},
	})
	time.Sleep(30 * time.Millisecond)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-queue-order", "run-queue-order", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-order-2",
			"reason":     "审批二",
		},
	})
	time.Sleep(60 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	for _, message := range msgs {
		if message.kind == "update_pending_perm_card" &&
			message.updatedPendingCard != nil &&
			message.updatedPendingCard.RequestID == "perm-order-2" {
			t.Fatalf("queued pending request should not override active card before resolve, msgs=%#v", msgs)
		}
	}

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-order-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("handle first card action: %v", err)
	}
	time.Sleep(750 * time.Millisecond)

	msgs = adapterTestMessenger(adapter).snapshot()
	resolvedIndex := -1
	pendingIndex := -1
	for i, message := range msgs {
		if message.kind == "update_perm_card" &&
			message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-order-1" {
			resolvedIndex = i
		}
		if message.kind == "update_pending_perm_card" &&
			message.updatedPendingCard != nil &&
			message.updatedPendingCard.RequestID == "perm-order-2" {
			pendingIndex = i
		}
	}
	if resolvedIndex < 0 {
		t.Fatalf("expected resolved card update for first request, msgs=%#v", msgs)
	}
	if pendingIndex < 0 {
		t.Fatalf("expected switch to second pending request after resolve, msgs=%#v", msgs)
	}
	if pendingIndex <= resolvedIndex {
		t.Fatalf("pending switch should happen after resolved card update, msgs=%#v", msgs)
	}
}

func TestPermissionActionFallsBackToCurrentPendingRequestWhenStale(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-stale", "run-stale", "chat-stale", "执行审批回退任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-stale", "run-stale", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-stale-1",
			"reason":     "审批一",
		},
	})
	time.Sleep(20 * time.Millisecond)

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-stale-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("resolve first permission: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-stale", "run-stale", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-stale-2",
			"reason":     "审批二",
		},
	})
	time.Sleep(30 * time.Millisecond)
	permissionCardID := ""
	for _, message := range adapterTestMessenger(adapter).snapshot() {
		if message.kind == "card" {
			permissionCardID = message.cardID
		}
	}
	if permissionCardID == "" {
		t.Fatal("expected permission card id for stale fallback test")
	}

	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.resolveHook = func(requestID string, _ string) error {
		if requestID == "perm-stale-1" {
			return assertErr(`runtime: permission request "perm-stale-1" not found`)
		}
		return nil
	}
	gateway.mu.Unlock()

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-stale-1",
		CardID:    permissionCardID,
		Decision:  "reject",
	}); err != nil {
		t.Fatalf("resolve stale permission should fallback: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	calls := gateway.snapshotCalls()
	resolveStale := 0
	resolveFallback := 0
	for _, call := range calls {
		if call == "resolve:perm-stale-1:reject" {
			resolveStale++
		}
		if call == "resolve:perm-stale-2:reject" {
			resolveFallback++
		}
	}
	if resolveStale == 0 || resolveFallback == 0 {
		t.Fatalf("expected stale+fallback resolve calls, got %#v", calls)
	}

	msgs := adapterTestMessenger(adapter).snapshot()
	foundRejected := false
	for _, message := range msgs {
		if message.kind == "update_perm_card" && message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-stale-2" && !message.resolvedCard.Approved {
			foundRejected = true
			break
		}
	}
	if !foundRejected {
		t.Fatalf("expected fallback request resolved card update, msgs=%#v", msgs)
	}
}

func TestPermissionActionIgnoresAlreadyResolvedRequestOnOpaquePrimaryError(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-opaque", "run-opaque", "chat-opaque", "执行审批回退任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-opaque", "run-opaque", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-opaque-1",
			"reason":     "审批一",
		},
	})
	time.Sleep(20 * time.Millisecond)
	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-opaque-1",
		Decision:  "allow_once",
	}); err != nil {
		t.Fatalf("resolve first permission: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-opaque", "run-opaque", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-opaque-2",
			"reason":     "审批二",
		},
	})
	time.Sleep(30 * time.Millisecond)

	permissionCardID := ""
	for _, message := range adapterTestMessenger(adapter).snapshot() {
		if message.kind == "card" {
			permissionCardID = message.cardID
		}
	}
	if permissionCardID == "" {
		t.Fatal("expected permission card id for opaque fallback test")
	}

	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.resolveHook = func(requestID string, _ string) error {
		if strings.TrimSpace(requestID) == "perm-opaque-1" {
			return assertErr("gateway rpc gateway.resolvePermission failed (internal_error): resolve_permission failed")
		}
		return nil
	}
	gateway.mu.Unlock()

	err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		EventID:   "evt-opaque-fallback",
		RequestID: "perm-opaque-1",
		Decision:  "allow_once",
		ActionType: "permission",
		CardID:    permissionCardID,
	})
	if err != nil {
		t.Fatalf("resolve opaque stale permission should be ignored: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	calls := gateway.snapshotCalls()
	resolvePrimary := 0
	for _, call := range calls {
		if call == "resolve:perm-opaque-1:allow_once" {
			resolvePrimary++
		}
	}
	if resolvePrimary != 1 {
		t.Fatalf("expected only initial resolve call for perm-opaque-1, got %#v", calls)
	}
	for _, call := range calls {
		if call == "resolve:perm-opaque-2:allow_once" {
			t.Fatalf("stale callback must not fallback-resolve next pending request, got %#v", calls)
		}
	}
}

func TestHandleCardActionAcceptsAllowAliasDecision(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-allow-alias", "run-allow-alias", "chat-allow-alias", "执行审批别名任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-allow-alias", "run-allow-alias", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-allow-alias-1",
			"reason":     "审批别名",
		},
	})
	time.Sleep(30 * time.Millisecond)

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		RequestID: "perm-allow-alias-1",
		Decision:  "allow",
	}); err != nil {
		t.Fatalf("handle allow alias decision: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	foundResolved := false
	for _, message := range msgs {
		if message.kind == "update_perm_card" && message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-allow-alias-1" && message.resolvedCard.Approved {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("expected resolved permission card update for allow alias, msgs=%#v", msgs)
	}
}

func TestHandleMessageInterruptsPreviousPendingRunInSameChat(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.cancelResult = true
	gateway.mu.Unlock()

	first := FeishuMessageEvent{
		EventID:     "evt-int-1",
		MessageID:   "msg-int-1",
		ChatID:      "chat-interrupt",
		ChatType:    "p2p",
		ContentText: "第一条任务",
	}
	second := FeishuMessageEvent{
		EventID:     "evt-int-2",
		MessageID:   "msg-int-2",
		ChatID:      "chat-interrupt",
		ChatType:    "p2p",
		ContentText: "第二条任务",
	}
	if err := adapter.HandleMessage(context.Background(), first); err != nil {
		t.Fatalf("handle first message: %v", err)
	}
	if err := adapter.HandleMessage(context.Background(), second); err != nil {
		t.Fatalf("handle second message: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	sessionID := BuildSessionID("chat-interrupt")
	firstRunID := BuildRunID("msg-int-1")
	secondRunID := BuildRunID("msg-int-2")

	calls := gateway.snapshotCalls()
	wantCancel := "cancel:" + sessionID + ":" + firstRunID
	foundCancel := false
	for _, call := range calls {
		if call == wantCancel {
			foundCancel = true
			break
		}
	}
	if !foundCancel {
		t.Fatalf("expected cancel call %q, got %#v", wantCancel, calls)
	}

	msgs := adapterTestMessenger(adapter).snapshot()
	foundInterrupted := false
	for _, message := range msgs {
		if message.kind == "update_card" &&
			message.runCard.Status == "interrupted" &&
			message.runCard.Result == "interrupted" {
			foundInterrupted = true
			break
		}
	}
	if !foundInterrupted {
		t.Fatalf("expected interrupted status card update, msgs=%#v", msgs)
	}

	adapter.mu.RLock()
	_, oldExists := adapter.activeRuns[runBindingKey(sessionID, firstRunID)]
	_, newExists := adapter.activeRuns[runBindingKey(sessionID, secondRunID)]
	adapter.mu.RUnlock()
	if oldExists {
		t.Fatal("expected old run to be untracked after interrupt")
	}
	if !newExists {
		t.Fatal("expected new run to stay active")
	}
}

func TestPermissionResolvedEventUpdatesPermissionCard(t *testing.T) {
	adapter := newTestAdapter(t)
	if err := adapter.bindThenRun(context.Background(), "session-resolve", "run-resolve", "chat-resolve", "执行审批事件任务"); err != nil {
		t.Fatalf("bindThenRun: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	pushGatewayEvent(t, adapterTestGateway(adapter), "session-resolve", "run-resolve", "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-resolve-1",
			"tool_name":  "filesystem_write_file",
			"reason":     "需要写文件权限",
		},
	})
	pushGatewayEvent(t, adapterTestGateway(adapter), "session-resolve", "run-resolve", "run_progress", map[string]any{
		"runtime_event_type": "permission_resolved",
		"payload": map[string]any{
			"request_id": "perm-resolve-1",
			"decision":   "allow",
		},
	})
	time.Sleep(40 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	updatedResolved := false
	lastApprovalStatus := ""
	for _, message := range msgs {
		if message.kind == "update_perm_card" && message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-resolve-1" && message.resolvedCard.Approved {
			updatedResolved = true
		}
		if message.kind == "update_card" {
			lastApprovalStatus = message.runCard.ApprovalStatus
		}
	}
	if !updatedResolved {
		t.Fatalf("expected resolved permission card update from permission_resolved event, msgs=%#v", msgs)
	}
	if lastApprovalStatus != "approved" {
		t.Fatalf("last approval status = %q, want approved; msgs=%#v", lastApprovalStatus, msgs)
	}
}

func TestRunTerminalFinalizesPermissionCardUsingRunScopedCardFallback(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-terminal-perm")
	runID := BuildRunID("msg-terminal-perm")
	chatID := "chat-terminal-perm"
	adapter.trackSession(sessionID, runID, chatID, "terminal permission task")
	if err := adapter.ensureRunCard(context.Background(), sessionID, runID); err != nil {
		t.Fatalf("ensureRunCard: %v", err)
	}

	key := runBindingKey(sessionID, runID)
	adapter.mu.Lock()
	binding := adapter.activeRuns[key]
	binding.ApprovalRecords = []approvalEntry{
		{
			RequestID: "perm-terminal-1",
			ToolName:  "filesystem_write_file",
			Operation: "write_file",
			Target:    "1.txt",
			Reason:    "需要写文件权限",
			Decision:  "allow_once",
		},
	}
	adapter.activeRuns[key] = binding
	adapter.runPermissionCards[key] = "perm-card-fallback"
	adapter.permissionCardRuns["perm-card-fallback"] = key
	delete(adapter.permissionCards, "perm-terminal-1")
	delete(adapter.requestRuns, "perm-terminal-1")
	adapter.mu.Unlock()

	adapter.markRunTerminal(sessionID, runID, "success", "done", "")

	msgs := adapterTestMessenger(adapter).snapshot()
	foundResolved := false
	for _, message := range msgs {
		if message.kind == "update_perm_card" &&
			message.chatID == "perm-card-fallback" &&
			message.resolvedCard != nil &&
			message.resolvedCard.RequestID == "perm-terminal-1" &&
			message.resolvedCard.Approved {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("expected terminal fallback to finalize permission card, msgs=%#v", msgs)
	}
}

func TestHandleCardActionUserQuestionResolvesAndUpdatesCard(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-ask-resolve")
	runID := BuildRunID("msg-ask-resolve")
	adapter.trackSession(sessionID, runID, "chat-ask-resolve", "ask resolve task")
	adapter.markUserQuestionPending(sessionID, runID, userQuestionEntry{
		RequestID:   "ask-resolve-1",
		QuestionID:  "q-resolve-1",
		Title:       "选择分支",
		Kind:        "single_choice",
		AllowSkip:   true,
		Options:     []UserQuestionCardOption{{Label: "main"}},
		Description: "请选择目标分支",
	})
	adapter.mu.Lock()
	adapter.userQuestionCards["ask-resolve-1"] = "ask-card-1"
	adapter.mu.Unlock()

	if err := adapter.HandleCardAction(context.Background(), FeishuCardActionEvent{
		ActionType: "user_question",
		RequestID:  "ask-resolve-1",
		Status:     "answered",
		Values:     []string{"main"},
	}); err != nil {
		t.Fatalf("handle card action: %v", err)
	}

	if adapterTestGateway(adapter).resolveCount != 1 {
		t.Fatalf("resolve count = %d, want 1", adapterTestGateway(adapter).resolveCount)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	foundUpdate := false
	for _, message := range msgs {
		if message.kind == "update_ask_card" && message.resolvedUserQuestion != nil &&
			message.resolvedUserQuestion.RequestID == "ask-resolve-1" {
			foundUpdate = true
			break
		}
	}
	if !foundUpdate {
		t.Fatalf("expected ask card update, got %#v", msgs)
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

func TestRefreshActiveCardsDoesNotForceFailStalledRun(t *testing.T) {
	adapter := newTestAdapter(t)
	base := time.Now().UTC()
	adapter.nowFn = func() time.Time { return base }

	sessionID := BuildSessionID("chat-stalled")
	runID := BuildRunID("msg-stalled")
	adapter.trackSession(sessionID, runID, "chat-stalled", "stalled task")
	if err := adapter.ensureRunCard(context.Background(), sessionID, runID); err != nil {
		t.Fatalf("ensureRunCard: %v", err)
	}

	adapter.nowFn = func() time.Time { return base.Add(defaultRunStallTimeout + 5*time.Second) }
	adapter.refreshActiveCards(context.Background())

	adapter.mu.RLock()
	binding, exists := adapter.activeRuns[runBindingKey(sessionID, runID)]
	adapter.mu.RUnlock()
	if !exists {
		t.Fatalf("expected stalled run to stay tracked")
	}
	if strings.TrimSpace(strings.ToLower(binding.Result)) != "pending" {
		t.Fatalf("result = %q, want pending", binding.Result)
	}
}

func TestPermissionRequestedAfterRunTerminalDoesNotReopenApprovalCard(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-stale-perm")
	runID := BuildRunID("msg-stale-perm")
	chatID := "chat-stale-perm"
	adapter.trackSession(sessionID, runID, chatID, "stale permission task")

	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_done", map[string]any{
		"runtime_event_type": "agent_done",
		"payload": map[string]any{
			"content": "done",
		},
	})
	time.Sleep(20 * time.Millisecond)

	// run 已终态且绑定已清理时，乱序/重复 permission_requested 不应重新弹出审批卡片。
	pushGatewayEvent(t, adapterTestGateway(adapter), sessionID, runID, "run_progress", map[string]any{
		"runtime_event_type": "permission_requested",
		"payload": map[string]any{
			"request_id": "perm-stale-1",
			"reason":     "旧事件回放",
		},
	})
	time.Sleep(30 * time.Millisecond)

	msgs := adapterTestMessenger(adapter).snapshot()
	for _, message := range msgs {
		if message.kind == "card" && message.card.RequestID == "perm-stale-1" {
			t.Fatalf("unexpected stale permission card after terminal run: %#v", msgs)
		}
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

func TestCardCallbackUserQuestionAnswerAccepted(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-callback-ask")
	runID := BuildRunID("msg-callback-ask")
	adapter.trackSession(sessionID, runID, "chat-callback-ask", "ask callback task")
	adapter.markUserQuestionPending(sessionID, runID, userQuestionEntry{
		RequestID:  "ask-callback-1",
		QuestionID: "q-callback-1",
		Title:      "选择模式",
		Kind:       "single_choice",
		Options:    []UserQuestionCardOption{{Label: "快速"}},
	})

	body := `{"action":{"value":{"action_type":"user_question","request_id":"ask-callback-1","status":"answered","value":"快速"}},"token":"verify"}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if adapterTestGateway(adapter).resolveCount != 1 {
		t.Fatalf("resolve count = %d, want 1", adapterTestGateway(adapter).resolveCount)
	}
	if !strings.Contains(recorder.Body.String(), "回答已提交") {
		t.Fatalf("response = %s, want ask toast", recorder.Body.String())
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

func TestCardCallbackInvalidActionPayloadReturnsInfoWithoutResolve(t *testing.T) {
	adapter := newTestAdapter(t)
	body := `{"action":{"value":{"action_type":"permission","request_id":"perm-x","decision":"allow_all"}},"token":"verify","header":{"token":"verify"}}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "callback ready") {
		t.Fatalf("response = %s, want callback ready", recorder.Body.String())
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

func TestTryHandleTextPermissionHandlesAskUserAnswerAndSkip(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-ask-text-cmd")
	runID := BuildRunID("msg-ask-text-cmd")
	adapter.trackSession(sessionID, runID, "chat-ask-text-cmd", "ask cmd task")
	adapter.markUserQuestionPending(sessionID, runID, userQuestionEntry{
		RequestID:  "ask-cmd-1",
		QuestionID: "q-cmd-1",
		Title:      "选择发布环境",
		Kind:       "single_choice",
		Options: []UserQuestionCardOption{
			{Label: "测试环境"},
			{Label: "生产环境"},
		},
		AllowSkip: true,
	})

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-ask-text-cmd", "回答 ask-cmd-1 测试环境")
	if err != nil || !handled {
		t.Fatalf("answer command = handled:%v err:%v", handled, err)
	}
	handled, err = adapter.tryHandleTextPermission(context.Background(), "chat-ask-text-cmd", "跳过 ask-cmd-1")
	if err != nil || !handled {
		t.Fatalf("skip command = handled:%v err:%v", handled, err)
	}
	if adapterTestGateway(adapter).resolveCount < 2 {
		t.Fatalf("resolve count = %d, want >=2", adapterTestGateway(adapter).resolveCount)
	}
}

func TestTryHandleTextPermissionRejectsInvalidChoiceAnswer(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-ask-invalid-choice")
	runID := BuildRunID("msg-ask-invalid-choice")
	adapter.trackSession(sessionID, runID, "chat-ask-invalid-choice", "ask invalid choice task")
	adapter.markUserQuestionPending(sessionID, runID, userQuestionEntry{
		RequestID:  "ask-invalid-choice-1",
		QuestionID: "q-invalid-choice-1",
		Title:      "选择发布环境",
		Kind:       "single_choice",
		Options: []UserQuestionCardOption{
			{Label: "测试环境"},
			{Label: "生产环境"},
		},
		AllowSkip: true,
	})

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-ask-invalid-choice", "回答 ask-invalid-choice-1 staging")
	if err != nil || !handled {
		t.Fatalf("invalid single_choice answer = handled:%v err:%v", handled, err)
	}
	if adapterTestGateway(adapter).resolveCount != 0 {
		t.Fatalf("resolve count = %d, want 0 for invalid answer", adapterTestGateway(adapter).resolveCount)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 || msgs[len(msgs)-1].text != "回答格式无效，请使用：回答 <request_id> <内容>" {
		t.Fatalf("unexpected invalid answer reply: %#v", msgs)
	}
}

func TestTryHandleTextPermissionRejectsMultiChoiceExceedingMaxChoices(t *testing.T) {
	adapter := newTestAdapter(t)
	sessionID := BuildSessionID("chat-ask-max-choice")
	runID := BuildRunID("msg-ask-max-choice")
	adapter.trackSession(sessionID, runID, "chat-ask-max-choice", "ask max choice task")
	adapter.markUserQuestionPending(sessionID, runID, userQuestionEntry{
		RequestID:  "ask-max-choice-1",
		QuestionID: "q-max-choice-1",
		Title:      "选择要发布的区域",
		Kind:       "multi_choice",
		Options: []UserQuestionCardOption{
			{Label: "华北"},
			{Label: "华东"},
			{Label: "华南"},
		},
		MaxChoices: 2,
		AllowSkip:  true,
	})

	handled, err := adapter.tryHandleTextPermission(context.Background(), "chat-ask-max-choice", "回答 ask-max-choice-1 华北,华东,华南")
	if err != nil || !handled {
		t.Fatalf("max_choices exceed answer = handled:%v err:%v", handled, err)
	}
	if adapterTestGateway(adapter).resolveCount != 0 {
		t.Fatalf("resolve count = %d, want 0 when max_choices exceeded", adapterTestGateway(adapter).resolveCount)
	}
	msgs := adapterTestMessenger(adapter).snapshot()
	if len(msgs) == 0 || msgs[len(msgs)-1].text != "回答格式无效，请使用：回答 <request_id> <内容>" {
		t.Fatalf("unexpected max_choices reply: %#v", msgs)
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
	requestID, toolName, operation, target, reason := extractPermissionRequest(nil)
	if requestID != "" || toolName != "" || operation != "" || target != "" || reason == "" {
		t.Fatalf("unexpected permission extraction: request=%q tool=%q op=%q target=%q reason=%q", requestID, toolName, operation, target, reason)
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
	if text := extractUserVisibleErrorText(map[string]any{
		"payload": map[string]any{"error": "runner_offline"},
	}); text != "本机 Runner 未连接，请在电脑上启动 `neocode runner`" {
		t.Fatalf("runner error text = %q", text)
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
	if stalled := shouldMarkRunStalled(sessionBinding{
		Result:         "pending",
		ApprovalStatus: "none",
		LastEventTime:  time.Now().UTC().Add(-(defaultRunStallTimeout + time.Second)),
	}, time.Now().UTC()); !stalled {
		t.Fatal("expected stalled run to be detected")
	}
	if stalled := shouldMarkRunStalled(sessionBinding{
		Result:         "pending",
		ApprovalStatus: "pending",
		LastEventTime:  time.Now().UTC().Add(-(defaultRunStallTimeout + time.Second)),
	}, time.Now().UTC()); stalled {
		t.Fatal("did not expect approval-pending run to be marked stalled")
	}
	if stalled := shouldMarkRunStalled(sessionBinding{
		Result:         "success",
		ApprovalStatus: "none",
		LastEventTime:  time.Now().UTC().Add(-(defaultRunStallTimeout + time.Second)),
	}, time.Now().UTC()); stalled {
		t.Fatal("did not expect terminal run to be marked stalled")
	}
	if status := terminalStatusFromResult("success"); status != "success" {
		t.Fatalf("terminal status = %q, want success", status)
	}
	if status := terminalStatusFromResult("failure"); status != "failure" {
		t.Fatalf("terminal status = %q, want failure", status)
	}
	if status := terminalStatusFromResult("unknown"); status != "running" {
		t.Fatalf("terminal status = %q, want running fallback", status)
	}
	if text := buildTerminalFallbackText("success", "执行完成"); text != "任务已完成：\n执行完成" {
		t.Fatalf("terminal fallback text = %q, want success summary", text)
	}
	if text := buildTerminalFallbackText("failure", "命令执行失败"); text != "任务执行失败：\n命令执行失败" {
		t.Fatalf("terminal fallback text = %q, want failure summary", text)
	}
	if text := buildTerminalFallbackText("failure", ""); text != "任务执行失败，请稍后重试。" {
		t.Fatalf("terminal fallback text = %q, want failure default", text)
	}
	safeLogAdapter := &Adapter{}
	safeLogAdapter.safeLog("ignored")
}

func TestAskUserHelperFunctionsCoverFallbackBranches(t *testing.T) {
	resolved := extractUserQuestionResolved(map[string]any{
		"payload": map[string]any{
			"request_id": " ask-1 ",
			"status":     " Answered ",
			"message":    " 已确认 ",
			"values":     []any{" 选项A ", "", 123, "选项B"},
		},
	})
	if resolved.RequestID != "ask-1" || resolved.Status != "answered" || resolved.Message != "已确认" {
		t.Fatalf("unexpected resolved payload: %#v", resolved)
	}
	if len(resolved.Values) != 2 || resolved.Values[0] != "选项A" || resolved.Values[1] != "选项B" {
		t.Fatalf("resolved values = %#v, want trimmed string values", resolved.Values)
	}
	if fallback := extractUserQuestionResolved(nil); fallback.RequestID != "" || fallback.Status != "" || fallback.Message != "" ||
		len(fallback.Values) != 0 {
		t.Fatalf("nil payload fallback = %#v", fallback)
	}

	if !shouldSendAskUserCard(userQuestionEntry{Kind: "single_choice", Options: []UserQuestionCardOption{{Label: "A"}}}) {
		t.Fatal("expected single choice question to send card")
	}
	if shouldSendAskUserCard(userQuestionEntry{Kind: "text"}) {
		t.Fatal("expected text question without skip to fall back to plain text")
	}
	if !shouldSendAskUserCard(userQuestionEntry{Kind: "text", AllowSkip: true}) {
		t.Fatal("expected skip-enabled text question to send card")
	}

	if !isUserQuestionResolvedEvent(" user_question_timeout ") {
		t.Fatal("expected timeout runtime type to be resolved event")
	}
	if isUserQuestionResolvedEvent("user_question_requested") {
		t.Fatal("did not expect requested runtime type to be resolved event")
	}
	if status := userQuestionStatusFromRuntimeType(" user_question_skipped "); status != "skipped" {
		t.Fatalf("status = %q, want skipped", status)
	}
	if status := userQuestionStatusFromRuntimeType("user_question_timeout"); status != "timeout" {
		t.Fatalf("status = %q, want timeout", status)
	}
	if status := userQuestionStatusFromRuntimeType("user_question_answered"); status != "answered" {
		t.Fatalf("status = %q, want answered", status)
	}

	prompt := buildAskUserTextPrompt(userQuestionEntry{
		RequestID:   "ask-2",
		Title:       "选择部署环境",
		Description: "请确认本次发布目标",
		Options: []UserQuestionCardOption{
			{Label: "测试"},
			{Label: "生产"},
		},
		AllowSkip: true,
	})
	if !strings.Contains(prompt, "选择部署环境") || !strings.Contains(prompt, "可选项：测试 / 生产") {
		t.Fatalf("prompt = %q, want title and option labels", prompt)
	}
	if !strings.Contains(prompt, "请回复：回答 ask-2 <内容>") || !strings.Contains(prompt, "如需跳过：跳过 ask-2") {
		t.Fatalf("prompt = %q, want answer and skip instructions", prompt)
	}
	if fallbackPrompt := buildAskUserTextPrompt(userQuestionEntry{}); !strings.Contains(fallbackPrompt, "请回答以下问题") {
		t.Fatalf("fallback prompt = %q, want default title", fallbackPrompt)
	}

	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{}, "skipped", nil, ""); summary != "用户已跳过该问题" {
		t.Fatalf("skip summary = %q", summary)
	}
	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{}, "timeout", nil, ""); summary != "问题等待超时" {
		t.Fatalf("timeout summary = %q", summary)
	}
	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{}, "answered", nil, " 已提交 "); summary != "用户回答：已提交" {
		t.Fatalf("message summary = %q", summary)
	}
	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{}, "answered", []string{"A", "B"}, ""); summary != "用户回答：A, B" {
		t.Fatalf("values summary = %q", summary)
	}
	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{Title: "选择模式"}, "answered", nil, ""); summary != "用户已回答：选择模式" {
		t.Fatalf("title summary = %q", summary)
	}
	if summary := buildUserQuestionResolvedSummary(userQuestionEntry{}, "answered", nil, ""); summary != "用户已回答问题" {
		t.Fatalf("fallback summary = %q", summary)
	}

	if value := readInt(nil, "count"); value != 0 {
		t.Fatalf("readInt nil map = %d, want 0", value)
	}
	intCases := []struct {
		name string
		raw  any
		want int
	}{
		{name: "int", raw: int(3), want: 3},
		{name: "int32", raw: int32(4), want: 4},
		{name: "int64", raw: int64(5), want: 5},
		{name: "float64", raw: float64(6), want: 6},
		{name: "json number", raw: json.Number("7"), want: 7},
		{name: "invalid", raw: json.Number("bad"), want: 0},
	}
	for _, testCase := range intCases {
		t.Run(testCase.name, func(t *testing.T) {
			if value := readInt(map[string]any{"count": testCase.raw}, "count"); value != testCase.want {
				t.Fatalf("readInt(%s) = %d, want %d", testCase.name, value, testCase.want)
			}
		})
	}
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
