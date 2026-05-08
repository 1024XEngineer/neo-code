package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/skills"
)

func TestNewRemoteRuntimeAdapterBranches(t *testing.T) {
	t.Parallel()

	originalRPCFactory := newGatewayRPCClientFactory
	originalStreamFactory := newGatewayStreamClientFactory
	t.Cleanup(func() {
		newGatewayRPCClientFactory = originalRPCFactory
		newGatewayStreamClientFactory = originalStreamFactory
	})

	newGatewayRPCClientFactory = func(options GatewayRPCClientOptions) (remoteGatewayRPCClient, error) {
		if strings.TrimSpace(options.ListenAddress) == "error" {
			return nil, errors.New("build rpc failed")
		}
		client := &stubRemoteRPCClient{
			notifications: make(chan gatewayRPCNotification, 4),
		}
		if options.ListenAddress == "dial-failed" {
			client.authErr = errors.New("dial failed")
		}
		if options.ListenAddress == "bind-failed" {
			client.callErrs = map[string]error{
				protocol.MethodGatewayBindStream: errors.New("bind failed"),
			}
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
	if _, err := NewRemoteRuntimeAdapter(RemoteRuntimeAdapterOptions{ListenAddress: "bind-failed", RequestTimeout: -1}); err == nil {
		t.Fatalf("expected bindStream fail-fast error")
	}

	adapter, err := NewRemoteRuntimeAdapter(RemoteRuntimeAdapterOptions{
		ListenAddress:  "ok",
		RequestTimeout: -1,
		RetryCount:     0,
	})
	if err != nil {
		t.Fatalf("NewRemoteRuntimeAdapter() error = %v", err)
	}
	if adapter.timeout != defaultRemoteRuntimeTimeout {
		t.Fatalf("timeout = %v, want %v", adapter.timeout, defaultRemoteRuntimeTimeout)
	}
	if adapter.retryCount != defaultGatewayRPCRetryCount {
		t.Fatalf("retryCount = %d, want %d", adapter.retryCount, defaultGatewayRPCRetryCount)
	}
	_ = adapter.Close()
}

func TestRemoteRuntimeAdapterSubmitGeneratesIDsAndNormalizesInput(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayBindStream: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionBindStream, SessionID: "s-1", RunID: "r-1"},
			protocol.MethodGatewayRun:        {Type: gateway.FrameTypeAck, Action: gateway.FrameActionRun, SessionID: "s-1", RunID: "r-1"},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.Submit(context.Background(), PrepareInput{
		SessionID: "  ",
		RunID:     "",
		Text:      "  hello  ",
		Images: []UserImageInput{
			{Path: "   "},
			{Path: " /tmp/a.png ", MimeType: " image/png "},
		},
		Workdir: " /repo ",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	methods := rpcClient.snapshotMethods()
	if len(methods) != 2 || methods[1] != protocol.MethodGatewayRun {
		t.Fatalf("unexpected method chain: %#v", methods)
	}
	params, ok := rpcClient.snapshotParams()[protocol.MethodGatewayRun].(protocol.RunParams)
	if !ok {
		t.Fatalf("run params type = %T, want protocol.RunParams", rpcClient.snapshotParams()[protocol.MethodGatewayRun])
	}
	if !params.NewSession {
		t.Fatalf("expected new_session=true when submitting draft input, params=%#v", params)
	}
	if strings.TrimSpace(params.SessionID) == "" || strings.TrimSpace(params.RunID) == "" {
		t.Fatalf("session/run id should be generated: %#v", params)
	}
	if params.InputText != "hello" || params.Workdir != "/repo" {
		t.Fatalf("unexpected normalized run payload: %#v", params)
	}
	if len(params.InputParts) != 1 || params.InputParts[0].Media == nil || params.InputParts[0].Media.URI != "/tmp/a.png" {
		t.Fatalf("unexpected run input parts: %#v", params.InputParts)
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
	streamClient := &stubRemoteStreamClient{events: make(chan RuntimeEvent)}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, streamClient, time.Second, 2)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.Compact(context.Background(), CompactInput{}); err == nil {
		t.Fatalf("expected compact empty session id error")
	}

	compactResult, err := adapter.Compact(context.Background(), CompactInput{SessionID: "s1", RunID: "r1"})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !compactResult.Applied || compactResult.BeforeChars != 100 || compactResult.AfterChars != 40 {
		t.Fatalf("compact result mismatch: %#v", compactResult)
	}

	if err := adapter.ResolvePermission(context.Background(), PermissionResolutionInput{RequestID: " req ", Decision: "APPROVE"}); err != nil {
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

func TestRemoteRuntimeAdapterCompactPayloadDecodeError(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayBindStream: {Type: gateway.FrameTypeAck, Action: gateway.FrameActionBindStream},
			protocol.MethodGatewayCompact: {
				Type:    gateway.FrameTypeAck,
				Action:  gateway.FrameActionCompact,
				Payload: "invalid-payload",
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(
		rpcClient,
		&stubRemoteStreamClient{events: make(chan RuntimeEvent)},
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.Compact(context.Background(), CompactInput{SessionID: "s1", RunID: "r1"}); err == nil {
		t.Fatalf("expected compact payload decode error")
	}
}

func TestRemoteRuntimeAdapterSkillMethods(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayActivateSessionSkill: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionActivateSessionSkill,
			},
			protocol.MethodGatewayDeactivateSessionSkill: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionDeactivateSessionSkill,
			},
			protocol.MethodGatewayListSessionSkills: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionListSessionSkills,
				Payload: map[string]any{
					"skills": []gateway.SessionSkillState{
						{
							SkillID: "go-review",
						},
					},
				},
			},
			protocol.MethodGatewayListAvailableSkills: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionListAvailableSkills,
				Payload: map[string]any{
					"skills": []gateway.AvailableSkillState{
						{
							Descriptor: gateway.SkillDescriptor{
								ID:          "go-review",
								Name:        "Go Review",
								Description: "Review Go code",
								Version:     "v1",
								Source: gateway.SkillSource{
									Kind:  "local",
									Layer: "project",
								},
								Scope: "session",
							},
							Active: true,
						},
					},
				},
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.ActivateSessionSkill(context.Background(), " s1 ", " go-review "); err != nil {
		t.Fatalf("ActivateSessionSkill() error = %v", err)
	}
	if err := adapter.DeactivateSessionSkill(context.Background(), " s1 ", " go-review "); err != nil {
		t.Fatalf("DeactivateSessionSkill() error = %v", err)
	}
	sessionSkills, err := adapter.ListSessionSkills(context.Background(), " s1 ")
	if err != nil {
		t.Fatalf("ListSessionSkills() error = %v", err)
	}
	if len(sessionSkills) != 1 || sessionSkills[0].SkillID != "go-review" {
		t.Fatalf("session skills = %#v, want one go-review skill", sessionSkills)
	}
	availableSkills, err := adapter.ListAvailableSkills(context.Background(), " s1 ")
	if err != nil {
		t.Fatalf("ListAvailableSkills() error = %v", err)
	}
	if len(availableSkills) != 1 || availableSkills[0].Descriptor.ID != "go-review" || !availableSkills[0].Active {
		t.Fatalf("available skills = %#v, want one active go-review skill", availableSkills)
	}
	if availableSkills[0].Descriptor.Source.Layer != skills.SourceLayerProject {
		t.Fatalf("available skills source layer = %q, want %q", availableSkills[0].Descriptor.Source.Layer, skills.SourceLayerProject)
	}

	params := rpcClient.snapshotParams()
	activateParams, ok := params[protocol.MethodGatewayActivateSessionSkill].(protocol.ActivateSessionSkillParams)
	if !ok {
		t.Fatalf("activate params type = %T, want protocol.ActivateSessionSkillParams", params[protocol.MethodGatewayActivateSessionSkill])
	}
	if activateParams.SessionID != "s1" || activateParams.SkillID != "go-review" {
		t.Fatalf("activate params = %#v, want trimmed session_id/skill_id", activateParams)
	}
	deactivateParams, ok := params[protocol.MethodGatewayDeactivateSessionSkill].(protocol.DeactivateSessionSkillParams)
	if !ok {
		t.Fatalf(
			"deactivate params type = %T, want protocol.DeactivateSessionSkillParams",
			params[protocol.MethodGatewayDeactivateSessionSkill],
		)
	}
	if deactivateParams.SessionID != "s1" || deactivateParams.SkillID != "go-review" {
		t.Fatalf("deactivate params = %#v, want trimmed session_id/skill_id", deactivateParams)
	}
}

func TestRemoteRuntimeAdapterSkillMethodsGatewayErrorsPassthrough(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		callErrs: map[string]error{
			protocol.MethodGatewayActivateSessionSkill: &GatewayRPCError{
				Method:      protocol.MethodGatewayActivateSessionSkill,
				Code:        protocol.JSONRPCCodeMethodNotFound,
				GatewayCode: protocol.GatewayCodeUnsupportedAction,
				Message:     "method not found",
			},
			protocol.MethodGatewayDeactivateSessionSkill: &GatewayRPCError{
				Method:      protocol.MethodGatewayDeactivateSessionSkill,
				Code:        protocol.JSONRPCCodeMethodNotFound,
				GatewayCode: protocol.GatewayCodeUnsupportedAction,
				Message:     "method not found",
			},
			protocol.MethodGatewayListSessionSkills: &GatewayRPCError{
				Method:      protocol.MethodGatewayListSessionSkills,
				Code:        protocol.JSONRPCCodeMethodNotFound,
				GatewayCode: protocol.GatewayCodeUnsupportedAction,
				Message:     "method not found",
			},
			protocol.MethodGatewayListAvailableSkills: &GatewayRPCError{
				Method:      protocol.MethodGatewayListAvailableSkills,
				Code:        protocol.JSONRPCCodeMethodNotFound,
				GatewayCode: protocol.GatewayCodeUnsupportedAction,
				Message:     "method not found",
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	assertRPCMethodNotFound := func(t *testing.T, method string, err error) {
		t.Helper()
		var rpcErr *GatewayRPCError
		if !errors.As(err, &rpcErr) {
			t.Fatalf("%s err = %v, want GatewayRPCError passthrough", method, err)
		}
		if rpcErr.Method != method {
			t.Fatalf("%s method = %q, want %q", method, rpcErr.Method, method)
		}
		if rpcErr.Code != protocol.JSONRPCCodeMethodNotFound {
			t.Fatalf("%s code = %d, want %d", method, rpcErr.Code, protocol.JSONRPCCodeMethodNotFound)
		}
		if rpcErr.GatewayCode != protocol.GatewayCodeUnsupportedAction {
			t.Fatalf("%s gateway_code = %q, want %q", method, rpcErr.GatewayCode, protocol.GatewayCodeUnsupportedAction)
		}
	}

	assertRPCMethodNotFound(t, protocol.MethodGatewayActivateSessionSkill, adapter.ActivateSessionSkill(context.Background(), "s", "skill"))
	assertRPCMethodNotFound(
		t,
		protocol.MethodGatewayDeactivateSessionSkill,
		adapter.DeactivateSessionSkill(context.Background(), "s", "skill"),
	)
	_, listSessionErr := adapter.ListSessionSkills(context.Background(), "s")
	assertRPCMethodNotFound(t, protocol.MethodGatewayListSessionSkills, listSessionErr)
	_, listAvailableErr := adapter.ListAvailableSkills(context.Background(), "s")
	assertRPCMethodNotFound(t, protocol.MethodGatewayListAvailableSkills, listAvailableErr)
}

func TestRemoteRuntimeAdapterCallFrameAndDecodeHelpers(t *testing.T) {
	t.Parallel()

	adapter := newRemoteRuntimeAdapterWithClients(nil, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
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

	eventCh := make(chan RuntimeEvent, 3)
	streamClient := &stubRemoteStreamClient{events: eventCh}
	adapter := newRemoteRuntimeAdapterWithClients(
		&stubRemoteRPCClient{notifications: make(chan gatewayRPCNotification)},
		streamClient,
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	eventCh <- RuntimeEvent{Type: EventAgentChunk, RunID: "run-a", SessionID: "session-a"}
	eventCh <- RuntimeEvent{Type: EventAgentDone, RunID: "run-a", SessionID: "session-a"}
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

	adapter.setActiveRun("run-c", "session-c")
	adapter.observeEvent(RuntimeEvent{Type: EventError})
	runID, sessionID = adapter.activeRun()
	if runID != "run-c" || sessionID != "session-c" {
		t.Fatalf("event error without run id should not clear active run, got run=%q session=%q", runID, sessionID)
	}
}

func TestNewRemoteRuntimeAdapterWithClientsNormalizesRetryCount(t *testing.T) {
	t.Parallel()

	adapter := newRemoteRuntimeAdapterWithClients(
		&stubRemoteRPCClient{notifications: make(chan gatewayRPCNotification)},
		&stubRemoteStreamClient{events: make(chan RuntimeEvent)},
		time.Second,
		0,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	if adapter.retryCount != defaultGatewayRPCRetryCount {
		t.Fatalf("retryCount = %d, want %d", adapter.retryCount, defaultGatewayRPCRetryCount)
	}
}

func TestRemoteRuntimeAdapterUsesDefaultRetryWhenOptionsZero(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayListSessions: {
				Type:    gateway.FrameTypeAck,
				Action:  gateway.FrameActionListSessions,
				Payload: map[string]any{"sessions": []gateway.SessionSummary{}},
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(
		rpcClient,
		&stubRemoteStreamClient{events: make(chan RuntimeEvent)},
		time.Second,
		0,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	if _, err := adapter.ListSessions(context.Background()); err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	options, ok := rpcClient.snapshotOptions()[protocol.MethodGatewayListSessions]
	if !ok {
		t.Fatalf("expected listSessions call options to be captured")
	}
	if options.Retries != defaultGatewayRPCRetryCount {
		t.Fatalf("listSessions retries = %d, want %d", options.Retries, defaultGatewayRPCRetryCount)
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
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
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
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
	t.Cleanup(func() { _ = adapter.Close() })

	if err := adapter.Submit(context.Background(), PrepareInput{}); err == nil || !strings.Contains(err.Error(), "bind failed") {
		t.Fatalf("expected bind failed submit error, got %v", err)
	}
	methods := rpcClient.snapshotMethods()
	if len(methods) != 1 || methods[0] != protocol.MethodGatewayBindStream {
		t.Fatalf("Submit() should fail after bindStream and before loadSession, methods=%#v", methods)
	}
	bindParams, ok := rpcClient.snapshotParams()[protocol.MethodGatewayBindStream].(protocol.BindStreamParams)
	if !ok || strings.TrimSpace(bindParams.SessionID) == "" {
		t.Fatalf("Submit() should generate default session id for bindStream, params=%#v", rpcClient.snapshotParams()[protocol.MethodGatewayBindStream])
	}

	rpcClient.authErr = errors.New("auth failed")
	if _, err := adapter.Compact(context.Background(), CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("expected compact auth error, got %v", err)
	}
	rpcClient.authErr = nil
	rpcClient.callErrs[protocol.MethodGatewayBindStream] = errors.New("bind compact failed")
	if _, err := adapter.Compact(context.Background(), CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "bind compact failed") {
		t.Fatalf("expected compact bind error, got %v", err)
	}
	rpcClient.callErrs[protocol.MethodGatewayBindStream] = nil
	rpcClient.callErrs[protocol.MethodGatewayCompact] = errors.New("compact failed")
	if _, err := adapter.Compact(context.Background(), CompactInput{SessionID: "s-1"}); err == nil || !strings.Contains(err.Error(), "compact failed") {
		t.Fatalf("expected compact rpc error, got %v", err)
	}
}

func TestRemoteRuntimeAdapterListAndLoadSessionErrorPaths(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(rpcClient, &stubRemoteStreamClient{events: make(chan RuntimeEvent)}, time.Second, 1)
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

func TestRemoteRuntimeAdapterBuildGatewayRunParams(t *testing.T) {
	t.Parallel()

	params := buildGatewayRunParams(" s ", " r ", true, PrepareInput{Text: " hi ", Workdir: " /w ", Mode: " plan ", Images: []UserImageInput{{Path: " /img.png ", MimeType: " image/png "}, {Path: " "}}})
	if params.SessionID != "s" || !params.NewSession || params.RunID != "r" || params.Workdir != "/w" || params.Mode != "plan" || params.InputText != "hi" || len(params.InputParts) != 1 {
		t.Fatalf("buildGatewayRunParams() = %#v", params)
	}
}

func TestRemoteRuntimeAdapterListModels(t *testing.T) {
	t.Parallel()

	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayListModels: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionListModels,
				Payload: map[string]any{
					"models": []gateway.ModelEntry{
						{ID: " m-1 ", Name: " Model One "},
						{ID: " m-2 ", Name: ""},
						{ID: " ", Name: "ignored"},
					},
					"selected_model_id": " m-2 ",
				},
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(
		rpcClient,
		&stubRemoteStreamClient{events: make(chan RuntimeEvent)},
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	models, selectedID, err := adapter.ListModels(context.Background(), " session-1 ")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected two models after trimming/filtering, got %#v", models)
	}
	if models[0].ID != "m-1" || models[0].Name != "Model One" {
		t.Fatalf("unexpected first model mapping: %#v", models[0])
	}
	if models[1].ID != "m-2" || models[1].Name != "m-2" {
		t.Fatalf("unexpected second model mapping: %#v", models[1])
	}
	if selectedID != "m-2" {
		t.Fatalf("selected model id = %q, want %q", selectedID, "m-2")
	}

	params, ok := rpcClient.snapshotParams()[protocol.MethodGatewayListModels].(protocol.ListModelsParams)
	if !ok {
		t.Fatalf("listModels params type = %T, want protocol.ListModelsParams", rpcClient.snapshotParams()[protocol.MethodGatewayListModels])
	}
	if params.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", params.SessionID, "session-1")
	}
}

func TestRemoteRuntimeAdapterCheckpointAndWorkspaceMethods(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	rpcClient := &stubRemoteRPCClient{
		frames: map[string]gateway.MessageFrame{
			protocol.MethodGatewayListCheckpoints: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionListCheckpoints,
				Payload: []CheckpointEntry{
					{CheckpointID: "cp-1", SessionID: "s-1", CreatedAtMS: 1700000000000, Restorable: true},
				},
			},
			protocol.MethodGatewayRestoreCheckpoint: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionRestoreCheckpoint,
				Payload: CheckpointRestoreResult{
					CheckpointID: "cp-1",
					SessionID:    "s-1",
				},
			},
			protocol.MethodGatewayUndoRestore: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionUndoRestore,
				Payload: CheckpointRestoreResult{
					CheckpointID: "cp-guard",
					SessionID:    "s-1",
				},
			},
			protocol.MethodGatewayCheckpointDiff: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionCheckpointDiff,
				Payload: CheckpointDiffResult{
					CheckpointID:     "cp-1",
					PrevCheckpointID: "cp-0",
					Files: CheckpointDiffFiles{
						Modified: []string{"a.txt"},
					},
					Patch: "diff --git a/a.txt b/a.txt",
				},
			},
			protocol.MethodGatewayListWorkspaces: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionWorkspaceList,
				Payload: map[string]any{
					"workspaces": []WorkspaceRecord{
						{Hash: "ws-1", Path: "/repo", Name: "repo", CreatedAt: now, UpdatedAt: now},
					},
				},
			},
			protocol.MethodGatewayCreateWorkspace: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionWorkspaceCreate,
				Payload: map[string]any{
					"workspace": WorkspaceRecord{Hash: "ws-2", Path: "/repo2", Name: "repo2"},
				},
			},
			protocol.MethodGatewaySwitchWorkspace: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionWorkspaceSwitch,
				Payload: map[string]any{
					"workspace_hash": "ws-1",
				},
			},
			protocol.MethodGatewayRenameWorkspace: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionWorkspaceRename,
				Payload: map[string]any{
					"hash": "ws-1",
					"name": "renamed",
				},
			},
			protocol.MethodGatewayDeleteWorkspace: {
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionWorkspaceDelete,
				Payload: map[string]any{
					"hash": "ws-1",
				},
			},
		},
		notifications: make(chan gatewayRPCNotification),
	}
	adapter := newRemoteRuntimeAdapterWithClients(
		rpcClient,
		&stubRemoteStreamClient{events: make(chan RuntimeEvent)},
		time.Second,
		1,
	)
	t.Cleanup(func() { _ = adapter.Close() })

	checkpoints, err := adapter.ListCheckpoints(context.Background(), CheckpointListInput{
		SessionID:      " s-1 ",
		Limit:          20,
		RestorableOnly: true,
	})
	if err != nil || len(checkpoints) != 1 || checkpoints[0].CheckpointID != "cp-1" {
		t.Fatalf("ListCheckpoints() = (%#v, %v)", checkpoints, err)
	}
	restoreResult, err := adapter.RestoreCheckpoint(context.Background(), CheckpointRestoreInput{
		SessionID:    " s-1 ",
		CheckpointID: " cp-1 ",
		Force:        true,
	})
	if err != nil || restoreResult.CheckpointID != "cp-1" {
		t.Fatalf("RestoreCheckpoint() = (%#v, %v)", restoreResult, err)
	}
	undoResult, err := adapter.UndoRestoreCheckpoint(context.Background(), " s-1 ")
	if err != nil || undoResult.CheckpointID != "cp-guard" {
		t.Fatalf("UndoRestoreCheckpoint() = (%#v, %v)", undoResult, err)
	}
	diffResult, err := adapter.CheckpointDiff(context.Background(), " s-1 ", " cp-1 ")
	if err != nil || diffResult.CheckpointID != "cp-1" || len(diffResult.Files.Modified) != 1 {
		t.Fatalf("CheckpointDiff() = (%#v, %v)", diffResult, err)
	}

	workspaces, err := adapter.ListWorkspaces(context.Background())
	if err != nil || len(workspaces) != 1 || workspaces[0].Hash != "ws-1" {
		t.Fatalf("ListWorkspaces() = (%#v, %v)", workspaces, err)
	}
	createdWorkspace, err := adapter.CreateWorkspace(context.Background(), WorkspaceCreateInput{
		Path: " /repo2 ",
		Name: " repo2 ",
	})
	if err != nil || createdWorkspace.Hash != "ws-2" {
		t.Fatalf("CreateWorkspace() = (%#v, %v)", createdWorkspace, err)
	}
	if err := adapter.SwitchWorkspace(context.Background(), " ws-1 "); err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	renamedWorkspace, err := adapter.RenameWorkspace(context.Background(), WorkspaceRenameInput{
		WorkspaceHash: " ws-1 ",
		Name:          " renamed ",
	})
	if err != nil || renamedWorkspace.Hash != "ws-1" || renamedWorkspace.Name != "renamed" {
		t.Fatalf("RenameWorkspace() = (%#v, %v)", renamedWorkspace, err)
	}
	if err := adapter.DeleteWorkspace(context.Background(), WorkspaceDeleteInput{
		WorkspaceHash: " ws-1 ",
		RemoveData:    true,
	}); err != nil {
		t.Fatalf("DeleteWorkspace() error = %v", err)
	}
}
