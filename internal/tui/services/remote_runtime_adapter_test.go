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
	agentruntime "neo-code/internal/runtime"
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
	streamClient := &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}
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
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

	err := adapter.Submit(context.Background(), agentruntime.PrepareInput{
		SessionID: "session-1",
		RunID:     "run-1",
		Workdir:   "/repo",
		Text:      " hello ",
		Images: []agentruntime.UserImageInput{
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
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

	err := adapter.Submit(context.Background(), agentruntime.PrepareInput{
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
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

	err := adapter.Submit(context.Background(), agentruntime.PrepareInput{
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

func TestRemoteRuntimeAdapterExecuteSystemToolUnsupported(t *testing.T) {
	rpcClient := &stubRemoteRPCClient{}
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

	_, err := adapter.ExecuteSystemTool(context.Background(), agentruntime.SystemToolInput{
		ToolName: "bash",
	})
	if err == nil || !errors.Is(err, ErrUnsupportedActionInGatewayMode) {
		t.Fatalf("expected unsupported_action_in_gateway_mode, got %v", err)
	}
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
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

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
	adapter, _ := newRemoteRuntimeAdapterForTest(t, rpcClient)

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
	rpcClient := &stubRemoteRPCClient{}
	adapter, streamClient := newRemoteRuntimeAdapterForTest(t, rpcClient)

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
	events <-chan agentruntime.RuntimeEvent
	closed bool
	mu     sync.Mutex
}

func (s *stubRemoteStreamClient) Events() <-chan agentruntime.RuntimeEvent {
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
var _ agentruntime.Runtime = (*RemoteRuntimeAdapter)(nil)
var _ = tools.ToolResult{}
var _ = agentsession.Summary{}
