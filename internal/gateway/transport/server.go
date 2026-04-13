package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
)

// Mode 表示网关 IPC 传输类型。
type Mode string

const (
	// ModeAuto 表示自动按平台选择传输实现。
	ModeAuto Mode = "auto"
	// ModeNPipe 表示使用 Windows Named Pipe。
	ModeNPipe Mode = "npipe"
	// ModeUDS 表示使用 Unix Domain Socket。
	ModeUDS Mode = "uds"
)

var errUnsupportedTransport = errors.New("unsupported transport on current platform")

// Handler 负责处理单条 JSON 消息并返回 JSON 响应。
type Handler func(ctx context.Context, payload []byte) []byte

// Config 描述网关传输层的监听参数。
type Config struct {
	Mode     Mode
	Endpoint string
}

// Server 封装网关 IPC 服务监听与连接处理逻辑。
type Server struct {
	listener net.Listener
	endpoint string
	handler  Handler
	once     sync.Once
}

// NewServer 根据配置创建并绑定 IPC 服务端。
func NewServer(cfg Config, handler Handler) (*Server, error) {
	if handler == nil {
		return nil, errors.New("transport handler is nil")
	}

	mode, err := normalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}

	listener, endpoint, err := listen(mode, cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	return &Server{
		listener: listener,
		endpoint: endpoint,
		handler:  handler,
	}, nil
}

// Endpoint 返回当前监听的 IPC 地址。
func (s *Server) Endpoint() string {
	if s == nil {
		return ""
	}
	return s.endpoint
}

// Serve 启动接受循环并在上下文取消时优雅退出。
func (s *Server) Serve(ctx context.Context) error {
	if s == nil || s.listener == nil {
		return errors.New("transport server is not initialized")
	}

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

// Close 关闭监听器并释放底层资源。
func (s *Server) Close() error {
	if s == nil || s.listener == nil {
		return nil
	}
	var closeErr error
	s.once.Do(func() {
		closeErr = s.listener.Close()
	})
	return closeErr
}

// handleConn 按消息粒度解码请求并写回响应。
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	for {
		var payload json.RawMessage
		if err := decoder.Decode(&payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32700,
					"message": "parse error",
				},
			})
			return
		}

		response := s.handler(ctx, payload)
		if len(response) == 0 {
			continue
		}
		if err := encoder.Encode(json.RawMessage(response)); err != nil {
			return
		}
	}
}

// normalizeMode 归一化传输模式并校验输入合法性。
func normalizeMode(mode Mode) (Mode, error) {
	value := strings.TrimSpace(strings.ToLower(string(mode)))
	if value == "" {
		value = string(ModeAuto)
	}

	switch Mode(value) {
	case ModeAuto, ModeNPipe, ModeUDS:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("invalid gateway transport mode: %s", mode)
	}
}

// listen 根据传输模式创建实际监听器。
func listen(mode Mode, endpoint string) (net.Listener, string, error) {
	switch mode {
	case ModeAuto:
		if runtime.GOOS == "windows" {
			return listenNPipe(endpoint)
		}
		return listenUDS(endpoint)
	case ModeNPipe:
		return listenNPipe(endpoint)
	case ModeUDS:
		return listenUDS(endpoint)
	default:
		return nil, "", fmt.Errorf("invalid gateway transport mode: %s", mode)
	}
}
