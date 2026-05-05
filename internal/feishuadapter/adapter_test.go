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
	chatID string
	kind   string
	text   string
	card   PermissionCardPayload
}

type fakeMessenger struct {
	mu       sync.Mutex
	messages []sentMessage
}

func (m *fakeMessenger) SendText(_ context.Context, chatID string, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "text", text: text})
	return nil
}

func (m *fakeMessenger) SendPermissionCard(_ context.Context, chatID string, payload PermissionCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "card", card: payload})
	return nil
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
		if err != context.Canceled {
			t.Fatalf("run error = %v, want context canceled", err)
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
	for i := 0; i < 2; i++ {
		request := signedRequest(t, adapter.cfg.SigningSecret, body)
		recorder := httptest.NewRecorder()
		adapter.handleFeishuEvent(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
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
							"user_id": "app",
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

	adapter.trackSession(BuildSessionID("chat-x"), BuildRunID("msg-x"), "chat-x")
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
	if len(msgs) < 3 {
		t.Fatalf("messages = %#v, want >=3", msgs)
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

func TestRunTerminalEventUntracksActiveRun(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-cleanup")
	runID := BuildRunID("msg-cleanup")
	adapter.trackSession(sessionID, runID, "chat-cleanup")

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
	adapter.trackSession(sessionID, runID, "chat-done-text")

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
	if last.kind != "text" || !strings.Contains(last.text, "这是最终回复") {
		t.Fatalf("expected assistant final text, got %#v", last)
	}
}

func TestRunProgressInternalEventsAreNotUserFacing(t *testing.T) {
	adapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.consumeGatewayEvents(ctx)

	sessionID := BuildSessionID("chat-throttle")
	runID := BuildRunID("msg-throttle")
	adapter.trackSession(sessionID, runID, "chat-throttle")

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

func TestCardCallbackResolveFailureKeepsHTTP200(t *testing.T) {
	adapter := newTestAdapter(t)
	gateway := adapterTestGateway(adapter)
	gateway.mu.Lock()
	gateway.resolveErr = assertErr("deny")
	gateway.mu.Unlock()

	body := `{"action":{"value":{"request_id":"perm-3","decision":"reject"}},"token":"verify"}`
	request := signedRequest(t, adapter.cfg.SigningSecret, body)
	recorder := httptest.NewRecorder()
	adapter.handleCardCallback(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "审批提交失败") {
		t.Fatalf("response = %s, want failure toast", recorder.Body.String())
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
	adapter.trackSession("session-a", "run-a", "chat-a")

	ctx, cancel := context.WithCancel(context.Background())
	go adapter.reconnectAndRebindLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	gw.mu.Lock()
	gw.pingErr = nil
	gw.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
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
	adapter.trackSession("session-x", "run-a", "chat-x")
	adapter.trackSession("session-x", "run-b", "chat-x")

	ctx, cancel := context.WithCancel(context.Background())
	go adapter.reconnectAndRebindLoop(ctx)
	time.Sleep(30 * time.Millisecond)
	gw.mu.Lock()
	gw.pingErr = nil
	gw.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
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
	adapter.trackSession("session-steady", "run-steady", "chat-steady")

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
	adapter.trackSession("session-auth-fail", "run-auth-fail", "chat-auth-fail")

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

func TestReadAndVerifyRequestRejectsNonPost(t *testing.T) {
	adapter := newTestAdapter(t)
	request := httptest.NewRequest(http.MethodGet, "/feishu/events", nil)
	recorder := httptest.NewRecorder()
	if body, ok := adapter.readAndVerifyRequest(recorder, request); ok || body != nil {
		t.Fatalf("expected non-post request rejection, body=%q ok=%v", string(body), ok)
	}
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestReadAndVerifyRequestRejectsUnreadableBody(t *testing.T) {
	adapter := newTestAdapter(t)
	request := httptest.NewRequest(http.MethodPost, "/feishu/events", errReader{})
	request.Header.Set(headerLarkTimestamp, strconvTimestamp(time.Now().UTC()))
	request.Header.Set(headerLarkNonce, "nonce")
	request.Header.Set(headerLarkSignature, "sig")
	recorder := httptest.NewRecorder()
	if _, ok := adapter.readAndVerifyRequest(recorder, request); ok {
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
	safeLogAdapter := &Adapter{}
	safeLogAdapter.safeLog("ignored")
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

type assertErr string

func (e assertErr) Error() string { return string(e) }

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, assertErr("read failed")
}
