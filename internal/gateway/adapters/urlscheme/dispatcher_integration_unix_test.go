//go:build !windows

package urlscheme

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
	"neo-code/internal/tools"
)

func TestDispatchEndToEndWithGatewayServer(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "run", "gateway.sock")
	server, err := gateway.NewServer(gateway.ServerOptions{
		ListenAddress: socketPath,
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("new gateway server: %v", err)
	}

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()
	runtimeStub := &urlschemeIntegrationRuntimeStub{}

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(serverCtx, runtimeStub)
	}()

	if err := waitGatewayReady(socketPath, 2*time.Second); err != nil {
		t.Fatalf("wait gateway ready: %v", err)
	}

	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }
	dispatcher.launchTerminalFn = func(string) error { return nil }

	successResult, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action:  protocol.WakeActionReview,
			Params:  map[string]string{"path": "README.md"},
			Workdir: "/workspace/repo",
			RawURL:  "http://neocode:18921/review?path=README.md&workdir=%2Fworkspace%2Frepo",
		},
		ListenAddress: socketPath,
	})
	if err != nil {
		t.Fatalf("dispatch review url: %v", err)
	}
	if successResult.Response.Type != gateway.FrameTypeAck {
		t.Fatalf("response type = %q, want %q", successResult.Response.Type, gateway.FrameTypeAck)
	}
	if successResult.Response.Action != gateway.FrameActionWakeOpenURL {
		t.Fatalf("response action = %q, want %q", successResult.Response.Action, gateway.FrameActionWakeOpenURL)
	}
	if successResult.Response.SessionID != "session-review-integration" {
		t.Fatalf("response session_id = %q, want %q", successResult.Response.SessionID, "session-review-integration")
	}

	_, err = dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: "open",
			Params: map[string]string{"path": "README.md"},
			RawURL: "http://neocode:18921/open?path=README.md",
		},
		ListenAddress: socketPath,
	})
	if err == nil {
		t.Fatal("expected invalid action error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != gateway.ErrorCodeInvalidAction.String() {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInvalidAction.String())
	}

	cancelServer()
	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("serve returned error: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gateway server did not stop in time")
	}
}

type urlschemeIntegrationRuntimeStub struct{}

func (s *urlschemeIntegrationRuntimeStub) Run(context.Context, gateway.RunInput) error {
	return nil
}

func (s *urlschemeIntegrationRuntimeStub) Compact(context.Context, gateway.CompactInput) (gateway.CompactResult, error) {
	return gateway.CompactResult{}, nil
}

func (s *urlschemeIntegrationRuntimeStub) ExecuteSystemTool(
	context.Context,
	gateway.ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (s *urlschemeIntegrationRuntimeStub) ActivateSessionSkill(
	context.Context,
	gateway.SessionSkillMutationInput,
) error {
	return nil
}

func (s *urlschemeIntegrationRuntimeStub) DeactivateSessionSkill(
	context.Context,
	gateway.SessionSkillMutationInput,
) error {
	return nil
}

func (s *urlschemeIntegrationRuntimeStub) ListSessionSkills(
	context.Context,
	gateway.ListSessionSkillsInput,
) ([]gateway.SessionSkillState, error) {
	return nil, nil
}

func (s *urlschemeIntegrationRuntimeStub) ListAvailableSkills(
	context.Context,
	gateway.ListAvailableSkillsInput,
) ([]gateway.AvailableSkillState, error) {
	return nil, nil
}

func (s *urlschemeIntegrationRuntimeStub) ResolvePermission(
	context.Context,
	gateway.PermissionResolutionInput,
) error {
	return nil
}

func (s *urlschemeIntegrationRuntimeStub) CancelRun(context.Context, gateway.CancelInput) (bool, error) {
	return false, nil
}

func (s *urlschemeIntegrationRuntimeStub) Events() <-chan gateway.RuntimeEvent {
	return nil
}

func (s *urlschemeIntegrationRuntimeStub) ListSessions(context.Context) ([]gateway.SessionSummary, error) {
	return nil, nil
}

func (s *urlschemeIntegrationRuntimeStub) LoadSession(context.Context, gateway.LoadSessionInput) (gateway.Session, error) {
	return gateway.Session{}, nil
}

func (s *urlschemeIntegrationRuntimeStub) CreateSession(
	context.Context,
	gateway.CreateSessionInput,
) (string, error) {
	return strings.TrimSpace("session-review-integration"), nil
}

func waitGatewayReady(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := transport.Dial(address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("gateway did not become ready before timeout")
}
