package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
)

func TestNewRemoteRuntimeAdapterBranches(t *testing.T) {
	t.Parallel()

	originalRPCFactory := newGatewayRPCClientFactory
	originalStreamFactory := newGatewayStreamClientFactory
	t.Cleanup(func() {
		newGatewayRPCClientFactory = originalRPCFactory
		newGatewayStreamClientFactory = originalStreamFactory
	})

	newGatewayRPCClientFactory = func(options GatewayRPCClientOptions) (*GatewayRPCClient, error) {
		if strings.TrimSpace(options.ListenAddress) == "error" {
			return nil, errors.New("build rpc failed")
		}
		client := &GatewayRPCClient{
			listenAddress:     options.ListenAddress,
			token:             "token",
			requestTimeout:    time.Second,
			retryCount:        1,
			closed:            make(chan struct{}),
			pending:           make(map[string]chan gatewayRPCResponse),
			notifications:     make(chan gatewayRPCNotification, 4),
			notificationQueue: make(chan gatewayRPCNotification, 4),
		}
		client.dialFn = func(_ string) (net.Conn, error) {
			if options.ListenAddress == "dial-failed" {
				return nil, errors.New("dial failed")
			}
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				request := readRPCRequestOrFail(t, decoder)
				writeRPCResultOrFail(t, encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionAuthenticate,
				})
			}()
			return clientConn, nil
		}
		return client, nil
	}
	newGatewayStreamClientFactory = func(source <-chan gatewayRPCNotification) *GatewayStreamClient {
		return NewGatewayStreamClient(source)
	}

	if _, err := NewRemoteRuntimeAdapter(RemoteRuntimeAdapterOptions{ListenAddress: "error"}); err == nil {
		t.Fatalf("expected rpc factory error")
	}
	if _, err := NewRemoteRuntimeAdapter(RemoteRuntimeAdapterOptions{ListenAddress: "dial-failed", RequestTimeout: -1}); err == nil {
		t.Fatalf("expected authenticate fail-fast error")
	}

	adapter, err := NewRemoteRuntimeAdapter(RemoteRuntimeAdapterOptions{
		ListenAddress:  "ok",
		RequestTimeout: -1,
	})
	if err != nil {
		t.Fatalf("NewRemoteRuntimeAdapter() error = %v", err)
	}
	if adapter.timeout != defaultRemoteRuntimeTimeout {
		t.Fatalf("timeout = %v, want %v", adapter.timeout, defaultRemoteRuntimeTimeout)
	}
	_ = adapter.Close()
}

func TestRemoteRuntimeAdapterPrepareUserInputAndRun(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayLoadSession: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionLoadSession, SessionID: "s-1"},
			protocol.MethodGatewayBindStream:  {Type: gateway.FrameTypeAck, Action: gateway.FrameActionBindStream, SessionID: "s-1", RunID: "r-1"},
			protocol.MethodGatewayRun:         {Type: gateway.FrameTypeAck, Action: gateway.FrameActionRun, SessionID: "s-1", RunID: "r-1"},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	streamClient := &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.PrepareUserInput(ctx, agentruntime.PrepareInput{}); err == nil {
		t.Fatalf("expected context cancellation error")
	}

	input, err := adapter.PrepareUserInput(context.Background(), agentruntime.PrepareInput{
		SessionID: "  ",
		RunID:     "",
		Text:      "  hello  ",
		Images: []agentruntime.UserImageInput{
			{Path: "   "},
			{Path: " /tmp/a.png ", MimeType: " image/png "},
		},
		Workdir: " /repo ",
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	if strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.RunID) == "" {
		t.Fatalf("session/run id should be generated")
	}
	if len(input.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(input.Parts))
	}

	if err := adapter.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	methods := rpcClient.snapshotMethods()
	if len(methods) != 3 || methods[2] != protocol.MethodGatewayRun {
		t.Fatalf("unexpected method chain: %#v", methods)
	}
}

func TestRemoteRuntimeAdapterCompactResolvePermissionAndListSessions(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayBindStream: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionBindStream},
			protocol.MethodGatewayCompact: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionCompact,
				Payload: gateway.CompactResult{
					Applied:      true,
					BeforeChars:  100,
					AfterChars:   40,
					TriggerMode:  "auto",
					TranscriptID: "t-1",
				},
			},
			protocol.MethodGatewayResolvePermission: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionResolvePermission},
			protocol.MethodGatewayListSessions: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionListSessions,
				Payload: map[string]any{
					"sessions": []gateway.SessionSummary{{ID: " s1 ", Title: " t1 "}},
				},
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	streamClient := &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 2)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.Compact(context.Background(), agentruntime.CompactInput{}); err == nil {
		t.Fatalf("expected compact empty session id error")
	}

	compactResult, err := adapter.Compact(context.Background(), agentruntime.CompactInput{SessionID: "s1", RunID: "r1"})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !compactResult.Applied || compactResult.BeforeChars != 100 || compactResult.AfterChars != 40 {
		t.Fatalf("compact result mismatch: %#v", compactResult)
	}

	if err := adapter.ResolvePermission(context.Background(), agentruntime.PermissionResolutionInput{RequestID: " req ", Decision: "APPROVE"}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}

	summaries, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "s1" || summaries[0].Title != "t1" {
		t.Fatalf("summaries mismatch: %#v", summaries)
	}
}

func TestRemoteRuntimeAdapterUnsupportedSkillMethods(t *testing.T) {
	t.Parallel()

	adapter := newRemoteRuntimeAdapterWithClients(
		&stubRemoteRPCClient{notifications: make(chan gatewayRPCNotification)},
		&stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)},
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.ActivateSessionSkill(context.Background(), "s", "skill"); err == nil {
		t.Fatalf("ActivateSessionSkill should be unsupported")
	}
	if err := adapter.DeactivateSessionSkill(context.Background(), "s", "skill"); err == nil {
		t.Fatalf("DeactivateSessionSkill should be unsupported")
	}
	if _, err := adapter.ListSessionSkills(context.Background(), "s"); err == nil {
		t.Fatalf("ListSessionSkills should be unsupported")
	}
}

func TestRemoteRuntimeAdapterCallFrameAndDecodeHelpers(t *testing.T) {
	t.Parallel()

	adapter := newRemoteRuntimeAdapterWithClients(nil, &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.callFrame(context.Background(), protocol.MethodGatewayPing, nil, GatewayRPCCallOptions{}); err == nil {
		t.Fatalf("expected nil rpc client error")
	}
	if err := adapter.authenticate(context.Background()); err == nil {
		t.Fatalf("authenticate should fail on nil rpc client")
	}

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			"error-without-payload": {Type: gateway.FrameTypeError},
			"error-with-payload": {
				Type:  gateway.FrameTypeError,
				Error: &gateway.FrameError{Code: "bad", Message: "oops"},
			},
			"unexpected-type": {Type: gateway.FrameTypeEvent},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter.rpcClient = rpcClient

	if _, err := adapter.callFrame(context.Background(), "error-without-payload", nil, GatewayRPCCallOptions{}); err == nil {
		t.Fatalf("expected error frame without payload")
	}
	if _, err := adapter.callFrame(context.Background(), "error-with-payload", nil, GatewayRPCCallOptions{}); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("expected frame error mapping, got %v", err)
	}
	if _, err := adapter.callFrame(context.Background(), "unexpected-type", nil, GatewayRPCCallOptions{}); err == nil {
		t.Fatalf("expected unexpected frame type error")
	}

	if err := decodeIntoValue(map[string]any{"a": 1}, nil); err == nil {
		t.Fatalf("decodeIntoValue should reject nil target")
	}
	if err := decodeIntoValue(func() {}, &map[string]any{}); err == nil {
		t.Fatalf("decodeIntoValue should fail on marshal error")
	}
	if err := decodeIntoValue(map[string]any{"value": "x"}, &[]int{}); err == nil {
		t.Fatalf("decodeIntoValue should fail on unmarshal mismatch")
	}

	decoded, err := decodeFramePayload[gateway.CompactResult](map[string]any{"applied": true})
	if err != nil || !decoded.Applied {
		t.Fatalf("decodeFramePayload() = (%#v, %v)", decoded, err)
	}
}

func TestRemoteRuntimeAdapterEventObservationAndActiveRunState(t *testing.T) {
	t.Parallel()

	eventCh := make(chan agentruntime.RuntimeEvent, 3)
	streamClient := &stubRemoteStreamClient{events: eventCh}
	adapter := newRemoteRuntimeAdapterWithClients(
		&stubRemoteRPCClient{notifications: make(chan gatewayRPCNotification)},
		streamClient,
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	eventCh <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentChunk, RunID: "run-a", SessionID: "session-a"}
	eventCh <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentDone, RunID: "run-a", SessionID: "session-a"}
	close(eventCh)

	for i := 0; i < 2; i++ {
		select {
		case <-adapter.Events():
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for forwarded event")
		}
	}

	runID, sessionID := adapter.activeRun()
	if runID != "" || sessionID != "session-a" {
		t.Fatalf("active run state mismatch: run=%q session=%q", runID, sessionID)
	}

	adapter.setActiveRun(" run-b ", " session-b ")
	adapter.clearActiveRun("other-run")
	runID, _ = adapter.activeRun()
	if runID != "run-b" {
		t.Fatalf("clearActiveRun should keep different run, got %q", runID)
	}
	adapter.clearActiveRun("run-b")
	runID, _ = adapter.activeRun()
	if runID != "" {
		t.Fatalf("expected cleared run id, got %q", runID)
	}
}

func TestRemoteRuntimeAdapterLoadSessionAndCancelErrorPaths(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		callErrs: map[string]error{protocol.MethodGatewayCancel: errors.New("cancel failed")},
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayLoadSession: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionLoadSession, Payload: func() {}},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.LoadSession(context.Background(), " "); err == nil {
		t.Fatalf("expected empty id validation error")
	}
	if _, err := adapter.LoadSession(context.Background(), "session-1"); err == nil {
		t.Fatalf("expected payload decode error")
	}

	adapter.setActiveRun("run-1", "session-1")
	if !adapter.CancelActiveRun() {
		t.Fatalf("expected cancel attempt for active run")
	}
}

func TestRemoteRuntimeAdapterSubmitAndCompactErrorPaths(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		callErrs: map[string]error{
			protocol.MethodGatewayBindStream: errors.New("bind failed"),
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.Submit(context.Background(), agentruntime.PrepareInput{}); err == nil || !strings.Contains(err.Error(), "bind failed") {
		t.Fatalf("expected bind failed submit error, got %v", err)
	}
	params := rpcClient.snapshotParams()[protocol.MethodGatewayLoadSession].(protocol.LoadSessionParams)
	if strings.TrimSpace(params.SessionID) == "" {
		t.Fatalf("Submit() should generate default session id")
	}

	rpcClient.authErr = errors.New("auth failed")
	if _, err := adapter.Compact(context.Background(), agentruntime.CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("expected compact auth error, got %v", err)
	}
	rpcClient.authErr = nil
	rpcClient.callErrs[protocol.MethodGatewayBindStream] = errors.New("bind compact failed")
	if _, err := adapter.Compact(context.Background(), agentruntime.CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "bind compact failed") {
		t.Fatalf("expected compact bind error, got %v", err)
	}
	rpcClient.callErrs[protocol.MethodGatewayBindStream] = nil
	rpcClient.callErrs[protocol.MethodGatewayCompact] = errors.New("compact failed")
	if _, err := adapter.Compact(context.Background(), agentruntime.CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "compact failed") {
		t.Fatalf("expected compact rpc error, got %v", err)
	}
}

func TestRemoteRuntimeAdapterListAndLoadSessionErrorPaths(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan agentruntime.RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	rpcClient.authErr = errors.New("auth failed")
	if _, err := adapter.ListSessions(context.Background()); err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("expected list auth error, got %v", err)
	}
	rpcClient.authErr = nil
	rpcClient.callErrs = map[string]error{protocol.MethodGatewayListSessions: errors.New("list failed")}
	if _, err := adapter.ListSessions(context.Background()); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("expected list rpc error, got %v", err)
	}
	rpcClient.callErrs = nil
	rpcClient.frames = map[string]gateway.MessageFrame{
		protocol.MethodGatewayListSessions: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionListSessions, Payload: func() {}},
	}
	if _, err := adapter.ListSessions(context.Background()); err == nil {
		t.Fatalf("expected list decode error")
	}

	rpcClient.authErr = errors.New("auth load failed")
	if _, err := adapter.LoadSession(context.Background(), "s-1"); err == nil || !strings.Contains(err.Error(), "auth load failed") {
		t.Fatalf("expected load auth error, got %v", err)
	}
	rpcClient.authErr = nil
	rpcClient.callErrs = map[string]error{protocol.MethodGatewayLoadSession: errors.New("load failed")}
	if _, err := adapter.LoadSession(context.Background(), "s-1"); err == nil || !strings.Contains(err.Error(), "load failed") {
		t.Fatalf("expected load rpc error, got %v", err)
	}
}

func TestRemoteRuntimeAdapterRenderInputHelpers(t *testing.T) {
	t.Parallel()

	text := renderInputTextFromParts([]providertypes.ContentPart{
		providertypes.NewTextPart("  first  "),
		providertypes.NewRemoteImagePart("/tmp/a.png"),
		providertypes.NewTextPart("second"),
		providertypes.NewTextPart("   "),
	})
	if text != "first\nsecond" {
		t.Fatalf("renderInputTextFromParts() = %q", text)
	}

	images := renderInputImagesFromParts([]providertypes.ContentPart{
		providertypes.NewTextPart("x"),
		providertypes.NewRemoteImagePart("  "),
		providertypes.ContentPart{
			Kind: providertypes.ContentPartImage,
			Image: &providertypes.ImagePart{
				URL:   " /tmp/b.png ",
				Asset: &providertypes.AssetRef{MimeType: " image/png "},
			},
		},
	})
	if len(images) != 1 || images[0].Path != "/tmp/b.png" || images[0].MimeType != "image/png" {
		t.Fatalf("renderInputImagesFromParts() = %#v", images)
	}

	params := buildGatewayRunParams(" s ", " r ", agentruntime.PrepareInput{Text: " hi ", Workdir: " /w ", Images: []agentruntime.UserImageInput{{Path: " /img.png ", MimeType: " image/png "}, {Path: " "}}})
	if params.SessionID != "s" || params.RunID != "r" || params.Workdir != "/w" || params.InputText != "hi" || len(params.InputParts) != 1 {
		t.Fatalf("buildGatewayRunParams() = %#v", params)
	}
}
