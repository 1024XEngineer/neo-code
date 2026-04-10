package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/tools"
)

func TestResolvePermissionValidation(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{}); err == nil {
		t.Fatalf("expected empty request id error")
	}
	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: "perm-1",
		Decision:  PermissionResolutionDecision("invalid"),
	}); err == nil {
		t.Fatalf("expected invalid decision error")
	}
	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: "perm-not-found",
		Decision:  PermissionResolutionAllowOnce,
	}); err == nil {
		t.Fatalf("expected request not found error")
	}
}

func TestResolvePermissionSuccess(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	request := registerPendingPermission(service, permissionExecutionInput{
		RunID:     "run-permission",
		SessionID: "session-permission",
		Call: providertypes.ToolCall{
			ID:   "call-1",
			Name: "webfetch",
		},
	}, security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			Operation:  "fetch",
			TargetType: security.TargetTypeURL,
			Target:     "https://example.com",
		},
	})
	defer clearPendingPermission(service, request.RequestID)

	errCh := make(chan error, 1)
	go func() {
		errCh <- service.ResolvePermission(context.Background(), PermissionResolutionInput{
			RequestID: request.RequestID,
			Decision:  PermissionResolutionAllowSession,
		})
	}()

	select {
	case resolved := <-request.ResultCh:
		if resolved != PermissionResolutionAllowSession {
			t.Fatalf("expected allow session decision, got %q", resolved)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting permission resolution")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}
}

func TestResolvePermissionDuplicateSubmissionIsNonBlocking(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	request := registerPendingPermission(service, permissionExecutionInput{
		RunID:     "run-permission-dup",
		SessionID: "session-permission-dup",
		Call: providertypes.ToolCall{
			ID:   "call-dup",
			Name: "webfetch",
		},
	}, security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "webfetch",
			Resource: "webfetch",
		},
	})
	defer clearPendingPermission(service, request.RequestID)

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: request.RequestID,
		Decision:  PermissionResolutionAllowOnce,
	}); err != nil {
		t.Fatalf("first ResolvePermission() error = %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- service.ResolvePermission(context.Background(), PermissionResolutionInput{
			RequestID: request.RequestID,
			Decision:  PermissionResolutionAllowSession,
		})
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second ResolvePermission() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("second ResolvePermission() should not block")
	}
}

func TestServiceRunPermissionRejectFlow(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	tool := &stubTool{name: "webfetch", content: "should-not-run"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:       "ask-webfetch",
			Type:     security.ActionTypeRead,
			Resource: "webfetch",
			Decision: security.DecisionAsk,
			Reason:   "requires approval",
		},
	})
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: "assistant",
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-ask-reject", Name: "webfetch", Arguments: `{"url":"https://example.com/private"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message:      providertypes.Message{Role: "assistant", Content: "done"},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- service.Run(context.Background(), UserInput{RunID: "run-permission-reject", Content: "fetch private"})
	}()

	var requestPayload PermissionRequestPayload
waitRequest:
	for {
		select {
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting permission request")
		case event := <-service.Events():
			if event.Type != EventPermissionRequest {
				continue
			}
			payload, ok := event.Payload.(PermissionRequestPayload)
			if !ok {
				t.Fatalf("expected permission request payload, got %#v", event.Payload)
			}
			requestPayload = payload
			break waitRequest
		}
	}

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: requestPayload.RequestID,
		Decision:  PermissionResolutionReject,
	}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}
	if err := <-runErrCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if tool.callCount != 0 {
		t.Fatalf("expected tool not executed after reject, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})

	found := false
	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected permission resolved payload, got %#v", event.Payload)
		}
		if payload.Decision == "deny" && payload.ResolvedAs == "rejected" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected user reject resolved payload")
	}
}

func TestPermissionHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizePermissionResolutionDecision(PermissionResolutionDecision("Y")); got != PermissionResolutionAllowOnce {
		t.Fatalf("expected Y => allow_once, got %q", got)
	}
	if got := normalizePermissionResolutionDecision(PermissionResolutionDecision("a")); got != PermissionResolutionAllowSession {
		t.Fatalf("expected a => allow_session, got %q", got)
	}
	if got := normalizePermissionResolutionDecision(PermissionResolutionDecision("n")); got != PermissionResolutionReject {
		t.Fatalf("expected n => reject, got %q", got)
	}
	if got := normalizePermissionResolutionDecision(PermissionResolutionDecision("???")); got != "" {
		t.Fatalf("expected unknown => empty, got %q", got)
	}

	if scope, err := rememberScopeFromDecision(PermissionResolutionAllowOnce); err != nil || scope != tools.SessionPermissionScopeOnce {
		t.Fatalf("expected once scope, got %q / %v", scope, err)
	}
	if scope, err := rememberScopeFromDecision(PermissionResolutionAllowSession); err != nil || scope != tools.SessionPermissionScopeAlways {
		t.Fatalf("expected always scope, got %q / %v", scope, err)
	}
	if scope, err := rememberScopeFromDecision(PermissionResolutionReject); err != nil || scope != tools.SessionPermissionScopeReject {
		t.Fatalf("expected reject scope, got %q / %v", scope, err)
	}
	if _, err := rememberScopeFromDecision(PermissionResolutionDecision("invalid")); err == nil {
		t.Fatalf("expected invalid decision error")
	}

	category := permissionToolCategory(security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "filesystem_grep",
			Resource: "filesystem_grep",
		},
	})
	if category != "filesystem_read" {
		t.Fatalf("expected filesystem_read category, got %q", category)
	}

	category = permissionToolCategory(security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "webfetch",
			Resource: "webfetch",
		},
	})
	if category != "webfetch" {
		t.Fatalf("expected webfetch category, got %q", category)
	}
}

func TestResolvePermissionCanceledContext(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	request := registerPendingPermission(service, permissionExecutionInput{
		RunID:     "run-canceled",
		SessionID: "session-canceled",
		Call: providertypes.ToolCall{
			ID:   "call-canceled",
			Name: "webfetch",
		},
	}, security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "webfetch",
			Resource: "webfetch",
		},
	})
	defer clearPendingPermission(service, request.RequestID)
	request.ResultCh <- PermissionResolutionAllowOnce

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.ResolvePermission(ctx, PermissionResolutionInput{
		RequestID: request.RequestID,
		Decision:  PermissionResolutionAllowOnce,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestExecuteToolCallWithPermissionReturnsContextCanceledFromEmitChunk(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.Register(&stubTool{
		name: "filesystem_read_file",
		executeFn: func(_ context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if input.EmitChunk == nil {
				t.Fatalf("expected EmitChunk callback")
			}
			if err := input.EmitChunk([]byte("stream-chunk")); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled from emitter, got %v", err)
			}
			return tools.NewErrorResult(input.Name, "emit failed", "", nil), context.Canceled
		},
	})

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, execErr := service.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:     "run-canceled",
		SessionID: "session-canceled",
		Call: providertypes.ToolCall{
			ID:        "call-canceled",
			Name:      "filesystem_read_file",
			Arguments: `{"path":"README.md"}`,
		},
		ToolTimeout: time.Second,
	})
	if !errors.Is(execErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", execErr)
	}
}

type doneSignalContext struct {
	context.Context
	doneCalled chan struct{}
	once       sync.Once
}

// Done 在 runtime.emit 进入阻塞发送分支时发出信号，便于测试精确控制取消时机。
func (c *doneSignalContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.doneCalled)
	})
	return c.Context.Done()
}

func TestExecuteToolCallWithPermissionDoesNotRecheckContextAfterSuccessfulEmit(t *testing.T) {
	t.Parallel()

	var cancel context.CancelFunc
	registry := tools.NewRegistry()
	registry.Register(&stubTool{
		name: "filesystem_read_file",
		executeFn: func(_ context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if input.EmitChunk == nil {
				t.Fatalf("expected EmitChunk callback")
			}
			if err := input.EmitChunk([]byte("stream-chunk")); err != nil {
				t.Fatalf("expected successful emit, got %v", err)
			}
			cancel()
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		},
	})

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	service.events = make(chan RuntimeEvent, 1)

	result, execErr := service.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:     "run-successful-emit",
		SessionID: "session-successful-emit",
		Call: providertypes.ToolCall{
			ID:        "call-successful-emit",
			Name:      "filesystem_read_file",
			Arguments: `{"path":"README.md"}`,
		},
		ToolTimeout: time.Second,
	})
	if execErr != nil {
		t.Fatalf("expected nil error after successful emit, got %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("expected successful tool result, got %+v", result)
	}
}

func TestExecuteToolCallWithPermissionReturnsContextCanceledWhenChunkNotDelivered(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.Register(&stubTool{
		name: "filesystem_read_file",
		executeFn: func(_ context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if input.EmitChunk == nil {
				t.Fatalf("expected EmitChunk callback")
			}
			if err := input.EmitChunk([]byte("stream-chunk")); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled from emitter, got %v", err)
			}
			return tools.NewErrorResult(input.Name, "emit failed", "", nil), context.Canceled
		},
	})

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.events = make(chan RuntimeEvent, 1)
	service.events <- RuntimeEvent{Type: EventAgentChunk}

	baseCtx, cancel := context.WithCancel(context.Background())
	ctx := &doneSignalContext{
		Context:    baseCtx,
		doneCalled: make(chan struct{}),
	}
	go func() {
		<-ctx.doneCalled
		cancel()
	}()

	_, execErr := service.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:     "run-canceled-blocked",
		SessionID: "session-canceled-blocked",
		Call: providertypes.ToolCall{
			ID:        "call-canceled-blocked",
			Name:      "filesystem_read_file",
			Arguments: `{"path":"README.md"}`,
		},
		ToolTimeout: time.Second,
	})
	if !errors.Is(execErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", execErr)
	}
}
