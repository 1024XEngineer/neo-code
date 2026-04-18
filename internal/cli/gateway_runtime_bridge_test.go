package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/gateway"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type runtimeStub struct {
	submitInput     agentruntime.PrepareInput
	submitErr       error
	compactInput    agentruntime.CompactInput
	compactResult   agentruntime.CompactResult
	compactErr      error
	permissionInput agentruntime.PermissionResolutionInput
	permissionErr   error
	cancelReturn    bool
	eventsCh        chan agentruntime.RuntimeEvent
	sessionList     []agentsession.Summary
	listErr         error
	loadID          string
	loadSession     agentsession.Session
	loadErr         error
}

func (s *runtimeStub) Submit(_ context.Context, input agentruntime.PrepareInput) error {
	s.submitInput = input
	return s.submitErr
}

func (s *runtimeStub) PrepareUserInput(context.Context, agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	return agentruntime.UserInput{}, nil
}

func (s *runtimeStub) Run(context.Context, agentruntime.UserInput) error {
	return nil
}

func (s *runtimeStub) Compact(_ context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	s.compactInput = input
	return s.compactResult, s.compactErr
}

func (s *runtimeStub) ExecuteSystemTool(context.Context, agentruntime.SystemToolInput) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (s *runtimeStub) ResolvePermission(_ context.Context, input agentruntime.PermissionResolutionInput) error {
	s.permissionInput = input
	return s.permissionErr
}

func (s *runtimeStub) CancelActiveRun() bool {
	return s.cancelReturn
}

func (s *runtimeStub) Events() <-chan agentruntime.RuntimeEvent {
	return s.eventsCh
}

func (s *runtimeStub) ListSessions(context.Context) ([]agentsession.Summary, error) {
	return s.sessionList, s.listErr
}

func (s *runtimeStub) LoadSession(_ context.Context, id string) (agentsession.Session, error) {
	s.loadID = id
	return s.loadSession, s.loadErr
}

func (s *runtimeStub) ActivateSessionSkill(context.Context, string, string) error {
	return nil
}

func (s *runtimeStub) DeactivateSessionSkill(context.Context, string, string) error {
	return nil
}

func (s *runtimeStub) ListSessionSkills(context.Context, string) ([]agentruntime.SessionSkillState, error) {
	return nil, nil
}

func TestNewGatewayRuntimePortBridgeRuntimeUnavailable(t *testing.T) {
	bridge, err := newGatewayRuntimePortBridge(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}
	if bridge != nil {
		t.Fatal("expected nil bridge when runtime is nil")
	}

	var nilBridge *gatewayRuntimePortBridge
	if err := nilBridge.Run(context.Background(), gateway.RunInput{}); err == nil {
		t.Fatal("expected run error for nil bridge")
	}
	if _, err := nilBridge.Compact(context.Background(), gateway.CompactInput{}); err == nil {
		t.Fatal("expected compact error for nil bridge")
	}
	if err := nilBridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{}); err == nil {
		t.Fatal("expected resolve_permission error for nil bridge")
	}
	if nilBridge.CancelActiveRun() {
		t.Fatal("cancel_active_run should be false for nil bridge")
	}
	if nilBridge.Events() != nil {
		t.Fatal("events channel should be nil for nil bridge")
	}
	if _, err := nilBridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error for nil bridge")
	}
	if _, err := nilBridge.LoadSession(context.Background(), "s-1"); err == nil {
		t.Fatal("expected load_session error for nil bridge")
	}
	if err := nilBridge.Close(); err != nil {
		t.Fatalf("close nil bridge: %v", err)
	}
}

func TestGatewayRuntimePortBridgeRuntimeMethods(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		cancelReturn: true,
		compactResult: agentruntime.CompactResult{
			Applied:        true,
			BeforeChars:    200,
			AfterChars:     100,
			SavedRatio:     0.5,
			TriggerMode:    "manual",
			TranscriptID:   "tx-1",
			TranscriptPath: "/tmp/tx-1.md",
		},
		sessionList: []agentsession.Summary{
			{
				ID:        "  session-1  ",
				Title:     "  title  ",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		loadSession: agentsession.Session{
			ID:        "  session-1  ",
			Title:     "  title  ",
			Workdir:   "  /tmp/work  ",
			CreatedAt: now,
			UpdatedAt: now,
			Messages: []providertypes.Message{
				{
					Role: " assistant ",
					Parts: []providertypes.ContentPart{
						{Kind: providertypes.ContentPartText, Text: "  hello  "},
						{Kind: providertypes.ContentPartImage},
					},
					ToolCalls: []providertypes.ToolCall{
						{ID: " tc-1 ", Name: " bash ", Arguments: `{"cmd":"pwd"}`},
					},
					ToolCallID: " call-1 ",
					IsError:    true,
				},
			},
		},
	}

	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer func() {
		if closeErr := bridge.Close(); closeErr != nil {
			t.Fatalf("close bridge: %v", closeErr)
		}
	}()

	runInput := gateway.RunInput{
		RequestID: " request-1 ",
		SessionID: " session-1 ",
		RunID:     " run-1 ",
		InputText: " base ",
		InputParts: []gateway.InputPart{
			{Type: gateway.InputPartTypeText, Text: " extra "},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: " /tmp/a.png ", MimeType: " image/png "}},
		},
		Workdir: " /tmp/work ",
	}
	if err := bridge.Run(context.Background(), runInput); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stub.submitInput.SessionID != "session-1" {
		t.Fatalf("submit session_id = %q, want %q", stub.submitInput.SessionID, "session-1")
	}
	if stub.submitInput.RunID != "run-1" {
		t.Fatalf("submit run_id = %q, want %q", stub.submitInput.RunID, "run-1")
	}
	if stub.submitInput.Workdir != "/tmp/work" {
		t.Fatalf("submit workdir = %q, want %q", stub.submitInput.Workdir, "/tmp/work")
	}
	if stub.submitInput.Text != "base\nextra" {
		t.Fatalf("submit text = %q, want %q", stub.submitInput.Text, "base\nextra")
	}
	if len(stub.submitInput.Images) != 1 || stub.submitInput.Images[0].Path != "/tmp/a.png" || stub.submitInput.Images[0].MimeType != "image/png" {
		t.Fatalf("submit images = %#v, want single image with trimmed path/mime", stub.submitInput.Images)
	}

	compactResult, err := bridge.Compact(context.Background(), gateway.CompactInput{
		SessionID: " session-1 ",
		RunID:     " run-1 ",
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if stub.compactInput.SessionID != "session-1" || stub.compactInput.RunID != "run-1" {
		t.Fatalf("compact input = %#v, want trimmed session/run ids", stub.compactInput)
	}
	if !compactResult.Applied || compactResult.BeforeChars != 200 || compactResult.AfterChars != 100 || compactResult.SavedRatio != 0.5 {
		t.Fatalf("compact result = %#v", compactResult)
	}

	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{
		RequestID: " request-1 ",
		Decision:  gateway.PermissionResolutionAllowSession,
	}); err != nil {
		t.Fatalf("resolve_permission: %v", err)
	}
	if stub.permissionInput.RequestID != "request-1" || string(stub.permissionInput.Decision) != "allow_session" {
		t.Fatalf("permission input = %#v, want trimmed request id and allow_session", stub.permissionInput)
	}

	if !bridge.CancelActiveRun() {
		t.Fatal("cancel_active_run should return stub value true")
	}

	sessions, err := bridge.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list_sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" || sessions[0].Title != "title" {
		t.Fatalf("sessions = %#v, want one trimmed session summary", sessions)
	}

	stub.sessionList = nil
	emptySessions, err := bridge.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list empty sessions: %v", err)
	}
	if emptySessions != nil {
		t.Fatalf("empty session list = %#v, want nil", emptySessions)
	}

	session, err := bridge.LoadSession(context.Background(), " session-1 ")
	if err != nil {
		t.Fatalf("load_session: %v", err)
	}
	if stub.loadID != "session-1" {
		t.Fatalf("load id = %q, want %q", stub.loadID, "session-1")
	}
	if session.ID != "session-1" || session.Title != "title" || session.Workdir != "/tmp/work" {
		t.Fatalf("loaded session = %#v, want trimmed fields", session)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("session messages len = %d, want 1", len(session.Messages))
	}
	if session.Messages[0].Content != "hello\n[image]" {
		t.Fatalf("rendered message content = %q, want %q", session.Messages[0].Content, "hello\n[image]")
	}
	if len(session.Messages[0].ToolCalls) != 1 || session.Messages[0].ToolCalls[0].Name != "bash" {
		t.Fatalf("message tool calls = %#v, want trimmed tool call", session.Messages[0].ToolCalls)
	}
}

func TestGatewayRuntimePortBridgeRuntimeMethodErrors(t *testing.T) {
	stub := &runtimeStub{
		submitErr:     errors.New("submit failed"),
		compactErr:    errors.New("compact failed"),
		permissionErr: errors.New("permission failed"),
		listErr:       errors.New("list failed"),
		loadErr:       errors.New("load failed"),
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	if err := bridge.Run(context.Background(), gateway.RunInput{}); err == nil {
		t.Fatal("expected run error from runtime")
	}
	if _, err := bridge.Compact(context.Background(), gateway.CompactInput{}); err == nil {
		t.Fatal("expected compact error from runtime")
	}
	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{}); err == nil {
		t.Fatal("expected resolve_permission error from runtime")
	}
	if _, err := bridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error from runtime")
	}
	if _, err := bridge.LoadSession(context.Background(), "s-1"); err == nil {
		t.Fatal("expected load_session error from runtime")
	}
}

func TestGatewayRuntimePortBridgeRunEventBridge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := make(chan agentruntime.RuntimeEvent, 3)
	stub := &runtimeStub{eventsCh: source}
	bridge, err := newGatewayRuntimePortBridge(ctx, stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	source <- agentruntime.RuntimeEvent{
		Type:           agentruntime.EventAgentChunk,
		RunID:          " run-1 ",
		SessionID:      " session-1 ",
		Turn:           3,
		Phase:          " thinking ",
		PayloadVersion: 2,
		Payload:        map[string]any{"k": "v"},
	}
	source <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentDone, RunID: "run-1", SessionID: "session-1"}
	source <- agentruntime.RuntimeEvent{Type: agentruntime.EventError, RunID: "run-1", SessionID: "session-1"}
	close(source)

	events := make([]gateway.RuntimeEvent, 0, 3)
	for event := range bridge.Events() {
		events = append(events, event)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[0].Type != gateway.RuntimeEventTypeRunProgress {
		t.Fatalf("event[0] type = %q, want %q", events[0].Type, gateway.RuntimeEventTypeRunProgress)
	}
	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("event payload type = %T, want map[string]any", events[0].Payload)
	}
	if payload["runtime_event_type"] != string(agentruntime.EventAgentChunk) {
		t.Fatalf("runtime_event_type = %#v, want %q", payload["runtime_event_type"], agentruntime.EventAgentChunk)
	}
	if payload["phase"] != "thinking" {
		t.Fatalf("payload phase = %#v, want %q", payload["phase"], "thinking")
	}
	if events[1].Type != gateway.RuntimeEventTypeRunDone {
		t.Fatalf("event[1] type = %q, want %q", events[1].Type, gateway.RuntimeEventTypeRunDone)
	}
	if events[2].Type != gateway.RuntimeEventTypeRunError {
		t.Fatalf("event[2] type = %q, want %q", events[2].Type, gateway.RuntimeEventTypeRunError)
	}
}

func TestGatewayRuntimePortBridgeStopsOnCloseAndContextCancel(t *testing.T) {
	source := make(chan agentruntime.RuntimeEvent)
	stub := &runtimeStub{eventsCh: source}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatalf("close bridge: %v", err)
	}
	select {
	case _, ok := <-bridge.Events():
		if ok {
			t.Fatal("events should be closed after bridge close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed events after bridge close")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelBridge, err := newGatewayRuntimePortBridge(cancelCtx, &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent)})
	if err != nil {
		t.Fatalf("new cancel bridge: %v", err)
	}
	select {
	case _, ok := <-cancelBridge.Events():
		if ok {
			t.Fatal("events should be closed when context is canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed events after context cancel")
	}

	nilCtxBridge, err := newGatewayRuntimePortBridge(nil, &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent)})
	if err != nil {
		t.Fatalf("new nil-ctx bridge: %v", err)
	}
	if err := nilCtxBridge.Close(); err != nil {
		t.Fatalf("close nil-ctx bridge: %v", err)
	}
}

func TestConvertGatewayRunInputAndSessionHelpers(t *testing.T) {
	converted := convertGatewayRunInput(gateway.RunInput{
		RequestID: " req-1 ",
		SessionID: " session-1 ",
		InputText: " base ",
		InputParts: []gateway.InputPart{
			{Type: gateway.InputPartTypeText, Text: "  text  "},
			{Type: gateway.InputPartTypeImage, Media: nil},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: "   "}},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: " /tmp/a.png ", MimeType: " image/png "}},
		},
		Workdir: " /tmp/work ",
	})
	if converted.RunID != "req-1" {
		t.Fatalf("run_id = %q, want request id fallback %q", converted.RunID, "req-1")
	}
	if converted.Text != "base\ntext" {
		t.Fatalf("text = %q, want %q", converted.Text, "base\ntext")
	}
	if len(converted.Images) != 1 || converted.Images[0].Path != "/tmp/a.png" {
		t.Fatalf("images = %#v, want one valid image", converted.Images)
	}

	if got := renderSessionMessageContent(nil); got != "" {
		t.Fatalf("render nil parts = %q, want empty", got)
	}
	parts := []providertypes.ContentPart{
		{Kind: providertypes.ContentPartText, Text: "  "},
		{Kind: providertypes.ContentPartText, Text: " line "},
		{Kind: providertypes.ContentPartImage},
	}
	if got := renderSessionMessageContent(parts); got != "line\n[image]" {
		t.Fatalf("rendered parts = %q, want %q", got, "line\n[image]")
	}

	if messages := convertSessionMessages(nil); messages != nil {
		t.Fatalf("convert nil messages = %#v, want nil", messages)
	}
}
