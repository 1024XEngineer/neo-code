package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// stubAskUserBroker implements AskUserBroker for tests.
type stubAskUserBroker struct {
	requestID string
	result    AskUserResult
	err       error
	lastReq   AskUserRequest
}

func (s *stubAskUserBroker) Open(ctx context.Context, request AskUserRequest) (string, AskUserResult, error) {
	s.lastReq = request
	return s.requestID, s.result, s.err
}

func TestNewAskUserToolDefaults(t *testing.T) {
	t.Parallel()

	tool := NewAskUserTool(nil)
	if tool.Name() != ToolNameAskUser {
		t.Fatalf("expected name %q, got %q", ToolNameAskUser, tool.Name())
	}
	if !strings.Contains(tool.Description(), "plan mode") {
		t.Fatalf("expected description mentioning plan mode, got %q", tool.Description())
	}
	schema := tool.Schema()
	if _, ok := schema["properties"]; !ok {
		t.Fatalf("expected schema with properties")
	}
	if tool.MicroCompactPolicy() != MicroCompactPolicyPreserveHistory {
		t.Fatalf("expected PreserveHistory policy, got %v", tool.MicroCompactPolicy())
	}
}

func TestAskUserToolSchemaHasRequiredFields(t *testing.T) {
	t.Parallel()

	tool := NewAskUserTool(nil)
	schema := tool.Schema()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required array in schema, got %T", schema["required"])
	}
	has := make(map[string]bool, len(required))
	for _, r := range required {
		has[r] = true
	}
	for _, field := range []string{"question_id", "title", "kind"} {
		if !has[field] {
			t.Fatalf("expected %q in required fields, got %v", field, required)
		}
	}
}

func TestAskUserToolExecuteNilBroker(t *testing.T) {
	t.Parallel()

	tool := NewAskUserTool(nil)
	_, err := tool.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "broker is nil") {
		t.Fatalf("expected broker nil error, got %v", err)
	}
}

func TestAskUserToolExecuteInvalidArgs(t *testing.T) {
	t.Parallel()

	tool := NewAskUserTool(&stubAskUserBroker{})

	tests := []struct {
		name    string
		args    string
		wantErr string
	}{
		{"missing question_id", `{"title":"T","kind":"text"}`, "question_id is required"},
		{"missing title", `{"question_id":"q1","kind":"text"}`, "title is required"},
		{"invalid kind", `{"question_id":"q1","title":"T","kind":"bogus"}`, "kind must be"},
		{"empty json", `{}`, "question_id is required"},
		{"malformed json", `{bad`, "invalid ask_user arguments"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tool.Execute(context.Background(), ToolCallInput{
				ID:        "call-1",
				Name:      ToolNameAskUser,
				Arguments: []byte(tt.args),
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAskUserToolExecuteHappyPath(t *testing.T) {
	t.Parallel()

	broker := &stubAskUserBroker{
		requestID: "ask-1",
		result: AskUserResult{
			Status:     "answered",
			QuestionID: "q1",
			Values:     []string{"opt-a"},
		},
	}
	tool := NewAskUserTool(broker)

	var emittedEvents []string
	var emittedPayloads []any
	emitter := func(eventName string, payload any) {
		emittedEvents = append(emittedEvents, eventName)
		emittedPayloads = append(emittedPayloads, payload)
	}

	result, err := tool.Execute(context.Background(), ToolCallInput{
		ID:                 "call-1",
		Name:               ToolNameAskUser,
		Arguments:          []byte(`{"question_id":"q1","title":"Pick one?","kind":"single_choice","options":[{"label":"A"},{"label":"B"}]}`),
		AskUserEventEmitter: emitter,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %s", result.Content)
	}

	// Verify events emitted in order
	if len(emittedEvents) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(emittedEvents), emittedEvents)
	}
	if emittedEvents[0] != "user_question_requested" {
		t.Fatalf("expected first event user_question_requested, got %q", emittedEvents[0])
	}
	if emittedEvents[1] != "user_question_answered" {
		t.Fatalf("expected second event user_question_answered, got %q", emittedEvents[1])
	}

	// Verify result content
	var parsed AskUserResult
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed.Status != "answered" || parsed.QuestionID != "q1" {
		t.Fatalf("unexpected result: %+v", parsed)
	}

	// Verify broker received correct request
	if broker.lastReq.QuestionID != "q1" {
		t.Fatalf("expected question_id q1, got %q", broker.lastReq.QuestionID)
	}
	if broker.lastReq.Kind != "single_choice" {
		t.Fatalf("expected kind single_choice, got %q", broker.lastReq.Kind)
	}
}

func TestAskUserToolExecuteTimeoutDefault(t *testing.T) {
	t.Parallel()

	broker := &stubAskUserBroker{
		requestID: "ask-1",
		result:    AskUserResult{Status: "timeout", QuestionID: "q1", Message: "timed out"},
		err:       errors.New("timed out"),
	}
	tool := NewAskUserTool(broker)

	result, err := tool.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	var parsed AskUserResult
	if jsonErr := json.Unmarshal([]byte(result.Content), &parsed); jsonErr != nil {
		t.Fatalf("unmarshal result: %v", jsonErr)
	}
	if parsed.Status != "timeout" {
		t.Fatalf("expected timeout status, got %q", parsed.Status)
	}
}

func TestAskUserToolExecuteWithoutEmitter(t *testing.T) {
	t.Parallel()

	broker := &stubAskUserBroker{
		requestID: "ask-1",
		result:    AskUserResult{Status: "skipped", QuestionID: "q1"},
	}
	tool := NewAskUserTool(broker)

	// Should not panic when emitter is nil
	result, err := tool.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Arguments: []byte(`{"question_id":"q1","title":"Skip me?","kind":"text","allow_skip":true}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %s", result.Content)
	}
}

func TestAskUserToolSetBroker(t *testing.T) {
	t.Parallel()

	tool := NewAskUserTool(nil)
	_, err := tool.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if err == nil {
		t.Fatalf("expected broker nil error before setting broker")
	}

	setter, ok := tool.(AskUserBrokerSetter)
	if !ok {
		t.Fatalf("expected tool to implement AskUserBrokerSetter")
	}
	broker := &stubAskUserBroker{
		requestID: "ask-2",
		result:    AskUserResult{Status: "answered", QuestionID: "q1", Values: []string{"yes"}},
	}
	setter.SetAskUserBroker(broker)

	result, err := tool.Execute(context.Background(), ToolCallInput{
		ID:        "call-2",
		Name:      ToolNameAskUser,
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error after setting broker: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestParseAskUserRequestDefaults(t *testing.T) {
	t.Parallel()

	t.Run("timeout defaults to 300", func(t *testing.T) {
		t.Parallel()
		req, err := parseAskUserRequest([]byte(`{"question_id":"q1","title":"T","kind":"text"}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if req.TimeoutSec != 300 {
			t.Fatalf("expected default timeout 300, got %d", req.TimeoutSec)
		}
	})

	t.Run("timeout capped at 3600", func(t *testing.T) {
		t.Parallel()
		req, err := parseAskUserRequest([]byte(`{"question_id":"q1","title":"T","kind":"text","timeout_sec":7200}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if req.TimeoutSec != 3600 {
			t.Fatalf("expected capped timeout 3600, got %d", req.TimeoutSec)
		}
	})

	t.Run("negative max_choices clamped", func(t *testing.T) {
		t.Parallel()
		req, err := parseAskUserRequest([]byte(`{"question_id":"q1","title":"T","kind":"multi_choice","max_choices":-5}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if req.MaxChoices != 0 {
			t.Fatalf("expected max_choices clamped to 0, got %d", req.MaxChoices)
		}
	})

	t.Run("kind normalized to lowercase", func(t *testing.T) {
		t.Parallel()
		req, err := parseAskUserRequest([]byte(`{"question_id":"q1","title":"T","kind":"TEXT"}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if req.Kind != "text" {
			t.Fatalf("expected kind normalized to text, got %q", req.Kind)
		}
	})
}

func TestAskUserToolVisibleInPlanMode(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	tool := NewAskUserTool(&stubAskUserBroker{requestID: "a", result: AskUserResult{Status: "answered"}})
	registry.Register(tool)

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	specs, err := manager.ListAvailableSpecs(context.Background(), SpecListInput{
		SessionID: "s-1",
		Mode:      "plan",
	})
	if err != nil {
		t.Fatalf("list specs: %v", err)
	}
	found := false
	for _, spec := range specs {
		if spec.Name == ToolNameAskUser {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ask_user to be visible in plan mode")
	}
}

func TestAskUserToolHiddenInBuildMode(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	tool := NewAskUserTool(&stubAskUserBroker{requestID: "a", result: AskUserResult{Status: "answered"}})
	registry.Register(tool)

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	specs, err := manager.ListAvailableSpecs(context.Background(), SpecListInput{
		SessionID: "s-1",
		Mode:      "build",
	})
	if err != nil {
		t.Fatalf("list specs: %v", err)
	}
	for _, spec := range specs {
		if spec.Name == ToolNameAskUser {
			t.Fatal("expected ask_user to be hidden in build mode")
		}
	}
}

func TestAskUserToolExecuteBlockedInNonPlanMode(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	tool := NewAskUserTool(&stubAskUserBroker{
		requestID: "ask-1",
		result:    AskUserResult{Status: "answered"},
	})
	registry.Register(tool)

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_, execErr := manager.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Mode:      "build",
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if execErr == nil || !strings.Contains(execErr.Error(), errAskUserNotAvailableInCurrentMode) {
		t.Fatalf("expected mode guard error, got %v", execErr)
	}
}

func TestAskUserToolExecutesInPlanMode(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	broker := &stubAskUserBroker{
		requestID: "ask-1",
		result:    AskUserResult{Status: "answered", QuestionID: "q1", Values: []string{"yes"}},
	}
	tool := NewAskUserTool(broker)
	registry.Register(tool)

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	result, execErr := manager.Execute(context.Background(), ToolCallInput{
		ID:        "call-1",
		Name:      ToolNameAskUser,
		Mode:      "plan",
		Arguments: []byte(`{"question_id":"q1","title":"Test?","kind":"text"}`),
	})
	if execErr != nil {
		t.Fatalf("expected no error in plan mode, got %v", execErr)
	}
	if result.IsError {
		t.Fatalf("expected success: %s", result.Content)
	}
}

func TestIsPlanModeOnlyTool(t *testing.T) {
	t.Parallel()

	if !isPlanModeOnlyTool(ToolNameAskUser) {
		t.Fatal("expected ask_user to be plan mode only")
	}
	if isPlanModeOnlyTool("bash") {
		t.Fatal("expected bash not to be plan mode only")
	}
	if isPlanModeOnlyTool("") {
		t.Fatal("expected empty name not to be plan mode only")
	}
}

func TestAskUserToolVisibleInReadOnlyMode(t *testing.T) {
	t.Parallel()

	// ask_user is in isReadOnlyVisibleTool list, so it should appear in ReadOnly mode
	registry := NewRegistry()
	tool := NewAskUserTool(&stubAskUserBroker{requestID: "a", result: AskUserResult{Status: "answered"}})
	registry.Register(tool)

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	specs, err := manager.ListAvailableSpecs(context.Background(), SpecListInput{
		SessionID: "s-1",
		Mode:      "plan",
		ReadOnly:  true,
	})
	if err != nil {
		t.Fatalf("list specs: %v", err)
	}
	found := false
	for _, spec := range specs {
		if spec.Name == ToolNameAskUser {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ask_user to be visible in read-only plan mode")
	}
}
