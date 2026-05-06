package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func newRemoteRuntimeAdapterForTest(
	t *testing.T,
	rpcClient *stubRemoteRPCClient,
) (*RemoteRuntimeAdapter, *stubRemoteStreamClient) {
	t.Helper()

	if rpcClient.notifications == nil {
		rpcClient.notifications = make(chan gatewayRPCNotification)
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter, streamClient
}

func TestRemoteRuntimeAdapterSubmitAuthenticatesBindsPreloadsAndRuns(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayLoadSession: {
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionLoadSession,
				SessionID: "session-1",
			},
			protocol.MethodGatewayBindStream: {
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionBindStream,
				SessionID: "session-1",
				RunID:     "run-1",
			},
			protocol.MethodGatewayRun: {
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionRun,
				SessionID: "session-1",
				RunID:     "run-1",
			},
		},
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	err := adapter.Submit(context.Background(), PrepareInput{
		SessionID: "session-1",
		RunID:     "run-1",
		Workdir:   "/repo",
		Mode:      "plan",
		Text:      " hello ",
		Images: []UserImageInput{
			{Path: " /tmp/a.png ", MimeType: " image/png "},
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if rpcClient.authCalls != 1 {
		t.Fatalf("authenticate call count = %d, want %d", rpcClient.authCalls, 1)
	}

	methods := rpcClient.snapshotMethods()
	if len(methods) != 3 ||
		methods[0] != protocol.MethodGatewayBindStream ||
		methods[1] != protocol.MethodGatewayLoadSession ||
		methods[2] != protocol.MethodGatewayRun {
		t.Fatalf("rpc methods = %#v", methods)
	}
	loadSessionParams, ok := rpcClient.snapshotParams()[protocol.MethodGatewayLoadSession].(protocol.LoadSessionParams)
	if !ok {
		t.Fatalf(
			"loadSession params type = %T, want protocol.LoadSessionParams",
			rpcClient.snapshotParams()[protocol.MethodGatewayLoadSession],
		)
	}
	if loadSessionParams.SessionID != "session-1" {
		t.Fatalf("loadSession session_id = %q, want %q", loadSessionParams.SessionID, "session-1")
	}

	params, ok := rpcClient.snapshotParams()[protocol.MethodGatewayRun].(protocol.RunParams)
	if !ok {
		t.Fatalf("run params type = %T, want protocol.RunParams", rpcClient.snapshotParams()[protocol.MethodGatewayRun])
	}
	if params.SessionID != "session-1" || params.RunID != "run-1" || params.Workdir != "/repo" {
		t.Fatalf("unexpected run params ids/workdir: %#v", params)
	}
	if params.Mode != "plan" {
		t.Fatalf("run mode = %q, want %q", params.Mode, "plan")
	}
	if params.InputText != "hello" {
		t.Fatalf("run input_text = %q, want %q", params.InputText, "hello")
	}
	if len(params.InputParts) != 1 || params.InputParts[0].Media == nil {
		t.Fatalf("run input_parts = %#v, want one image part", params.InputParts)
	}
	if params.InputParts[0].Media.URI != "/tmp/a.png" || params.InputParts[0].Media.MimeType != "image/png" {
		t.Fatalf("unexpected image part media: %#v", params.InputParts[0].Media)
	}
}

func TestRemoteRuntimeAdapterSubmitFailFastOnAuthenticateError(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{
		authErr: errors.New("auth failed"),
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	err := adapter.Submit(context.Background(), PrepareInput{
		SessionID: "session-1",
		RunID:     "run-1",
		Text:      "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if methods := rpcClient.snapshotMethods(); len(methods) != 0 {
		t.Fatalf("expected no rpc call after auth failure, got %#v", methods)
	}
}

func TestRemoteRuntimeAdapterSubmitFailFastOnBindStreamError(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{
		callErrs: map[string]error{
			protocol.MethodGatewayBindStream: errors.New("stream bind failed"),
		},
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	err := adapter.Submit(context.Background(), PrepareInput{
		SessionID: "session-1",
		RunID:     "run-1",
		Text:      "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "stream bind failed") {
		t.Fatalf("expected bindStream failure, got %v", err)
	}

	methods := rpcClient.snapshotMethods()
	if len(methods) != 1 || methods[0] != protocol.MethodGatewayBindStream {
		t.Fatalf("expected only bindStream call before failure, got %#v", methods)
	}
}

func TestRemoteRuntimeAdapterExecuteSystemTool(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rpcClient := &stubRemoteRPCClient{
			frames: map[string]gateway.MessageFrame{
				protocol.MethodGatewayExecuteSystemTool: {
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionExecuteSystemTool,
					Payload: tools.ToolResult{
						Name:    "memo_list",
						Content: "memo ok",
					},
				},
			},
			notifications: make(chan gatewayRPCNotification),
		}
		streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
		adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
		t.Cleanup(func() { _ = adapter.Close() })

		result, err := adapter.ExecuteSystemTool(context.Background(), SystemToolInput{
			SessionID: "session-1",
			RunID:     "run-1",
			Workdir:   "/repo",
			ToolName:  "memo_list",
			Arguments: []byte("null"),
		})
		if err != nil {
			t.Fatalf("ExecuteSystemTool() error = %v", err)
		}
		if result.Content != "memo ok" || result.Name != "memo_list" {
			t.Fatalf("result = %#v, want memo_list/memo ok", result)
		}
		if rpcClient.authCalls != 1 {
			t.Fatalf("authenticate call count = %d, want %d", rpcClient.authCalls, 1)
		}
		params, ok := rpcClient.snapshotParams()[protocol.MethodGatewayExecuteSystemTool].(protocol.ExecuteSystemToolParams)
		if !ok {
			t.Fatalf(
				"executeSystemTool params type = %T, want protocol.ExecuteSystemToolParams",
				rpcClient.snapshotParams()[protocol.MethodGatewayExecuteSystemTool],
			)
		}
		if params.SessionID != "session-1" || params.RunID != "run-1" || params.Workdir != "/repo" || params.ToolName != "memo_list" {
			t.Fatalf("unexpected executeSystemTool params: %#v", params)
		}
		if string(params.Arguments) != "{}" {
			t.Fatalf("executeSystemTool arguments = %s, want {}", string(params.Arguments))
		}
	})

	t.Run("gateway unsupported action passthrough", func(t *testing.T) {
		rpcClient := &stubRemoteRPCClient{
			callErrs: map[string]error{
				protocol.MethodGatewayExecuteSystemTool: &GatewayRPCError{
					Method:      protocol.MethodGatewayExecuteSystemTool,
					Code:        protocol.JSONRPCCodeMethodNotFound,
					GatewayCode: protocol.GatewayCodeUnsupportedAction,
					Message:     "method not found",
				},
			},
			notifications: make(chan gatewayRPCNotification),
		}
		streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
		adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
		t.Cleanup(func() { _ = adapter.Close() })

		_, err := adapter.ExecuteSystemTool(context.Background(), SystemToolInput{
			ToolName: "memo_list",
		})
		var rpcErr *GatewayRPCError
		if !errors.As(err, &rpcErr) {
			t.Fatalf("expected GatewayRPCError passthrough, got %v", err)
		}
		if rpcErr.Code != protocol.JSONRPCCodeMethodNotFound || rpcErr.GatewayCode != protocol.GatewayCodeUnsupportedAction {
			t.Fatalf("unexpected rpc error payload: %#v", rpcErr)
		}
	})

	t.Run("gateway method not found passthrough", func(t *testing.T) {
		rpcClient := &stubRemoteRPCClient{
			callErrs: map[string]error{
				protocol.MethodGatewayExecuteSystemTool: &GatewayRPCError{
					Method:  protocol.MethodGatewayExecuteSystemTool,
					Code:    protocol.JSONRPCCodeMethodNotFound,
					Message: "method not found",
				},
			},
			notifications: make(chan gatewayRPCNotification),
		}
		streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
		adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
		t.Cleanup(func() { _ = adapter.Close() })

		_, err := adapter.ExecuteSystemTool(context.Background(), SystemToolInput{
			ToolName: "memo_list",
		})
		var rpcErr *GatewayRPCError
		if !errors.As(err, &rpcErr) {
			t.Fatalf("expected GatewayRPCError passthrough, got %v", err)
		}
		if rpcErr.Code != protocol.JSONRPCCodeMethodNotFound || rpcErr.GatewayCode != "" {
			t.Fatalf("unexpected rpc error payload: %#v", rpcErr)
		}
	})
}

func TestRemoteRuntimeAdapterLoadSessionMinimalMapping(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayLoadSession: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionLoadSession,
				Payload: gateway.Session{
					ID:      "session-9",
					Title:   "title-9",
					Workdir: "/repo",
					Messages: []gateway.SessionMessage{
						{
							Role:       providertypes.RoleAssistant,
							Content:    "hello",
							ToolCallID: "call-1",
							ToolCalls: []gateway.ToolCall{
								{ID: "call-1", Name: "bash", Arguments: `{"command":"pwd"}`},
							},
						},
					},
				},
			},
		},
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	session, err := adapter.LoadSession(context.Background(), "session-9")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if session.ID != "session-9" || session.Title != "title-9" || session.Workdir != "/repo" {
		t.Fatalf("unexpected session mapping: %#v", session)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("message count = %d, want %d", len(session.Messages), 1)
	}
	if text := renderPartsForRemoteAdapterTest(session.Messages[0].Parts); text != "hello" {
		t.Fatalf("message parts text = %q, want %q", text, "hello")
	}
	if len(session.Messages[0].ToolCalls) != 1 || session.Messages[0].ToolCalls[0].Name != "bash" {
		t.Fatalf("tool call mapping mismatch: %#v", session.Messages[0].ToolCalls)
	}
}

func TestRemoteRuntimeAdapterCancelActiveRunSendsGatewayCancel(t *testing.T) {
	methodCh := make(chan string, 1)
	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayCancel: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionCancel,
			},
		},
		methodCh: methodCh,
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if canceled := adapter.CancelActiveRun(); canceled {
		t.Fatalf("expected no active run to cancel")
	}

	adapter.setActiveRun("run-cancel", "session-cancel")
	if canceled := adapter.CancelActiveRun(); !canceled {
		t.Fatalf("expected cancel request to be scheduled")
	}

	select {
	case method := <-methodCh:
		if method != protocol.MethodGatewayCancel {
			t.Fatalf("cancel method = %q, want %q", method, protocol.MethodGatewayCancel)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for cancel rpc call")
	}
}

func TestRemoteRuntimeAdapterCloseClosesUnderlyingClients(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{notifications: make(chan gatewayRPCNotification)}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)

	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !rpcClient.closed {
		t.Fatalf("expected rpc client to be closed")
	}
	if !streamClient.closed {
		t.Fatalf("expected stream client to be closed")
	}
}

type stubRemoteRPCClient struct {
	mu sync.Mutex

	authCalls int
	authErr   error

	methods []string
	params  map[string]any
	options map[string]GatewayRPCCallOptions

	callErrs map[string]error
	frames   map[string]gateway.MessageFrame
	methodCh chan string

	notifications chan gatewayRPCNotification
	closed        bool
}

func (s *stubRemoteRPCClient) Authenticate(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authCalls++
	return s.authErr
}

func (s *stubRemoteRPCClient) CallWithOptions(
	_ context.Context,
	method string,
	params any,
	result any,
	options GatewayRPCCallOptions,
) error {
	s.mu.Lock()
	s.methods = append(s.methods, method)
	if s.params == nil {
		s.params = map[string]any{}
	}
	if s.options == nil {
		s.options = map[string]GatewayRPCCallOptions{}
	}
	s.params[method] = params
	s.options[method] = options
	callErr := s.callErrs[method]
	frame, hasFrame := s.frames[method]
	s.mu.Unlock()

	if s.methodCh != nil {
		select {
		case s.methodCh <- method:
		default:
		}
	}
	if callErr != nil {
		return callErr
	}
	if typed, ok := result.(*gateway.MessageFrame); ok {
		if !hasFrame {
			frame = gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameAction(method),
			}
		}
		*typed = frame
	}
	return nil
}

func (s *stubRemoteRPCClient) Notifications() <-chan gatewayRPCNotification {
	return s.notifications
}

func (s *stubRemoteRPCClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		if s.notifications != nil {
			close(s.notifications)
		}
	}
	return nil
}

func (s *stubRemoteRPCClient) snapshotMethods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.methods...)
}

func (s *stubRemoteRPCClient) snapshotParams() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make(map[string]any, len(s.params))
	for key, value := range s.params {
		cloned[key] = value
	}
	return cloned
}

func (s *stubRemoteRPCClient) snapshotOptions() map[string]GatewayRPCCallOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make(map[string]GatewayRPCCallOptions, len(s.options))
	for key, value := range s.options {
		cloned[key] = value
	}
	return cloned
}

type stubRemoteStreamClient struct {
	events <-chan RuntimeEvent
	closed bool
	mu     sync.Mutex
}

func (s *stubRemoteStreamClient) Events() <-chan RuntimeEvent {
	return s.events
}

func (s *stubRemoteStreamClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func renderPartsForRemoteAdapterTest(parts []providertypes.ContentPart) string {
	builder := strings.Builder{}
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartText {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

var _ remoteGatewayRPCClient = (*stubRemoteRPCClient)(nil)
var _ remoteGatewayStreamClient = (*stubRemoteStreamClient)(nil)
var _ Runtime = (*RemoteRuntimeAdapter)(nil)
var _ = tools.ToolResult{}
var _ = agentsession.Summary{}
