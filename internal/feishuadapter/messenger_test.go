package feishuadapter

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type queuedHTTPResponse struct {
	status int
	body   string
	err    error
}

type queuedHTTPClient struct {
	mu        sync.Mutex
	responses []queuedHTTPResponse
}

func (c *queuedHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return nil, assertErr("unexpected http call")
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
