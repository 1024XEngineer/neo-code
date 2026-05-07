package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/tools"
)

type runnerManagerAdapter struct {
	executeFn func(context.Context, tools.ToolCallInput) (tools.ToolResult, error)
}

func (m *runnerManagerAdapter) ListAvailableSpecs(context.Context, tools.SpecListInput) ([]providertypes.ToolSpec, error) {
	return nil, nil
}

func (m *runnerManagerAdapter) MicroCompactPolicy(string) tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (m *runnerManagerAdapter) MicroCompactSummarizer(string) tools.ContentSummarizer {
	return nil
}

func (m *runnerManagerAdapter) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, input)
	}
	return tools.ToolResult{}, nil
}

func (m *runnerManagerAdapter) RememberSessionDecision(string, security.Action, tools.SessionPermissionScope) error {
	return nil
}

func TestNewRunnerDefaultsAndValidation(t *testing.T) {
	t.Run("missing required fields", func(t *testing.T) {
		if _, err := New(Config{}); err == nil || !strings.Contains(err.Error(), "runner_id is required") {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := New(Config{RunnerID: "runner-1"}); err == nil || !strings.Contains(err.Error(), "gateway_address is required") {
			t.Fatalf("New() error = %v", err)
		}
	})

	t.Run("fills defaults", func(t *testing.T) {
		workdir := t.TempDir()
		prevWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd() error = %v", err)
		}
		if err := os.Chdir(workdir); err != nil {
			t.Fatalf("Chdir() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(prevWD) })

		r, err := New(Config{
			RunnerID:       "runner-1",
			GatewayAddress: "127.0.0.1:8080",
			Logger:         log.New(io.Discard, "", 0),
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if r.cfg.Workdir != workdir {
			t.Fatalf("workdir = %q, want %q", r.cfg.Workdir, workdir)
		}
		if r.cfg.HeartbeatInterval != 10*time.Second {
			t.Fatalf("HeartbeatInterval = %s, want 10s", r.cfg.HeartbeatInterval)
		}
		if r.cfg.ReconnectBackoffMin != 500*time.Millisecond {
			t.Fatalf("ReconnectBackoffMin = %s, want 500ms", r.cfg.ReconnectBackoffMin)
		}
		if r.cfg.ReconnectBackoffMax != 10*time.Second {
			t.Fatalf("ReconnectBackoffMax = %s, want 10s", r.cfg.ReconnectBackoffMax)
		}
		if r.cfg.RequestTimeout != 30*time.Second {
			t.Fatalf("RequestTimeout = %s, want 30s", r.cfg.RequestTimeout)
		}
		if r.capSigner == nil {
			t.Fatal("capSigner = nil, want initialized")
		}
	})
}

func TestRunnerParseToolRequest(t *testing.T) {
	req, err := parseToolRequest(map[string]any{
		"request_id": "req-1",
		"tool_name":  "bash",
	})
	if err != nil {
		t.Fatalf("parseToolRequest() error = %v", err)
	}
	if req.RequestID != "req-1" || req.ToolName != "bash" {
		t.Fatalf("parseToolRequest() = %#v", req)
	}

	if _, err := parseToolRequest(map[string]any{"tool_name": "bash"}); err == nil || !strings.Contains(err.Error(), "missing request_id") {
		t.Fatalf("parseToolRequest() error = %v", err)
	}
	if _, err := parseToolRequest(map[string]any{"request_id": "req-2"}); err == nil || !strings.Contains(err.Error(), "missing tool_name") {
		t.Fatalf("parseToolRequest() error = %v", err)
	}
}

func TestRunnerHandleToolRequestInvalidParamsAndSendRequest(t *testing.T) {
	r := &Runner{
		cfg: Config{
			RunnerID:       "runner-1",
			Workdir:        "/safe/work",
			RequestTimeout: 200 * time.Millisecond,
		},
		logger:    log.New(io.Discard, "", 0),
		toolMgr:   &runnerManagerAdapter{},
		capSigner: NewCapSigner([]string{"/safe/work"}),
	}

	runnerConn, serverConn := newRunnerSocketPair(t)
	defer runnerConn.Close()
	defer serverConn.Close()

	r.handleToolRequest(context.Background(), runnerConn, map[string]any{})
	_ = serverConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, err := serverConn.ReadMessage()
	if err == nil {
		t.Fatal("expected invalid params path to not emit a response")
	}
	runnerConn.Close()
	serverConn.Close()

	runnerConn, serverConn = newRunnerSocketPair(t)
	defer runnerConn.Close()
	defer serverConn.Close()

	if err := r.sendRequest(runnerConn, "gateway.executeToolResult", map[string]any{"request_id": "req-1"}); err != nil {
		t.Fatalf("sendRequest() error = %v", err)
	}

	var response map[string]any
	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	if err := serverConn.ReadJSON(&response); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if response["method"] != "gateway.executeToolResult" {
		t.Fatalf("method = %v, want gateway.executeToolResult", response["method"])
	}

	if err := serverConn.WriteJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      "ping-1",
		"method":  "gateway.ping",
	}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	r.handlePing(runnerConn, map[string]any{"id": "ping-1"})

	var pong map[string]any
	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	if err := serverConn.ReadJSON(&pong); err != nil {
		t.Fatalf("ReadJSON(pong) error = %v", err)
	}
	if pong["result"] != "pong" {
		t.Fatalf("pong result = %v, want pong", pong["result"])
	}
}

func TestRunnerRunHandlesPingAndToolRequest(t *testing.T) {
	var executeCount atomic.Int32
	resultReceived := make(chan map[string]any, 1)
	server := newRunnerGatewayServer(t, func(conn *websocket.Conn) {
		var authenticate map[string]any
		if err := conn.ReadJSON(&authenticate); err != nil {
			t.Fatalf("read authenticate: %v", err)
		}
		if authenticate["method"] != "gateway.authenticate" {
			t.Fatalf("authenticate method = %v", authenticate["method"])
		}

		var register map[string]any
		if err := conn.ReadJSON(&register); err != nil {
			t.Fatalf("read register: %v", err)
		}
		if register["method"] != "gateway.registerRunner" {
			t.Fatalf("register method = %v", register["method"])
		}

		if err := conn.WriteJSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      "ping-1",
			"method":  "gateway.ping",
		}); err != nil {
			t.Fatalf("WriteJSON(ping) error = %v", err)
		}

		var pong map[string]any
		if err := conn.ReadJSON(&pong); err != nil {
			t.Fatalf("read pong: %v", err)
		}
		if pong["result"] != "pong" {
			t.Fatalf("pong result = %v, want pong", pong["result"])
		}

		if err := conn.WriteJSON(map[string]any{
			"jsonrpc": "2.0",
			"method":  "gateway.toolRequest",
			"params": map[string]any{
				"request_id":   "req-1",
				"session_id":   "session-1",
				"run_id":       "run-1",
				"tool_call_id": "tool-1",
				"tool_name":    "bash",
				"arguments":    json.RawMessage(`{"command":"echo hi"}`),
			},
		}); err != nil {
			t.Fatalf("WriteJSON(toolRequest) error = %v", err)
		}

		var result map[string]any
		if err := conn.ReadJSON(&result); err != nil {
			t.Fatalf("read tool result: %v", err)
		}
		resultReceived <- result
	})
	defer server.Close()

	r := &Runner{
		cfg: Config{
			RunnerID:            "runner-1",
			RunnerName:          "Local Runner",
			GatewayAddress:      runnerGatewayAddress(server.URL),
			Workdir:             t.TempDir(),
			RequestTimeout:      time.Second,
			HeartbeatInterval:   5 * time.Second,
			ReconnectBackoffMin: time.Millisecond,
			ReconnectBackoffMax: 2 * time.Millisecond,
		},
		logger: log.New(io.Discard, "", 0),
		toolMgr: &runnerManagerAdapter{
			executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
				executeCount.Add(1)
				if input.Name != "bash" {
					t.Fatalf("input.Name = %q, want bash", input.Name)
				}
				return tools.ToolResult{Content: "ok"}, nil
			},
		},
		capSigner: NewCapSigner(nil),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- r.Run(ctx)
	}()

	var result map[string]any
	select {
	case result = <-resultReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool result")
	}
	cancel()
	server.CloseClientConnections()

	if err := <-runErrCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if executeCount.Load() != 1 {
		t.Fatalf("execute count = %d, want 1", executeCount.Load())
	}
	params := result["params"].(map[string]any)
	if params["content"] != "ok" {
		t.Fatalf("result content = %v, want ok", params["content"])
	}
}

func TestRunnerRunAlreadyRunningAndStop(t *testing.T) {
	canceled := false
	r := &Runner{
		running: true,
		cancel:  func() { canceled = true },
	}
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("Run() error = %v", err)
	}
	r.Stop()
	if !canceled {
		t.Fatal("Stop() did not call cancel")
	}
}

func newRunnerSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		serverConnCh <- conn
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	clientConn, _, err := websocket.DefaultDialer.Dial("ws://"+serverURL.Host, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	serverConn := <-serverConnCh
	return clientConn, serverConn
}

func newRunnerGatewayServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ws" {
			http.NotFound(writer, request)
			return
		}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		defer conn.Close()
		handler(conn)
	}))
}

func runnerGatewayAddress(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.TrimPrefix(rawURL, "http://")
	}
	return parsed.Host
}
