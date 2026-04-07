package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestStdIOClientListToolsAndCallTool(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClient(t)
	defer func() { _ = client.Close() }()

	toolsList, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(toolsList) != 1 || toolsList[0].Name != "search" {
		t.Fatalf("unexpected tools list: %+v", toolsList)
	}

	result, err := client.CallTool(context.Background(), "search", []byte(`{"query":"mcp"}`))
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !strings.Contains(result.Content, "search") {
		t.Fatalf("unexpected call result content: %q", result.Content)
	}
}

func TestStdIOClientHealthCheck(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClient(t)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestStdIOClientConcurrentCallTool(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClient(t)
	defer func() { _ = client.Close() }()

	const workers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := client.CallTool(context.Background(), "search", []byte(`{"query":"mcp"}`))
			if err != nil {
				errCh <- err
				return
			}
			if !strings.Contains(result.Content, "search") {
				errCh <- fmt.Errorf("unexpected content: %q", result.Content)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent call failed: %v", err)
	}
}

func TestReadFramedMessageRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 32)
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", maxStdioFrameBytes+1, payload)
	reader := bufio.NewReader(strings.NewReader(raw))
	_, err := readFramedMessage(reader)
	if err == nil {
		t.Fatalf("expected oversized payload error")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected exceeds limit error, got %v", err)
	}
}

func TestNewStdIOClientValidationAndDefaults(t *testing.T) {
	t.Parallel()

	if _, err := NewStdIOClient(StdioClientConfig{}); err == nil {
		t.Fatalf("expected empty command error")
	}
	client, err := NewStdIOClient(StdioClientConfig{Command: "cmd"})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	if client.cfg.StartTimeout <= 0 || client.cfg.CallTimeout <= 0 || client.cfg.RestartBackoff <= 0 {
		t.Fatalf("expected default timeouts/backoff to be initialized")
	}
}

func TestStdIOClientCallToolInputValidation(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{}
	if _, err := client.CallTool(context.Background(), "", nil); err == nil {
		t.Fatalf("expected empty tool name error")
	}
	if _, err := client.CallTool(context.Background(), "search", []byte("{not-json")); err == nil {
		t.Fatalf("expected invalid json arguments error")
	}
}

func TestStdIOClientCallRejectsClosedAndDisconnected(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{
		pending: make(map[string]chan rpcReply),
		cfg: StdioClientConfig{
			CallTimeout:    time.Second,
			StartTimeout:   time.Second,
			RestartBackoff: time.Millisecond,
		},
	}
	client.shutdown = true
	if _, err := client.call(context.Background(), "tools/list", map[string]any{}); err == nil {
		t.Fatalf("expected closed error")
	}

	client.shutdown = false
	client.started = true
	client.stdin = nil
	if _, err := client.call(context.Background(), "tools/list", map[string]any{}); err == nil {
		t.Fatalf("expected disconnected error")
	}
}

func TestStdIOClientEnsureStartedBackoff(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{
		cfg: StdioClientConfig{
			Command:        "cmd",
			StartTimeout:   time.Second,
			CallTimeout:    time.Second,
			RestartBackoff: time.Second,
		},
		pending: make(map[string]chan rpcReply),
		retryAt: time.Now().Add(2 * time.Second),
	}

	err := client.ensureStarted(context.Background())
	if err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Fatalf("expected backoff error, got %v", err)
	}
}

func TestReadFramedMessageHeaderErrors(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("X-Test: 1\r\n\r\n{}"))
	if _, err := readFramedMessage(reader); err == nil || !strings.Contains(err.Error(), "missing content-length") {
		t.Fatalf("expected missing content-length error, got %v", err)
	}

	reader = bufio.NewReader(strings.NewReader("Content-Length: nope\r\n\r\n{}"))
	if _, err := readFramedMessage(reader); err == nil || !strings.Contains(err.Error(), "invalid content-length") {
		t.Fatalf("expected invalid content-length error, got %v", err)
	}
}

func TestDecodeCallResultVariants(t *testing.T) {
	t.Parallel()

	result := decodeCallResult(json.RawMessage(`{"content":" ok ","isError":true,"extra":1}`))
	if result.Content != "ok" || !result.IsError {
		t.Fatalf("unexpected decode result: %+v", result)
	}
	if result.Metadata["extra"] != float64(1) {
		t.Fatalf("expected metadata extra")
	}

	result = decodeCallResult(json.RawMessage(`{"content":[{"text":"a"},"b"],"is_error":true}`))
	if result.Content != "a\nb" || !result.IsError {
		t.Fatalf("unexpected list content decode: %+v", result)
	}

	result = decodeCallResult(json.RawMessage(`{"content":{"nested":"x"}}`))
	if result.Content == "" {
		t.Fatalf("expected fallback string content")
	}

	result = decodeCallResult(json.RawMessage(`not-json`))
	if result.Content != "not-json" {
		t.Fatalf("expected raw fallback content, got %q", result.Content)
	}
	if _, ok := result.Metadata["raw_result"]; !ok {
		t.Fatalf("expected raw_result metadata")
	}
}

func TestFailAllPendingLocked(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{
		pending: map[string]chan rpcReply{
			"a": make(chan rpcReply, 1),
			"b": make(chan rpcReply, 1),
		},
	}
	client.failAllPendingLocked(errors.New("closed"))
	if len(client.pending) != 0 {
		t.Fatalf("expected pending cleared")
	}
}

func TestWriteFramedMessageError(t *testing.T) {
	t.Parallel()

	if err := writeFramedMessage(errWriter{}, []byte(`{}`)); err == nil {
		t.Fatalf("expected write error")
	}
}

func TestStdIOClientWaitLoopNilCommand(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{
		pending: make(map[string]chan rpcReply),
		started: true,
	}
	client.waitLoop(nil)
	if client.started {
		t.Fatalf("expected started=false after nil command waitLoop")
	}
}

func TestStdIOClientBumpBackoffClamp(t *testing.T) {
	t.Parallel()

	client := &StdIOClient{
		cfg: StdioClientConfig{
			RestartBackoff: time.Second,
		},
		backoff: maxStdioRestartBackoff,
	}
	client.bumpBackoffLocked()
	if client.backoff != maxStdioRestartBackoff {
		t.Fatalf("expected clamp to max backoff, got %v", client.backoff)
	}
	if client.retryAt.IsZero() {
		t.Fatalf("expected retryAt assigned")
	}
}

func newTestStdIOClient(t *testing.T) *StdIOClient {
	t.Helper()

	client, err := NewStdIOClient(StdioClientConfig{
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestHelperProcessMCPStdioServer", "--"},
		Env:          []string{"GO_WANT_MCP_STDIO_HELPER=1"},
		StartTimeout: 3 * time.Second,
		CallTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	return client
}

func TestHelperProcessMCPStdioServer(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_STDIO_HELPER") != "1" {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		payload, err := readFramedMessage(reader)
		if err != nil {
			if err == io.EOF {
				os.Exit(0)
			}
			os.Exit(2)
		}

		var request map[string]any
		if err := json.Unmarshal(payload, &request); err != nil {
			os.Exit(3)
		}

		method, _ := request["method"].(string)
		requestID, _ := request["id"].(string)

		var response any
		switch method {
		case "tools/list":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "search",
							"description": "search docs",
							"inputSchema": map[string]any{
								"type":       "object",
								"properties": map[string]any{"query": map[string]any{"type": "string"}},
							},
						},
					},
				},
			}
		case "tools/call":
			params, _ := request["params"].(map[string]any)
			name, _ := params["name"].(string)
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"content": fmt.Sprintf("ok:%s", name),
					"isError": false,
				},
			}
		default:
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			}
		}

		rawResponse, err := json.Marshal(response)
		if err != nil {
			os.Exit(4)
		}
		if err := writeFramedMessage(os.Stdout, rawResponse); err != nil {
			os.Exit(5)
		}
	}
}
