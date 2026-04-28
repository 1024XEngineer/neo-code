package cli

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"neo-code/internal/gateway"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

type runtimeStub struct {
	submitInput     agentruntime.PrepareInput
	submitErr       error
	compactInput    agentruntime.CompactInput
	compactResult   agentruntime.CompactResult
	compactErr      error
	systemToolInput agentruntime.SystemToolInput
	systemToolRes   tools.ToolResult
	systemToolErr   error
	permissionInput agentruntime.PermissionResolutionInput
	permissionErr   error
	activateSession struct {
		sessionID string
		skillID   string
	}
	activateSessionErr error
	deactivateSession  struct {
		sessionID string
		skillID   string
	}
	deactivateSessionErr  error
	sessionSkills         []agentruntime.SessionSkillState
	sessionSkillsErr      error
	listSessionSkillsID   string
	availableSkills       []agentruntime.AvailableSkillState
	availableSkillsErr    error
	listAvailableSkillsID string
	cancelReturn          bool
	eventsCh              chan agentruntime.RuntimeEvent
	sessionList           []agentsession.Summary
	listErr               error
	loadID                string
	loadSession           agentsession.Session
	loadErr               error
	createID              string
	createSession         agentsession.Session
	createErr             error
}

const testBridgeSubjectID = bridgeLocalSubjectID

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

func (s *runtimeStub) ExecuteSystemTool(_ context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	s.systemToolInput = input
	return s.systemToolRes, s.systemToolErr
}

func (s *runtimeStub) ResolvePermission(_ context.Context, input agentruntime.PermissionResolutionInput) error {
	s.permissionInput = input
	return s.permissionErr
}

func (s *runtimeStub) CancelActiveRun() bool {
	return s.cancelReturn
}

func (s *runtimeStub) CancelRun(string) bool {
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

func (s *runtimeStub) CreateSession(_ context.Context, id string) (agentsession.Session, error) {
	s.createID = id
	return s.createSession, s.createErr
}

func (s *runtimeStub) ActivateSessionSkill(_ context.Context, sessionID string, skillID string) error {
	s.activateSession.sessionID = sessionID
	s.activateSession.skillID = skillID
	return s.activateSessionErr
}

func (s *runtimeStub) DeactivateSessionSkill(_ context.Context, sessionID string, skillID string) error {
	s.deactivateSession.sessionID = sessionID
	s.deactivateSession.skillID = skillID
	return s.deactivateSessionErr
}

func (s *runtimeStub) ListSessionSkills(_ context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	s.listSessionSkillsID = sessionID
	return s.sessionSkills, s.sessionSkillsErr
}

func (s *runtimeStub) ListAvailableSkills(_ context.Context, sessionID string) ([]agentruntime.AvailableSkillState, error) {
	s.listAvailableSkillsID = sessionID
	return s.availableSkills, s.availableSkillsErr
}

type runtimeWithoutCreator struct {
	base *runtimeStub
}

func (r *runtimeWithoutCreator) Submit(ctx context.Context, input agentruntime.PrepareInput) error {
	return r.base.Submit(ctx, input)
}
func (r *runtimeWithoutCreator) PrepareUserInput(ctx context.Context, input agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	return r.base.PrepareUserInput(ctx, input)
}
func (r *runtimeWithoutCreator) Run(ctx context.Context, input agentruntime.UserInput) error {
	return r.base.Run(ctx, input)
}
func (r *runtimeWithoutCreator) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return r.base.Compact(ctx, input)
}
func (r *runtimeWithoutCreator) ExecuteSystemTool(ctx context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	return r.base.ExecuteSystemTool(ctx, input)
}
func (r *runtimeWithoutCreator) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return r.base.ResolvePermission(ctx, input)
}
func (r *runtimeWithoutCreator) CancelActiveRun() bool {
	return r.base.CancelActiveRun()
}
func (r *runtimeWithoutCreator) Events() <-chan agentruntime.RuntimeEvent {
	return r.base.Events()
}
func (r *runtimeWithoutCreator) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return r.base.ListSessions(ctx)
}
func (r *runtimeWithoutCreator) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return r.base.LoadSession(ctx, id)
}
func (r *runtimeWithoutCreator) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return r.base.ActivateSessionSkill(ctx, sessionID, skillID)
}
func (r *runtimeWithoutCreator) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return r.base.DeactivateSessionSkill(ctx, sessionID, skillID)
}
func (r *runtimeWithoutCreator) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return r.base.ListSessionSkills(ctx, sessionID)
}

func (r *runtimeWithoutCreator) ListAvailableSkills(
	ctx context.Context,
	sessionID string,
) ([]agentruntime.AvailableSkillState, error) {
	return r.base.ListAvailableSkills(ctx, sessionID)
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
	if _, err := nilBridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		ToolName:  "memo_list",
	}); err == nil {
		t.Fatal("expected execute_system_tool error for nil bridge")
	}
	if err := nilBridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected activate_session_skill error for nil bridge")
	}
	if err := nilBridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected deactivate_session_skill error for nil bridge")
	}
	if _, err := nilBridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_session_skills error for nil bridge")
	}
	if _, err := nilBridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected list_available_skills error for nil bridge")
	}
	if err := nilBridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{}); err == nil {
		t.Fatal("expected resolve_permission error for nil bridge")
	}
	if _, err := nilBridge.CancelRun(context.Background(), gateway.CancelInput{SubjectID: testBridgeSubjectID, RunID: "run-1"}); err == nil {
		t.Fatal("expected cancel_run error for nil bridge")
	}
	if nilBridge.Events() != nil {
		t.Fatal("events channel should be nil for nil bridge")
	}
	if _, err := nilBridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error for nil bridge")
	}
	if _, err := nilBridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected load_session error for nil bridge")
	}
	if _, err := nilBridge.CreateSession(context.Background(), gateway.CreateSessionInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected create_session error for nil bridge")
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
		systemToolRes: tools.ToolResult{
			ToolCallID: "call-system-1",
			Name:       "memo_list",
			Content:    "memo ok",
		},
		sessionSkills: []agentruntime.SessionSkillState{
			{
				SkillID: "go-review",
				Descriptor: &skills.Descriptor{
					ID:          "go-review",
					Name:        "Go Review",
					Description: "Review Go code",
					Version:     "v1",
					Source: skills.Source{
						Kind: skills.SourceKindLocal,
					},
					Scope: skills.ScopeSession,
				},
			},
			{
				SkillID: "missing-skill",
				Missing: true,
			},
		},
		availableSkills: []agentruntime.AvailableSkillState{
			{
				Descriptor: skills.Descriptor{
					ID:          "go-review",
					Name:        "Go Review",
					Description: "Review Go code",
					Version:     "v1",
					Source: skills.Source{
						Kind: skills.SourceKindLocal,
					},
					Scope: skills.ScopeSession,
				},
				Active: true,
			},
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
		SubjectID: testBridgeSubjectID,
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
		SubjectID: testBridgeSubjectID,
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

	systemToolResult, err := bridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		RunID:     " run-1 ",
		Workdir:   " /tmp/work ",
		ToolName:  " memo_list ",
		Arguments: []byte(`{"limit":10}`),
	})
	if err != nil {
		t.Fatalf("execute_system_tool: %v", err)
	}
	if stub.systemToolInput.SessionID != "session-1" || stub.systemToolInput.RunID != "run-1" {
		t.Fatalf("system tool input ids = %#v, want trimmed session/run ids", stub.systemToolInput)
	}
	if stub.systemToolInput.Workdir != "/tmp/work" || stub.systemToolInput.ToolName != "memo_list" {
		t.Fatalf("system tool input = %#v, want trimmed workdir/tool_name", stub.systemToolInput)
	}
	if string(stub.systemToolInput.Arguments) != `{"limit":10}` {
		t.Fatalf("system tool arguments = %s, want {\"limit\":10}", string(stub.systemToolInput.Arguments))
	}
	if systemToolResult.Content != "memo ok" || systemToolResult.Name != "memo_list" {
		t.Fatalf("system tool result = %#v, want stubbed result", systemToolResult)
	}

	if err := bridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		SkillID:   " go-review ",
	}); err != nil {
		t.Fatalf("activate_session_skill: %v", err)
	}
	if stub.activateSession.sessionID != "session-1" || stub.activateSession.skillID != "go-review" {
		t.Fatalf("activate skill input = %#v, want trimmed session/skill ids", stub.activateSession)
	}

	if err := bridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		SkillID:   " go-review ",
	}); err != nil {
		t.Fatalf("deactivate_session_skill: %v", err)
	}
	if stub.deactivateSession.sessionID != "session-1" || stub.deactivateSession.skillID != "go-review" {
		t.Fatalf("deactivate skill input = %#v, want trimmed session/skill ids", stub.deactivateSession)
	}

	sessionSkills, err := bridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatalf("list_session_skills: %v", err)
	}
	if stub.listSessionSkillsID != "session-1" {
		t.Fatalf("list session skills session_id = %q, want %q", stub.listSessionSkillsID, "session-1")
	}
	if len(sessionSkills) != 2 || sessionSkills[0].SkillID != "go-review" || sessionSkills[1].SkillID != "missing-skill" {
		t.Fatalf("session skills = %#v, want mapped runtime states", sessionSkills)
	}
	if sessionSkills[0].Descriptor == nil || sessionSkills[0].Descriptor.ID != "go-review" {
		t.Fatalf("session skill descriptor = %#v, want go-review descriptor", sessionSkills[0].Descriptor)
	}
	if !sessionSkills[1].Missing {
		t.Fatalf("missing session skill should keep missing=true, got %#v", sessionSkills[1])
	}

	availableSkills, err := bridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatalf("list_available_skills: %v", err)
	}
	if stub.listAvailableSkillsID != "session-1" {
		t.Fatalf("list available skills session_id = %q, want %q", stub.listAvailableSkillsID, "session-1")
	}
	if len(availableSkills) != 1 || availableSkills[0].Descriptor.ID != "go-review" || !availableSkills[0].Active {
		t.Fatalf("available skills = %#v, want one active go-review skill", availableSkills)
	}

	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{
		SubjectID: testBridgeSubjectID,
		RequestID: " request-1 ",
		Decision:  gateway.PermissionResolutionAllowSession,
	}); err != nil {
		t.Fatalf("resolve_permission: %v", err)
	}
	if stub.permissionInput.RequestID != "request-1" || string(stub.permissionInput.Decision) != "allow_session" {
		t.Fatalf("permission input = %#v, want trimmed request id and allow_session", stub.permissionInput)
	}

	canceled, err := bridge.CancelRun(context.Background(), gateway.CancelInput{
		SubjectID: testBridgeSubjectID,
		RunID:     " run-1 ",
	})
	if err != nil {
		t.Fatalf("cancel_run: %v", err)
	}
	if !canceled {
		t.Fatal("cancel_run should return stub value true")
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

	session, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
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

func TestGatewayRuntimePortBridgeLoadSessionNotFoundBranches(t *testing.T) {
	t.Parallel()

	base := &runtimeStub{
		loadErr: agentsession.ErrSessionNotFound,
	}
	bridgeWithoutCreator, err := newGatewayRuntimePortBridge(context.Background(), &runtimeWithoutCreator{base: base})
	if err != nil {
		t.Fatalf("new bridge without creator: %v", err)
	}
	t.Cleanup(func() { _ = bridgeWithoutCreator.Close() })

	if _, err := bridgeWithoutCreator.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); !errors.Is(err, gateway.ErrRuntimeResourceNotFound) {
		t.Fatalf("expected ErrRuntimeResourceNotFound, got %v", err)
	}

	stub := &runtimeStub{
		loadErr:   os.ErrNotExist,
		createErr: errors.New("create failed"),
	}
	bridgeWithCreator, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge with creator: %v", err)
	}
	t.Cleanup(func() { _ = bridgeWithCreator.Close() })

	if _, err := bridgeWithCreator.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-2",
	}); err == nil || err.Error() != "create failed" {
		t.Fatalf("expected create failed error, got %v", err)
	}
}

func TestIsRuntimeNotFoundErrorIncludesOSErrNotExist(t *testing.T) {
	t.Parallel()

	if !isRuntimeNotFoundError(os.ErrNotExist) {
		t.Fatalf("os.ErrNotExist should be treated as runtime not found")
	}
	if !isRuntimeNotFoundError(agentsession.ErrSessionNotFound) {
		t.Fatalf("ErrSessionNotFound should be treated as runtime not found")
	}
	if isRuntimeNotFoundError(errors.New("session not found")) {
		t.Fatalf("plain string not-found error should not be treated as runtime not found")
	}
}

func TestGatewayRuntimePortBridgeRuntimeMethodErrors(t *testing.T) {
	stub := &runtimeStub{
		submitErr:            errors.New("submit failed"),
		compactErr:           errors.New("compact failed"),
		systemToolErr:        errors.New("system tool failed"),
		permissionErr:        errors.New("permission failed"),
		activateSessionErr:   errors.New("activate skill failed"),
		deactivateSessionErr: errors.New("deactivate skill failed"),
		sessionSkillsErr:     errors.New("list session skills failed"),
		availableSkillsErr:   errors.New("list available skills failed"),
		listErr:              errors.New("list failed"),
		loadErr:              errors.New("load failed"),
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	if err := bridge.Run(context.Background(), gateway.RunInput{SubjectID: testBridgeSubjectID}); err == nil {
		t.Fatal("expected run error from runtime")
	}
	if _, err := bridge.Compact(context.Background(), gateway.CompactInput{SubjectID: testBridgeSubjectID}); err == nil {
		t.Fatal("expected compact error from runtime")
	}
	if _, err := bridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		ToolName:  "memo_list",
	}); err == nil {
		t.Fatal("expected execute_system_tool error from runtime")
	}
	if err := bridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected activate_session_skill error from runtime")
	}
	if err := bridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected deactivate_session_skill error from runtime")
	}
	if _, err := bridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_session_skills error from runtime")
	}
	if _, err := bridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_available_skills error from runtime")
	}
	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected resolve_permission error from runtime")
	}
	if _, err := bridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error from runtime")
	}
	if _, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected load_session error from runtime")
	}
}

func TestGatewayRuntimePortBridgeLoadSessionUpsertWhenMissing(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		loadErr: agentsession.ErrSessionNotFound,
		createSession: agentsession.Session{
			ID:        "session-new",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	session, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-new ",
	})
	if err != nil {
		t.Fatalf("load_session upsert: %v", err)
	}
	if stub.loadID != "session-new" {
		t.Fatalf("load id = %q, want %q", stub.loadID, "session-new")
	}
	if stub.createID != "session-new" {
		t.Fatalf("create id = %q, want %q", stub.createID, "session-new")
	}
	if session.ID != "session-new" || session.Title != "New Session" || session.Workdir != "/tmp/work" {
		t.Fatalf("upsert session = %#v, want created session snapshot", session)
	}
}

func TestGatewayRuntimePortBridgeLoadSessionNoUpsertOnPlainStringNotFoundError(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		loadErr: errors.New("open sessions/session-new.json: no such file"),
		createSession: agentsession.Session{
			ID:        "session-new",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	_, err = bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-new ",
	})
	if err == nil || err.Error() != "open sessions/session-new.json: no such file" {
		t.Fatalf("expected original string error passthrough, got %v", err)
	}
	if stub.createID != "" {
		t.Fatalf("create should not be called for plain string error, got createID=%q", stub.createID)
	}
}

func TestGatewayRuntimePortBridgeCreateSession(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		createSession: agentsession.Session{
			ID:        "session-created",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	sessionID, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-created ",
	})
	if err != nil {
		t.Fatalf("create_session: %v", err)
	}
	if stub.createID != "session-created" {
		t.Fatalf("create id = %q, want %q", stub.createID, "session-created")
	}
	if sessionID != "session-created" {
		t.Fatalf("session_id = %q, want %q", sessionID, "session-created")
	}
}

func TestGatewayRuntimePortBridgeCreateSessionBranches(t *testing.T) {
	t.Run("subject denied", func(t *testing.T) {
		stub := &runtimeStub{}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		if _, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
			SubjectID: "external_subject",
		}); !errors.Is(err, gateway.ErrRuntimeAccessDenied) {
			t.Fatalf("expected ErrRuntimeAccessDenied, got %v", err)
		}
	})

	t.Run("runtime no creator", func(t *testing.T) {
		base := &runtimeStub{}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), &runtimeWithoutCreator{base: base})
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		if _, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
			SubjectID: testBridgeSubjectID,
		}); err == nil {
			t.Fatal("expected runtime creator unavailable error")
		}
	})
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
