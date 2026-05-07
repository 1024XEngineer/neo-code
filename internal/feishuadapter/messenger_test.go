package feishuadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type queuedHTTPResponse struct {
	status int
	body   string
	err    error
}

type queuedHTTPClient struct {
	mu        sync.Mutex
	responses []queuedHTTPResponse
	requests  []*http.Request
	bodies    [][]byte
}

func (c *queuedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return nil, assertErr("unexpected http call")
	}
	if req != nil {
		cloned := req.Clone(req.Context())
		if req.Body != nil {
			body, _ := io.ReadAll(req.Body)
			cloned.Body = io.NopCloser(bytes.NewReader(body))
			req.Body = io.NopCloser(bytes.NewReader(body))
			c.bodies = append(c.bodies, body)
		}
		c.requests = append(c.requests, cloned)
	}
	current := c.responses[0]
	c.responses = c.responses[1:]
	if current.err != nil {
		return nil, current.err
	}
	return &http.Response{
		StatusCode: current.status,
		Body:       io.NopCloser(strings.NewReader(current.body)),
		Header:     make(http.Header),
	}, nil
}

func TestSendMessageRequiresFeishuBusinessCodeZero(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`,
			},
			{
				status: 200,
				body:   `{"code":999,"msg":"forbidden"}`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	err := messenger.SendText(context.Background(), "chat-id", "hello")
	if err == nil {
		t.Fatal("expected send message business error")
	}
	if !strings.Contains(err.Error(), "code=999") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessageSuccessWhenHTTPAndBusinessCodePass(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`,
			},
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","data":{"message_id":"mid"}}`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	if err := messenger.SendText(context.Background(), "chat-id", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func TestSendPermissionCardUsesInteractiveMessage(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`,
			},
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","data":{"message_id":"mid"}}`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	if err := messenger.SendPermissionCard(context.Background(), "chat-id", PermissionCardPayload{
		RequestID: "perm-1",
		Message:   "需要审批",
	}); err != nil {
		t.Fatalf("send permission card: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.requests))
	}
	var payload map[string]any
	if err := json.Unmarshal(client.bodies[1], &payload); err != nil {
		t.Fatalf("decode send body: %v", err)
	}
	if payload["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v, want interactive", payload["msg_type"])
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "allow_once") || !strings.Contains(content, "perm-1") {
		t.Fatalf("content = %q, want permission buttons", content)
	}
}

func TestSendMessageReturnsInvalidJSONOnHTTPFailure(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`,
			},
			{
				status: 500,
				body:   `{`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	err := messenger.SendText(context.Background(), "chat-id", "hello")
	if err == nil || !strings.Contains(err.Error(), "body=invalid_json") {
		t.Fatalf("error = %v, want invalid_json failure", err)
	}
}

func TestTenantAccessTokenUsesCache(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`,
			},
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","data":{"message_id":"mid-1"}}`,
			},
			{
				status: 200,
				body:   `{"code":0,"msg":"ok","data":{"message_id":"mid-2"}}`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	if err := messenger.SendText(context.Background(), "chat-id", "hello"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := messenger.SendText(context.Background(), "chat-id", "hello again"); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(client.requests))
	}
	if !strings.Contains(client.requests[1].Header.Get("Authorization"), "Bearer token") {
		t.Fatalf("authorization header missing cached token: %#v", client.requests[1].Header)
	}
	if !strings.Contains(client.requests[2].Header.Get("Authorization"), "Bearer token") {
		t.Fatalf("authorization header missing cached token on second send: %#v", client.requests[2].Header)
	}
}

func TestMessengerCoversConstructorAndTokenFailures(t *testing.T) {
	defaultMessenger := NewFeishuMessenger(" app ", " secret ", nil)
	if defaultMessenger == nil {
		t.Fatal("expected default messenger")
	}

	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{
				status: 500,
				body:   `{"code":999,"msg":"bad app"}`,
			},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	err := messenger.SendText(context.Background(), "chat-id", "hello")
	if err == nil || !strings.Contains(err.Error(), "fetch feishu tenant token failed") {
		t.Fatalf("error = %v, want token fetch failure", err)
	}
}

func TestSendStatusCardReturnsMessageIDAndUpdateCardUsesPatch(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{status: 200, body: `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`},
			{status: 200, body: `{"code":0,"msg":"ok","data":{"message_id":"card-mid"}}`},
			{status: 200, body: `{"code":0,"msg":"ok","data":{"message_id":"updated"}}`},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	cardID, err := messenger.SendStatusCard(context.Background(), "chat-id", StatusCardPayload{
		TaskName:        "task",
		Status:          "planning",
		ApprovalStatus:  "pending",
		Result:          "success",
		Elapsed:         "3s",
		Summary:         "done",
		AsyncRewakeHint: "async hint",
	})
	if err != nil {
		t.Fatalf("send status card: %v", err)
	}
	if cardID != "card-mid" {
		t.Fatalf("card id = %q, want card-mid", cardID)
	}
	if err := messenger.UpdateCard(context.Background(), "card-mid", StatusCardPayload{TaskName: "task"}); err != nil {
		t.Fatalf("update card: %v", err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(client.requests))
	}
	if client.requests[2].Method != http.MethodPatch {
		t.Fatalf("update method = %s, want PATCH", client.requests[2].Method)
	}
	if got := client.requests[2].URL.Path; !strings.HasSuffix(got, "/open-apis/im/v1/messages/card-mid") {
		t.Fatalf("unexpected update path: %s", got)
	}
}

func TestDoJSONRequestWithMessageIDRejectsInvalidSuccessBody(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{status: 200, body: `{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`},
			{status: 200, body: `{`},
		},
	}
	messenger := NewFeishuMessenger("app", "secret", client)
	err := messenger.SendText(context.Background(), "chat-id", "hello")
	if err == nil || !strings.Contains(err.Error(), "invalid response body") {
		t.Fatalf("error = %v, want invalid response body", err)
	}
}

func TestTenantAccessTokenFallsBackToDefaultExpiry(t *testing.T) {
	client := &queuedHTTPClient{
		responses: []queuedHTTPResponse{
			{status: 200, body: `{"code":0,"msg":"ok","tenant_access_token":"token","expire":0}`},
		},
	}
	token, err := NewFeishuMessenger("app", "secret", client).(*feishuMessenger).tenantAccessToken(context.Background())
	if err != nil {
		t.Fatalf("tenant access token: %v", err)
	}
	if token != "token" {
		t.Fatalf("token = %q, want token", token)
	}
	messenger := NewFeishuMessenger("app", "secret", client).(*feishuMessenger)
	messenger.cachedToken = "cached"
	messenger.expireAt = time.Now().Add(time.Minute)
	token, err = messenger.tenantAccessToken(context.Background())
	if err != nil || token != "cached" {
		t.Fatalf("cached token = %q err=%v", token, err)
	}
}

func TestBuildStatusCardAndHelpers(t *testing.T) {
	card := buildStatusCard(StatusCardPayload{
		TaskName:        "task",
		Status:          "planning",
		ApprovalStatus:  "approved",
		Result:          "failure",
		Elapsed:         "5s",
		Summary:         "summary",
		AsyncRewakeHint: "hint",
	})
	header, _ := card["header"].(map[string]any)
	if header["template"] != "wathet" {
		t.Fatalf("template = %v, want wathet", header["template"])
	}
	elements, _ := card["elements"].([]map[string]any)
	if len(elements) < 7 {
		t.Fatalf("expected detail elements, got %#v", card["elements"])
	}

	if note := statusNoteElement("task"); note["tag"] != "note" {
		t.Fatalf("unexpected note element: %#v", note)
	}
	if bar := statusBarElement("💭", "状态", "thinking"); bar["tag"] != "column_set" {
		t.Fatalf("unexpected bar element: %#v", bar)
	}
	if icon, color := statusIconAndColor("success"); icon != "🎉" || color != "green" {
		t.Fatalf("success icon/color = %q/%q", icon, color)
	}
	if icon, color := statusIconAndColor("unknown"); icon != "🔵" || color != "blue" {
		t.Fatalf("default icon/color = %q/%q", icon, color)
	}
	if value := fallbackStatusField("  ", "fallback"); value != "fallback" {
		t.Fatalf("fallback field = %q", value)
	}
}
