package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
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
	if got := resolveSubAgentMaxTurns(99); got != 99 {
		t.Fatalf("resolveSubAgentMaxTurns(99) = %d", got)
	}
	if got := resolveSubAgentMaxTurns(3); got != 3 {
		t.Fatalf("resolveSubAgentMaxTurns(3) = %d", got)
	}

	if got := effectiveMaxToolCallsPerStep(0); got != 0 {
		t.Fatalf("effectiveMaxToolCallsPerStep(0) = %d, want 0", got)
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
	if !isRecoverableSubAgentToolError(fmt.Errorf("wrapped: %w", tools.ErrPermissionDenied)) {
		t.Fatalf("wrapped permission denied should be recoverable")
	}
	if !isSubAgentPermissionDeniedError(errors.New(permissionRejectedErrorMessage)) {
		t.Fatalf("permission rejected message should be recognized")
	}
	if isSubAgentPermissionDeniedError(errors.New("other error")) {
		t.Fatalf("non-permission error should not be recognized as denied")
	}
}

func TestBuildSubAgentInitialMessagesAndOutputParserEdges(t *testing.T) {
	t.Parallel()

	messages := buildSubAgentInitialMessages(subagent.StepInput{
		Policy: subagent.RolePolicy{
			AllowedTools: []string{"filesystem_read_file", "filesystem_grep"},
		},
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
		Capability: subagent.Capability{
			AllowedPaths: []string{"/tmp/workdir", "/tmp/workdir", " "},
		},
	})
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	text := messages[0].Parts[0].Text
	if text == "" {
		t.Fatalf("expected non-empty initial message")
	}
	if !strings.Contains(text, "allowed_tools: filesystem_read_file, filesystem_grep") {
		t.Fatalf("expected allowed_tools in initial message, got %q", text)
	}
	if !strings.Contains(text, "allowed_paths:") || !strings.Contains(text, "- /tmp/workdir") {
		t.Fatalf("expected allowed_paths in initial message, got %q", text)
	}

	prompt := buildSubAgentSystemPrompt(
		subagent.RolePolicy{
			SystemPrompt:        "role prompt",
			ToolUseMode:         subagent.ToolUseModeAuto,
			MaxToolCallsPerStep: 2,
		},
		[]string{"filesystem_read_file"},
		[]string{"/tmp/workdir"},
	)
	if !strings.Contains(prompt, "allowed_tools: filesystem_read_file") {
		t.Fatalf("expected allowed_tools in system prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "allowed_paths:") || !strings.Contains(prompt, "- /tmp/workdir") {
		t.Fatalf("expected allowed_paths in system prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "spawn_subagent(mode=todo)") {
		t.Fatalf("expected mode=todo responsibility guidance, got %q", prompt)
	}
	if !strings.Contains(prompt, "只返回单个 JSON 对象") {
		t.Fatalf("expected strict json output guidance, got %q", prompt)
	}

	emptyPrompt := buildSubAgentSystemPrompt(
		subagent.RolePolicy{
			SystemPrompt:        "role prompt",
			ToolUseMode:         subagent.ToolUseModeAuto,
			MaxToolCallsPerStep: 1,
		},
		nil,
		nil,
	)
	if !strings.Contains(emptyPrompt, "allowed_tools: (none)") {
		t.Fatalf("expected explicit empty allowed_tools marker, got %q", emptyPrompt)
	}
	if !strings.Contains(emptyPrompt, "allowed_paths: (none)") {
		t.Fatalf("expected explicit empty allowed_paths marker, got %q", emptyPrompt)
	}

	if _, err := extractSubAgentJSONObject("{\"summary\":"); err == nil {
		t.Fatalf("expected incomplete json error")
	}
	if _, err := extractSubAgentJSONObject("no json"); err == nil {
		t.Fatalf("expected missing json error")
	}
	if _, err := extractSubAgentJSONObject(`{"example":true}`); err == nil {
		t.Fatalf("expected required contract keys error")
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

	if filtered := filterToolSpecsByAllowlist(nil, []string{"bash"}); len(filtered) != 0 {
		t.Fatalf("expected empty specs when input is nil")
	}

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

func TestSubAgentToolResultToMessageAppliesSubAgentLimit(t *testing.T) {
	t.Parallel()

	longContent := strings.Repeat("x", subAgentToolResultMaxRunes+128)
	message := subAgentToolResultToMessage(
		providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"},
		subagent.ToolExecutionResult{
			Name:     "filesystem_read_file",
			Content:  longContent,
			Decision: permissionDecisionAllow,
			Metadata: map[string]any{"source": "tool"},
		},
	)
	content := message.Parts[0].Text
	if !strings.Contains(content, "[truncated]") {
		t.Fatalf("expected truncated marker in tool content, got %q", content)
	}
	if len([]rune(content)) > subAgentToolResultMaxRunes+len([]rune(subAgentTextTruncatedSuffix)) {
		t.Fatalf("unexpected content length after truncate, got=%d", len([]rune(content)))
	}
	if message.ToolMetadata["truncated"] != "true" {
		t.Fatalf("expected truncated metadata=true, got %+v", message.ToolMetadata)
	}
}

func TestTrimSubAgentMessageWindowKeepsPinnedAndRecent(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("task context")}},
	}
	for idx := 0; idx < 24; idx++ {
		messages = append(messages, providertypes.Message{
			Role:  providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("step-%02d-%s", idx, strings.Repeat("x", 1024)))},
		})
	}

	trimmed := trimSubAgentMessageWindow(messages)
	if len(trimmed) > subAgentMessageWindowMaxMessages {
		t.Fatalf("trimmed messages len = %d, want <= %d", len(trimmed), subAgentMessageWindowMaxMessages)
	}
	if trimmed[0].Parts[0].Text != "task context" {
		t.Fatalf("expected pinned message kept, got %q", trimmed[0].Parts[0].Text)
	}
	if !strings.Contains(trimmed[1].Parts[0].Text, "[subagent_history_trimmed]") {
		t.Fatalf("expected history summary marker, got %q", trimmed[1].Parts[0].Text)
	}
	last := trimmed[len(trimmed)-1].Parts[0].Text
	if !strings.Contains(last, "step-23-") {
		t.Fatalf("expected latest message retained, got %q", last)
	}
}

func TestTrimSubAgentMessageWindowClampsPinnedMessage(t *testing.T) {
	t.Parallel()

	pinned := strings.Repeat("p", subAgentMessageWindowMaxRunes+64)
	trimmed := trimSubAgentMessageWindow([]providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(pinned)}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("tail")}},
	})
	if len(trimmed) < 1 {
		t.Fatalf("expected non-empty trimmed messages")
	}
	if got := trimmed[0].Parts[0].Text; !strings.Contains(got, "[truncated]") {
		t.Fatalf("expected pinned message to be truncated, got %q", got)
	}
}
