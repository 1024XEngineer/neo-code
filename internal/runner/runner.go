package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"neo-code/internal/tools"
)

// Runner 是本地执行守护进程，主动连接云端 Gateway，接收工具执行请求。
type Runner struct {
	cfg        Config
	logger     *log.Logger
	toolMgr    tools.Manager
	capSigner  *CapSigner

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// Config 表示 runner 运行时配置。
type Config struct {
	RunnerID          string
	RunnerName        string
	GatewayAddress    string
	Token             string
	Workdir           string
	WorkdirAllowlist  []string
	HeartbeatInterval time.Duration
	ReconnectBackoffMin time.Duration
	ReconnectBackoffMax time.Duration
	RequestTimeout    time.Duration
	Logger            *log.Logger
}

// New 创建 runner 实例。
func New(cfg Config) (*Runner, error) {
	if strings.TrimSpace(cfg.RunnerID) == "" {
		return nil, fmt.Errorf("runner: runner_id is required")
	}
	if strings.TrimSpace(cfg.GatewayAddress) == "" {
		return nil, fmt.Errorf("runner: gateway_address is required")
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}
	if cfg.ReconnectBackoffMin <= 0 {
		cfg.ReconnectBackoffMin = 500 * time.Millisecond
	}
	if cfg.ReconnectBackoffMax <= 0 {
		cfg.ReconnectBackoffMax = 10 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "runner: ", log.LstdFlags)
	}

	toolMgr := tools.NewRegistry()

	capSigner := NewCapSigner(cfg.WorkdirAllowlist)

	return &Runner{
		cfg:       cfg,
		logger:    logger,
		toolMgr:   toolMgr,
		capSigner: capSigner,
	}, nil
}

// Run 启动 runner 主循环。
func (r *Runner) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("runner: already running")
	}
	r.running = true
	ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	backoff := r.cfg.ReconnectBackoffMin
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := r.connectAndServe(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.logger.Printf("connection failed: %v, reconnecting in %v", err, backoff)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = time.Duration(math.Min(float64(backoff*2), float64(r.cfg.ReconnectBackoffMax)))
		// 添加 jitter
		jitter := time.Duration(rand.Int63n(int64(backoff / 4)))
		backoff += jitter
	}
}

func (r *Runner) connectAndServe(ctx context.Context) error {
	url := fmt.Sprintf("ws://%s/ws", r.cfg.GatewayAddress)
	if r.cfg.Token != "" {
		url += "?token=" + r.cfg.Token
	}

	header := http.Header{}
	header.Set("X-Runner-ID", r.cfg.RunnerID)

	dialer := websocket.Dialer{
		HandshakeTimeout: r.cfg.RequestTimeout,
	}
	conn, resp, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	r.logger.Printf("connected to gateway at %s", url)

	// 认证
	if err := r.sendRequest(conn, "gateway.authenticate", map[string]string{
		"token": r.cfg.Token,
	}); err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	// 注册 runner
	if err := r.sendRequest(conn, "gateway.registerRunner", map[string]any{
		"runner_id":   r.cfg.RunnerID,
		"runner_name": r.cfg.RunnerName,
		"workdir":     r.cfg.Workdir,
	}); err != nil {
		return fmt.Errorf("register runner: %w", err)
	}

	r.logger.Printf("runner registered: %s", r.cfg.RunnerID)

	// 启动心跳
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go r.heartbeatLoop(heartbeatCtx, conn)

	// 事件循环
	return r.eventLoop(ctx, conn)
}

func (r *Runner) eventLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, rawMessage, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var msg map[string]any
		if err := json.Unmarshal(rawMessage, &msg); err != nil {
			r.logger.Printf("failed to parse message: %v", err)
			continue
		}

		method, _ := msg["method"].(string)
		switch method {
		case "gateway.toolRequest":
			r.handleToolRequest(ctx, conn, msg)
		case "gateway.ping":
			r.handlePing(conn, msg)
		default:
			// 可能是对之前请求的响应，忽略
		}
	}
}

func (r *Runner) handleToolRequest(ctx context.Context, conn *websocket.Conn, msg map[string]any) {
	params, ok := msg["params"].(map[string]any)
	if !ok {
		r.logger.Printf("tool request missing params")
		return
	}

	req, err := parseToolRequest(params)
	if err != nil {
		r.logger.Printf("failed to parse tool request: %v", err)
		return
	}

	r.logger.Printf("executing tool: %s (request_id=%s)", req.ToolName, req.RequestID)

	// 执行工具
	execCtx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	defer cancel()

	result, toolErr := r.toolMgr.Execute(execCtx, tools.ToolCallInput{
		ID:        req.ToolCallID,
		Name:      req.ToolName,
		Arguments: req.Arguments,
		Workdir:   r.cfg.Workdir,
	})

	content := ""
	isError := false
	if toolErr != nil {
		content = toolErr.Error()
		isError = true
	} else {
		content = result.Content
	}

	// 发送结果回网关
	resultParams := map[string]any{
		"request_id":   req.RequestID,
		"session_id":   req.SessionID,
		"run_id":       req.RunID,
		"runner_id":    r.cfg.RunnerID,
		"tool_call_id": req.ToolCallID,
		"content":      content,
		"is_error":     isError,
	}

	if err := r.sendRequest(conn, "gateway.executeToolResult", resultParams); err != nil {
		r.logger.Printf("failed to send tool result: %v", err)
	}
}

func (r *Runner) handlePing(conn *websocket.Conn, msg map[string]any) {
	reqID, _ := msg["id"].(string)
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result":  "pong",
	}
	data, _ := json.Marshal(response)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		r.logger.Printf("failed to send pong: %v", err)
	}
}

func (r *Runner) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(r.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.sendRequest(conn, "gateway.ping", nil); err != nil {
				r.logger.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

func (r *Runner) sendRequest(conn *websocket.Conn, method string, params any) error {
	request := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      fmt.Sprintf("req_%d", time.Now().UnixNano()),
	}
	if params != nil {
		request["params"] = params
	}

	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(r.cfg.RequestTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func parseToolRequest(params map[string]any) (ToolExecutionRequest, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return ToolExecutionRequest{}, err
	}
	var req ToolExecutionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return ToolExecutionRequest{}, err
	}
	if req.RequestID == "" {
		return ToolExecutionRequest{}, fmt.Errorf("missing request_id")
	}
	if req.ToolName == "" {
		return ToolExecutionRequest{}, fmt.Errorf("missing tool_name")
	}
	return req, nil
}

// Stop 停止 runner。
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}
