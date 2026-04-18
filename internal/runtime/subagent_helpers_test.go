package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/subagent"
)

func TestSubAgentEngineHelperFunctions(t *testing.T) {
	t.Parallel()

	assistant := ensureAssistantRole(providertypes.Message{})
	if assistant.Role != providertypes.RoleAssistant {
		t.Fatalf("role = %q, want assistant", assistant.Role)
	}
	explicit := ensureAssistantRole(providertypes.Message{Role: providertypes.RoleUser})
	if explicit.Role != providertypes.RoleUser {
		t.Fatalf("existing role should be preserved")
	}

	if got := resolveSubAgentMaxTurns(0); got != subAgentMaxStepTurnsDefault {
		t.Fatalf("resolveSubAgentMaxTurns(0) = %d", got)
	}
	if got := resolveSubAgentMaxTurns(99); got != subAgentMaxStepTurnsLimit {
		t.Fatalf("resolveSubAgentMaxTurns(99) = %d", got)
	}
	if got := resolveSubAgentMaxTurns(3); got != 3 {
		t.Fatalf("resolveSubAgentMaxTurns(3) = %d", got)
	}

	if got := effectiveMaxToolCallsPerStep(0); got != subAgentMaxStepTurnsDefault {
		t.Fatalf("effectiveMaxToolCallsPerStep(0) = %d", got)
	}
	if got := effectiveMaxToolCallsPerStep(2); got != 2 {
		t.Fatalf("effectiveMaxToolCallsPerStep(2) = %d", got)
	}

	allowlist := normalizeToolAllowlist([]string{" Bash ", "bash", "filesystem_read_file"})
	if len(allowlist) != 2 {
		t.Fatalf("normalizeToolAllowlist size = %d, want 2", len(allowlist))
	}
	if toolAllowed(nil, "bash") {
		t.Fatalf("empty allowlist should deny")
	}
	if !toolAllowed(allowlist, "BASH") {
		t.Fatalf("allowlist should match case-insensitive")
	}

	call := normalizeSubAgentToolCall(providertypes.ToolCall{Name: " bash ", Arguments: " {}", ID: ""}, 1)
	if call.ID == "" || call.Name != "bash" || call.Arguments != "{}" {
		t.Fatalf("normalizeSubAgentToolCall() = %+v", call)
	}

	if !isRecoverableSubAgentToolError(nil) {
		t.Fatalf("nil error should be recoverable")
	}
	if isRecoverableSubAgentToolError(errors.New("boom")) {
		t.Fatalf("generic error should not be recoverable")
	}
	if !isRecoverableSubAgentToolError(permissionDecisionDenyError(t)) {
		t.Fatalf("permission decision error should be recoverable")
	}
}

func TestBuildSubAgentInitialMessagesAndOutputParserEdges(t *testing.T) {
	t.Parallel()

	messages := buildSubAgentInitialMessages(subagent.StepInput{
		Task: subagent.Task{
			ID:             "task-init",
			Goal:           "goal",
			ExpectedOutput: "expected",
			ContextSlice: subagent.TaskContextSlice{
				TaskID: "task-init",
				Goal:   "context",
			},
		},
		Workdir: "/tmp/workdir",
		Trace:   []string{"  one ", "", "two"},
	})
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if text := messages[0].Parts[0].Text; text == "" {
		t.Fatalf("expected non-empty initial message")
	}

	if _, err := extractSubAgentJSONObject("{\"summary\":"); err == nil {
		t.Fatalf("expected incomplete json error")
	}
	if _, err := extractSubAgentJSONObject("no json"); err == nil {
		t.Fatalf("expected missing json error")
	}
}

func TestRuntimeSubAgentResolveSettingsAndToolExecutorEdges(t *testing.T) {
	t.Parallel()

	engine := runtimeSubAgentEngine{}
	if _, _, _, err := engine.resolveSettings(); err == nil || !errors.Is(err, errSubAgentRuntimeUnavailable) {
		t.Fatalf("expected runtime unavailable error, got %v", err)
	}

	service := &Service{configManager: newRuntimeConfigManager(t)}
	engine = runtimeSubAgentEngine{service: service}
	if _, _, _, err := engine.resolveSettings(); err == nil || !errors.Is(err, errSubAgentRuntimeUnavailable) {
		t.Fatalf("expected provider factory unavailable error, got %v", err)
	}

	executor := newSubAgentRuntimeToolExecutor(nil)
	if _, err := executor.ListToolSpecs(context.Background(), subagent.ToolSpecListInput{}); err == nil {
		t.Fatalf("expected unavailable executor error")
	}
}

func TestRuntimeSubAgentGenerateStepMessageError(t *testing.T) {
	t.Parallel()

	engine := runtimeSubAgentEngine{}
	outcome, err := engine.generateStepMessage(
		context.Background(),
		&scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				_ = ctx
				_ = req
				_ = events
				return errors.New("provider error")
			},
		},
		"model",
		"prompt",
		nil,
		nil,
	)
	if err == nil || outcome.err != nil {
		t.Fatalf("expected wrapped provider error, outcome=%+v err=%v", outcome, err)
	}
}

func TestSubAgentToolExecutorUtilityFunctions(t *testing.T) {
	t.Parallel()

	if !toolResultTruncated(map[string]any{"truncated": "TRUE"}) {
		t.Fatalf("string truncated flag should be recognized")
	}
	if toolResultTruncated(map[string]any{"truncated": 1}) {
		t.Fatalf("unsupported truncated type should be false")
	}

	if got := elapsedMilliseconds(time.Time{}); got != 0 {
		t.Fatalf("zero start elapsed = %d, want 0", got)
	}
	if got := elapsedMilliseconds(time.Now().Add(2 * time.Second)); got != 0 {
		t.Fatalf("future start elapsed = %d, want 0", got)
	}
}
