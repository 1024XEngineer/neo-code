package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type nopWriteCloser struct {
	bytes.Buffer
}

func (n *nopWriteCloser) Close() error { return nil }

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestStdIOClientListToolsAndCallTool(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClientWithMode(t, "framed")
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

func TestStdIOClientLineProtocolInitializeAndCalls(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClientWithMode(t, "line")
	defer func() { _ = client.Close() }()

	toolsList, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() with line protocol error = %v", err)
	}
	if len(toolsList) != 1 || toolsList[0].Name != "search" {
		t.Fatalf("unexpected tools list: %+v", toolsList)
	}

	result, err := client.CallTool(context.Background(), "search", []byte(`{"query":"mcp"}`))
	if err != nil {
		t.Fatalf("CallTool() with line protocol error = %v", err)
	}
	if !strings.Contains(result.Content, "search") {
		t.Fatalf("unexpected call result content: %q", result.Content)
	}
}

func TestStdIOClientInitializeFallbackToFramed(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClientWithMode(t, "framed")
	defer func() { _ = client.Close() }()

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("expected fallback initialize success, got %v", err)
	}
	if client.protocol != stdioProtocolFramed {
		t.Fatalf("expected framed protocol selected, got %q", client.protocol)
	}
}

func TestStdIOClientHealthCheck(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClientWithMode(t, "framed")
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestStdIOClientSendNotificationFramedDefault(t *testing.T) {
	t.Parallel()

	writer := &nopWriteCloser{}
	client := &StdIOClient{
		pending: make(map[string]chan rpcReply),
		stdin:   writer,
		started: true,
		cfg: StdioClientConfig{
			CallTimeout:    time.Second,
			StartTimeout:   time.Second,
			RestartBackoff: time.Millisecond,
		},
	}

	err := client.sendNotification(context.Background(), "notifications/initialized", map[string]any{}, true)
	if err != nil {
		t.Fatalf("sendNotification() error = %v", err)
	}
	if !strings.Contains(writer.String(), "Content-Length:") {
		t.Fatalf("expected framed header, got: %q", writer.String())
	}
}

func TestStdIOClientSendNotificationLineProtocol(t *testing.T) {
	t.Parallel()

	writer := &nopWriteCloser{}
	client := &StdIOClient{
		pending:  make(map[string]chan rpcReply),
		stdin:    writer,
		started:  true,
		protocol: stdioProtocolLine,
		cfg: StdioClientConfig{
			CallTimeout:    time.Second,
			StartTimeout:   time.Second,
			RestartBackoff: time.Millisecond,
		},
	}

	err := client.sendNotificationWithProtocol(
		context.Background(),
		"notifications/initialized",
		map[string]any{},
		true,
		stdioProtocolLine,
	)
	if err != nil {
		t.Fatalf("sendNotificationWithProtocol() error = %v", err)
	}
	raw := writer.String()
	if strings.Contains(raw, "Content-Length:") {
		t.Fatalf("expected line protocol write, got: %q", raw)
	}
	if !strings.HasSuffix(raw, "\n") {
		t.Fatalf("expected newline-delimited payload, got: %q", raw)
	}
}

func TestStdIOClientConcurrentCallTool(t *testing.T) {
	t.Parallel()

	client := newTestStdIOClientWithMode(t, "framed")
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

func TestReadRPCMessageLine(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("Starting...\n{\"jsonrpc\":\"2.0\",\"id\":\"1\",\"result\":{}}\n"))
	payload, protocol, err := readRPCMessage(reader)
	if err != nil {
		t.Fatalf("readRPCMessage() error = %v", err)
	}
	if protocol != stdioProtocolLine {
		t.Fatalf("expected line protocol, got %q", protocol)
	}
	if !strings.Contains(string(payload), `"jsonrpc":"2.0"`) {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func TestReadRPCMessageFramed(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":"1","result":{"ok":true}}`
	raw := "log\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	reader := bufio.NewReader(strings.NewReader(raw))
	payload, protocol, err := readRPCMessage(reader)
	if err != nil {
		t.Fatalf("readRPCMessage() error = %v", err)
	}
	if protocol != stdioProtocolFramed {
		t.Fatalf("expected framed protocol, got %q", protocol)
	}
	if string(payload) != body {
		t.Fatalf("unexpected payload: %s", payload)
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
	if _, err := readFramedMessage(reader); err == nil || !(strings.Contains(err.Error(), "missing content-length") || errors.Is(err, io.EOF)) {
		t.Fatalf("expected missing content-length error, got %v", err)
	}

	reader = bufio.NewReader(strings.NewReader("Content-Length: nope\r\n\r\n{}"))
	if _, err := readFramedMessage(reader); err == nil || !strings.Contains(err.Error(), "invalid content-length") {
		t.Fatalf("expected invalid content-length error, got %v", err)
	}
}

func TestReadFramedMessageIgnoresStdoutPreamble(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":"1","result":{"ok":true}}`
	raw := "Starting Time MCP server...\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	reader := bufio.NewReader(strings.NewReader(raw))
	payload, err := readFramedMessage(reader)
	if err != nil {
		t.Fatalf("readFramedMessage() error = %v", err)
	}
	if string(payload) != body {
		t.Fatalf("unexpected payload: %s", string(payload))
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

	result = decodeCallResult(json.RawMessage(`{"content":[{"type":"resource_link","uri":"https://example.com"},{"type":"image","mimeType":"image/png"}]}`))
	if !strings.Contains(result.Content, `"type":"resource_link"`) || !strings.Contains(result.Content, `"type":"image"`) {
		t.Fatalf("expected non-text items to be preserved, got %q", result.Content)
	}

	result = decodeCallResult(json.RawMessage(`{"content":{"nested":"x"}}`))
	if !strings.Contains(result.Content, `"nested":"x"`) {
		t.Fatalf("expected structured map content, got %q", result.Content)
	}

	result = decodeCallResult(json.RawMessage(`{"content":[],"isError":true}`))
	if !result.IsError || strings.Contains(strings.ToLower(result.Content), "ok") {
		t.Fatalf("expected non-ok error fallback content, got %+v", result)
	}

	result = decodeCallResult(json.RawMessage(`not-json`))
	if result.Content != "not-json" {
		t.Fatalf("expected raw fallback content, got %q", result.Content)
	}
	if _, ok := result.Metadata["raw_result"]; !ok {
		t.Fatalf("expected raw_result metadata")
	}
}

func TestDecodeCallContentItemVariants(t *testing.T) {
	t.Parallel()

	if got := decodeCallContentItem(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}
	if got := decodeCallContentItem(" text "); got != "text" {
		t.Fatalf("expected trimmed text, got %q", got)
	}
	if got := decodeCallContentItem(map[string]any{"text": " hello "}); got != "hello" {
		t.Fatalf("expected text extraction, got %q", got)
	}
	if got := decodeCallContentItem(map[string]any{"type": "resource_link", "uri": "https://example.com"}); !strings.Contains(got, `"type":"resource_link"`) {
		t.Fatalf("expected json fallback for object item, got %q", got)
	}
	if got := decodeCallContentItem(123); got != "123" {
		t.Fatalf("expected scalar json fallback, got %q", got)
	}
	if got := decodeCallContentItem(map[string]any{"bad": func() {}}); !strings.Contains(got, "bad") {
		t.Fatalf("expected fmt fallback for non-marshalable map, got %q", got)
	}
	if got := decodeCallContentItem(func() {}); !strings.Contains(got, "0x") {
		t.Fatalf("expected fmt fallback for non-marshalable scalar, got %q", got)
	}
}

func TestDecodeCallResultContentFallbackOK(t *testing.T) {
	t.Parallel()

	result := decodeCallResult(json.RawMessage(`{"content":[]}`))
	if result.IsError {
		t.Fatalf("expected non-error result")
	}
	if result.Content != "ok" {
		t.Fatalf("expected ok fallback content, got %q", result.Content)
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
	return newTestStdIOClientWithMode(t, "framed")
}

func newTestStdIOClientWithMode(t *testing.T, wireMode string) *StdIOClient {
	t.Helper()

	if strings.TrimSpace(wireMode) == "" {
		wireMode = "framed"
	}

	client, err := NewStdIOClient(StdioClientConfig{
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestHelperProcessMCPStdioServer", "--"},
		Env:          []string{"GO_WANT_MCP_STDIO_HELPER=1", "GO_MCP_STDIO_REQUIRE_INITIALIZE=1", "GO_MCP_STDIO_WIRE=" + wireMode},
		StartTimeout: 3 * time.Second,
		CallTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	return client
}

func TestStdIOClientInitializeFailure(t *testing.T) {
	t.Parallel()

	client, err := NewStdIOClient(StdioClientConfig{
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestHelperProcessMCPStdioServer", "--"},
		Env:          []string{"GO_WANT_MCP_STDIO_HELPER=1", "GO_MCP_STDIO_INIT_FAIL=1", "GO_MCP_STDIO_WIRE=framed"},
		StartTimeout: 3 * time.Second,
		CallTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	_, callErr := client.ListTools(context.Background())
	if callErr == nil || !strings.Contains(callErr.Error(), "initialize session") {
		t.Fatalf("expected initialize error, got %v", callErr)
	}
}

func TestStdIOClientListToolsTimeout(t *testing.T) {
	t.Parallel()

	client, err := NewStdIOClient(StdioClientConfig{
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestHelperProcessMCPStdioServer", "--"},
		Env:          []string{"GO_WANT_MCP_STDIO_HELPER=1", "GO_MCP_STDIO_REQUIRE_INITIALIZE=1", "GO_MCP_STDIO_WIRE=framed", "GO_MCP_STDIO_LIST_DELAY_MS=300"},
		StartTimeout: 3 * time.Second,
		CallTimeout:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	_, callErr := client.ListTools(context.Background())
	if callErr == nil || !errors.Is(callErr, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", callErr)
	}
}

func TestStdIOClientListToolsProtocolError(t *testing.T) {
	t.Parallel()

	client, err := NewStdIOClient(StdioClientConfig{
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestHelperProcessMCPStdioServer", "--"},
		Env:          []string{"GO_WANT_MCP_STDIO_HELPER=1", "GO_MCP_STDIO_REQUIRE_INITIALIZE=1", "GO_MCP_STDIO_WIRE=framed", "GO_MCP_STDIO_LIST_MALFORMED=1"},
		StartTimeout: 3 * time.Second,
		CallTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewStdIOClient() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	_, callErr := client.ListTools(context.Background())
	if callErr == nil || !strings.Contains(callErr.Error(), "decode tools/list result") {
		t.Fatalf("expected decode tools/list result error, got %v", callErr)
	}
}

func TestHelperProcessMCPStdioServer(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_STDIO_HELPER") != "1" {
		return
	}

	requireInitialize := os.Getenv("GO_MCP_STDIO_REQUIRE_INITIALIZE") == "1"
	initFail := os.Getenv("GO_MCP_STDIO_INIT_FAIL") == "1"
	listDelayMS, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("GO_MCP_STDIO_LIST_DELAY_MS")))
	listMalformed := os.Getenv("GO_MCP_STDIO_LIST_MALFORMED") == "1"
	wireMode := strings.TrimSpace(os.Getenv("GO_MCP_STDIO_WIRE"))
	if wireMode == "" {
		wireMode = "framed"
	}
	initialized := !requireInitialize

	reader := bufio.NewReader(os.Stdin)
	for {
		var (
			payload []byte
			err     error
		)
		switch wireMode {
		case "line":
			payload, _, err = readRPCMessage(reader)
		default:
			payload, err = readFramedMessage(reader)
		}
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
		case "initialize":
			if initFail {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"error": map[string]any{
						"code":    -32600,
						"message": "initialize rejected",
					},
				}
				break
			}
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo": map[string]any{
						"name":    "test-helper",
						"version": "1.0.0",
					},
				},
			}
		case "notifications/initialized":
			initialized = true
			continue
		case "tools/list":
			if !initialized {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"error": map[string]any{
						"code":    -32002,
						"message": "server not initialized",
					},
				}
				break
			}
			if listDelayMS > 0 {
				time.Sleep(time.Duration(listDelayMS) * time.Millisecond)
			}
			if listMalformed {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"result":  "broken",
				}
				break
			}
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
			if !initialized {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"error": map[string]any{
						"code":    -32002,
						"message": "server not initialized",
					},
				}
				break
			}
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
		switch wireMode {
		case "line":
			err = writeLineMessage(os.Stdout, rawResponse)
		default:
			err = writeFramedMessage(os.Stdout, rawResponse)
		}
		if err != nil {
			os.Exit(5)
		}
	}
}
