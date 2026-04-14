package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/security"
	"neo-code/internal/tools"
	"neo-code/internal/tools/mcp"
)

type runtimeStubMCPClient struct {
	tools      []mcp.ToolDescriptor
	callResult mcp.CallResult
	callErr    error
	callCount  int
}

func (s *runtimeStubMCPClient) ListTools(ctx context.Context) ([]mcp.ToolDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]mcp.ToolDescriptor(nil), s.tools...), nil
}

func (s *runtimeStubMCPClient) CallTool(ctx context.Context, toolName string, arguments []byte) (mcp.CallResult, error) {
	if err := ctx.Err(); err != nil {
		return mcp.CallResult{}, err
	}
	s.callCount++
	if s.callErr != nil {
		return mcp.CallResult{}, s.callErr
	}
	return s.callResult, nil
}

func (s *runtimeStubMCPClient) HealthCheck(ctx context.Context) error {
	return ctx.Err()
}

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
		Decision:  approvalflow.DecisionAllowOnce,
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

	requestID, resultCh, err := service.approvalBroker.Open()
	if err != nil {
		t.Fatalf("open approval request: %v", err)
	}
	defer service.approvalBroker.Close(requestID)

	errCh := make(chan error, 1)
	go func() {
		errCh <- service.ResolvePermission(context.Background(), PermissionResolutionInput{
			RequestID: requestID,
			Decision:  approvalflow.DecisionAllowSession,
		})
	}()

	select {
	case resolved := <-resultCh:
		if resolved != approvalflow.DecisionAllowSession {
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

	requestID, _, err := service.approvalBroker.Open()
	if err != nil {
		t.Fatalf("open approval request: %v", err)
	}
	defer service.approvalBroker.Close(requestID)

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: requestID,
		Decision:  approvalflow.DecisionAllowOnce,
	}); err != nil {
		t.Fatalf("first ResolvePermission() error = %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- service.ResolvePermission(context.Background(), PermissionResolutionInput{
			RequestID: requestID,
			Decision:  approvalflow.DecisionAllowSession,
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

func TestAwaitPermissionDecisionSerializesConcurrentAskRequests(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	permissionErr := permissionDecisionAskError(t)

	type awaitResult struct {
		decision  approvalflow.Decision
		requestID string
		err       error
	}
	resultCh := make(chan awaitResult, 2)

	runAwait := func(callID string) {
		decision, requestID, err := service.awaitPermissionDecision(
			context.Background(),
			permissionExecutionInput{
				RunID:     "run-ask-serial",
				SessionID: "session-ask-serial",
				Call: providertypes.ToolCall{
					ID:   callID,
					Name: "filesystem_read_file",
				},
			},
			permissionErr,
		)
		resultCh <- awaitResult{decision: decision, requestID: requestID, err: err}
	}

	go runAwait("call-1")
	go runAwait("call-2")

	var firstReqID string
	select {
	case event := <-service.Events():
		if event.Type != EventPermissionRequested {
			t.Fatalf("expected first event permission requested, got %q", event.Type)
		}
		payload, ok := event.Payload.(PermissionRequestPayload)
		if !ok {
			t.Fatalf("expected PermissionRequestPayload, got %#v", event.Payload)
		}
		firstReqID = payload.RequestID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting first permission request")
	}

	select {
	case event := <-service.Events():
		t.Fatalf("unexpected second permission request before resolving first: %+v", event)
	case <-time.After(80 * time.Millisecond):
	}

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: firstReqID,
		Decision:  approvalflow.DecisionAllowOnce,
	}); err != nil {
		t.Fatalf("ResolvePermission(first) error = %v", err)
	}

	var secondReqID string
	select {
	case event := <-service.Events():
		if event.Type != EventPermissionRequested {
			t.Fatalf("expected second event permission requested, got %q", event.Type)
		}
		payload, ok := event.Payload.(PermissionRequestPayload)
		if !ok {
			t.Fatalf("expected PermissionRequestPayload, got %#v", event.Payload)
		}
		secondReqID = payload.RequestID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting second permission request")
	}

	if firstReqID == secondReqID {
		t.Fatalf("expected distinct permission request IDs, got %q", firstReqID)
	}

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: secondReqID,
		Decision:  approvalflow.DecisionAllowSession,
	}); err != nil {
		t.Fatalf("ResolvePermission(second) error = %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case res := <-resultCh:
			if res.err != nil {
				t.Fatalf("awaitPermissionDecision() error = %v", res.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting awaitPermissionDecision result")
		}
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
			if !isPermissionRequestEvent(event.Type) {
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
		Decision:  approvalflow.DecisionReject,
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

func TestServiceRunMCPPermissionAllowFlow(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	mcpClient := &runtimeStubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create issue", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{Content: "mcp create ok"},
	}
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", mcpClient); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewPolicyEngine(security.DecisionAllow, []security.PolicyRule{
		{
			ID:               "ask-github-create",
			Priority:         720,
			Decision:         security.DecisionAsk,
			Reason:           "mcp create requires approval",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.github.create_issue"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
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
						{ID: "call-mcp-allow", Name: "mcp.github.create_issue", Arguments: `{"title":"hello"}`},
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
		runErrCh <- service.Run(context.Background(), UserInput{RunID: "run-mcp-permission-allow", Content: "create issue"})
	}()

	var requestPayload PermissionRequestPayload
waitRequest:
	for {
		select {
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting permission request")
		case event := <-service.Events():
			if !isPermissionRequestEvent(event.Type) {
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

	if requestPayload.ToolName != "mcp.github.create_issue" ||
		requestPayload.ToolCategory != "mcp.github" ||
		requestPayload.RuleID != "ask-github-create" ||
		requestPayload.Reason != "mcp create requires approval" ||
		requestPayload.Decision != "ask" {
		t.Fatalf("unexpected permission request payload: %+v", requestPayload)
	}
	if requestPayload.RememberScope != "" {
		t.Fatalf("expected empty request remember scope, got %+v", requestPayload)
	}

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: requestPayload.RequestID,
		Decision:  approvalflow.DecisionAllowSession,
	}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}
	if err := <-runErrCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if mcpClient.callCount != 1 {
		t.Fatalf("expected MCP tool to execute once, got %d", mcpClient.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})

	foundResolved := false
	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
		}
		if payload.ToolName != "mcp.github.create_issue" ||
			payload.ToolCategory != "mcp.github" ||
			payload.RuleID != "ask-github-create" ||
			payload.Reason != "permission approved by user" ||
			payload.Decision != "allow" ||
			payload.ResolvedAs != "approved" ||
			payload.RememberScope != string(tools.SessionPermissionScopeAlways) {
			t.Fatalf("unexpected permission resolved payload: %+v", payload)
		}
		foundResolved = true
	}
	if !foundResolved {
		t.Fatalf("expected permission resolved event")
	}
}

func TestServiceRunMCPPermissionRejectFlow(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	mcpClient := &runtimeStubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create issue", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{Content: "should-not-run"},
	}
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", mcpClient); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewPolicyEngine(security.DecisionAllow, []security.PolicyRule{
		{
			ID:               "ask-github-create",
			Priority:         720,
			Decision:         security.DecisionAsk,
			Reason:           "mcp create requires approval",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.github.create_issue"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
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
						{ID: "call-mcp-reject", Name: "mcp.github.create_issue", Arguments: `{"title":"hello"}`},
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
		runErrCh <- service.Run(context.Background(), UserInput{RunID: "run-mcp-permission-reject", Content: "create issue"})
	}()

	var requestPayload PermissionRequestPayload
waitRequest:
	for {
		select {
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting permission request")
		case event := <-service.Events():
			if !isPermissionRequestEvent(event.Type) {
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
		Decision:  approvalflow.DecisionReject,
	}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}
	if err := <-runErrCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if mcpClient.callCount != 0 {
		t.Fatalf("expected rejected MCP tool not to execute, got %d", mcpClient.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})

	foundResolved := false
	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
		}
		if payload.ToolName != "mcp.github.create_issue" ||
			payload.RuleID != "ask-github-create" ||
			payload.Decision != "deny" ||
			payload.ResolvedAs != "rejected" ||
			payload.RememberScope != string(tools.SessionPermissionScopeReject) {
			t.Fatalf("unexpected permission resolved payload: %+v", payload)
		}
		foundResolved = true
	}
	if !foundResolved {
		t.Fatalf("expected permission resolved event")
	}
}

func TestServiceRunMCPPermissionHardDenyFlow(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	mcpClient := &runtimeStubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create issue", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{Content: "should-not-run"},
	}
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", mcpClient); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewPolicyEngine(security.DecisionAllow, []security.PolicyRule{
		{
			ID:             "deny-github-server",
			Priority:       830,
			Decision:       security.DecisionDeny,
			Reason:         "github mcp server denied",
			ActionTypes:    []security.ActionType{security.ActionTypeMCP},
			ToolCategories: []string{"mcp.github"},
			TargetTypes:    []security.TargetType{security.TargetTypeMCP},
		},
		{
			ID:               "ask-github-create",
			Priority:         720,
			Decision:         security.DecisionAsk,
			Reason:           "mcp create requires approval",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.github.create_issue"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
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
						{ID: "call-mcp-deny", Name: "mcp.github.create_issue", Arguments: `{"title":"hello"}`},
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
	if err := service.Run(context.Background(), UserInput{RunID: "run-mcp-permission-deny", Content: "create issue"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if mcpClient.callCount != 0 {
		t.Fatalf("expected hard denied MCP tool not to execute, got %d", mcpClient.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})
	assertNoPermissionRequestFlow(t, events)

	foundResolved := false
	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
		}
		if payload.ToolName != "mcp.github.create_issue" ||
			payload.RuleID != "deny-github-server" ||
			payload.Reason != "github mcp server denied" ||
			payload.Decision != "deny" ||
			payload.ResolvedAs != "denied" ||
			payload.RememberScope != "" {
			t.Fatalf("unexpected permission resolved payload: %+v", payload)
		}
		foundResolved = true
	}
	if !foundResolved {
		t.Fatalf("expected permission resolved event")
	}
}

func TestPermissionHelpers(t *testing.T) {
	t.Parallel()

	if scope, err := rememberScopeFromDecision(approvalflow.DecisionAllowOnce); err != nil || scope != tools.SessionPermissionScopeOnce {
		t.Fatalf("expected once scope, got %q / %v", scope, err)
	}
	if scope, err := rememberScopeFromDecision(approvalflow.DecisionAllowSession); err != nil || scope != tools.SessionPermissionScopeAlways {
		t.Fatalf("expected always scope, got %q / %v", scope, err)
	}
	if scope, err := rememberScopeFromDecision(approvalflow.DecisionReject); err != nil || scope != tools.SessionPermissionScopeReject {
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
	if category != permissionToolCategoryFilesystemRead {
		t.Fatalf("expected %s category, got %q", permissionToolCategoryFilesystemRead, category)
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

	category = permissionToolCategory(security.Action{
		Type: security.ActionTypeMCP,
		Payload: security.ActionPayload{
			Target: "mcp.github.enterprise.create_issue",
		},
	})
	if category != "mcp.github.enterprise" {
		t.Fatalf("expected mcp.github.enterprise category, got %q", category)
	}

	category = permissionToolCategory(security.Action{
		Type: security.ActionTypeMCP,
		Payload: security.ActionPayload{
			Target: "mcp",
		},
	})
	if category != permissionToolCategoryMCP {
		t.Fatalf("expected %s fallback category, got %q", permissionToolCategoryMCP, category)
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
	requestID, resultCh, err := service.approvalBroker.Open()
	if err != nil {
		t.Fatalf("open approval request: %v", err)
	}
	defer service.approvalBroker.Close(requestID)
	resultCh <- approvalflow.DecisionAllowOnce

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.ResolvePermission(ctx, PermissionResolutionInput{
		RequestID: requestID,
		Decision:  approvalflow.DecisionAllowOnce,
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
	defer cancel()
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
