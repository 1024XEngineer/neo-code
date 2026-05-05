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
	pingErr       error
	runErr        error
	runErrOnce    bool
}

func newFakeGatewayClient() *fakeGatewayClient {
	return &fakeGatewayClient{notifications: make(chan GatewayNotification, 16)}
}

func (f *fakeGatewayClient) Authenticate(context.Context) error {
	f.record("authenticate")
	return nil
}
func (f *fakeGatewayClient) BindStream(_ context.Context, sessionID string, runID string) error {
	f.record("bind:" + sessionID + ":" + runID)
	return nil
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
	f.resolveCount++
	f.mu.Unlock()
	return nil
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
	mu       sync.Mutex
	messages []sentMessage
	nextID   int
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

func (m *fakeMessenger) SendStatusCard(_ context.Context, chatID string, payload StatusCardPayload) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cardID := fmt.Sprintf("card-%d", m.nextID)
	m.messages = append(m.messages, sentMessage{chatID: chatID, kind: "status_card", runCard: payload, cardID: cardID})
	return cardID, nil
}

func (m *fakeMessenger) UpdateCard(_ context.Context, cardID string, payload StatusCardPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{kind: "update_card", runCard: payload, cardID: cardID})
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
	adapter.trackSession("session-x", "run-a", "chat-x", "task-a")
	adapter.trackSession("session-x", "run-b", "chat-x", "task-b")

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

type assertErr string

func (e assertErr) Error() string { return string(e) }
