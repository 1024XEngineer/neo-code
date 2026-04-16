package compact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/context/internalcompact"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

type stubSummaryGenerator struct {
	generateFn func(ctx context.Context, input SummaryInput) (SummaryOutput, error)
	calls      []SummaryInput
	output     SummaryOutput
	err        error
}

func (g *stubSummaryGenerator) Generate(ctx context.Context, input SummaryInput) (SummaryOutput, error) {
	cloned := input
	cloned.ArchivedMessages = cloneMessages(input.ArchivedMessages)
	cloned.RetainedMessages = cloneMessages(input.RetainedMessages)
	cloned.CurrentTaskState = input.CurrentTaskState.Clone()
	g.calls = append(g.calls, cloned)
	if g.generateFn != nil {
		return g.generateFn(ctx, input)
	}
	return SummaryOutput{
		TaskState:      g.output.TaskState.Clone(),
		DisplaySummary: g.output.DisplaySummary,
	}, g.err
}

func validSemanticSummary() string {
	entries := map[string]string{
		internalcompact.SectionDone:        "- Completed the previous investigation and captured the outcome.",
		internalcompact.SectionInProgress:  "- Continue from the retained recent context window.",
		internalcompact.SectionDecisions:   "- Keep manual compact summaries in the existing section layout for compatibility.",
		internalcompact.SectionCodeChanges: "- Updated internal/context/compact/runner.go to use semantic summaries.",
		internalcompact.SectionConstraints: "- Preserve only the minimum information needed to continue the work.",
	}

	lines := []string{internalcompact.SummaryMarker}
	for _, section := range internalcompact.SummarySections() {
		lines = append(lines, section+":", entries[section], "")
	}
	return strings.Join(lines[:len(lines)-1], "\n")
}

func validSummaryOutput() SummaryOutput {
	return SummaryOutput{
		TaskState: agentsession.TaskState{
			Goal:      "Continue the current coding task",
			Progress:  []string{"Captured the archived context into durable task state."},
			OpenItems: []string{"Finish the retained follow-up work."},
			NextStep:  "Continue from the retained recent context window.",
		},
		DisplaySummary: validSemanticSummary(),
	}
}

func TestManualCompactKeepRecentRetainsRecentMessagesAndWholeToolBlock(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old requirement")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle request")}},
		{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: providertypes.RoleTool, ToolCallID: "call-old", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old result")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after tool")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("instruction to keep")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ack")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent follow up")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest result")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-c",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 8,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected manual compact applied")
	}
	if len(result.Messages) != 10 {
		t.Fatalf("expected summary + 9 retained messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != providertypes.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", result.Messages[0].Role)
	}
	for _, section := range []string{"done:", "in_progress:", "decisions:", "code_changes:", "constraints:"} {
		summaryText := renderTranscriptParts(result.Messages[0].Parts)
		if !strings.Contains(summaryText, section) {
			t.Fatalf("expected summary to include section %q, got %q", section, summaryText)
		}
	}
	if result.Messages[1].Role != providertypes.RoleAssistant || len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected retained tool call block start, got %+v", result.Messages[1])
	}
	if result.Messages[2].Role != providertypes.RoleTool || result.Messages[2].ToolCallID != "call-old" {
		t.Fatalf("expected retained tool result, got %+v", result.Messages[2])
	}
	if result.TaskState.Goal != "Continue the current coding task" {
		t.Fatalf("expected durable task state to be returned, got %+v", result.TaskState)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected generator to run once, got %d", len(generator.calls))
	}
	if len(generator.calls[0].ArchivedMessages) != 3 || len(generator.calls[0].RetainedMessages) != 9 {
		t.Fatalf("unexpected generator input: %+v", generator.calls[0])
	}
	if generator.calls[0].ArchivedMessageCount != 3 {
		t.Fatalf("expected archived message count 3, got %+v", generator.calls[0])
	}
}

func TestManualCompactPassesCurrentTaskStateAndFiltersOldDisplaySummary(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	currentState := agentsession.TaskState{
		Goal:      "Finish task state refactor",
		OpenItems: []string{"Update tests"},
		NextStep:  "Patch compact runner tests",
	}
	messages := []providertypes.Message{
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart(validSemanticSummary())}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-task-state",
		Workdir:   t.TempDir(),
		Messages:  messages,
		TaskState: currentState,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compact applied")
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected generator called once, got %d", len(generator.calls))
	}
	if generator.calls[0].CurrentTaskState.Goal != currentState.Goal {
		t.Fatalf("expected current task state forwarded, got %+v", generator.calls[0].CurrentTaskState)
	}
	if len(generator.calls[0].ArchivedMessages) != 2 {
		t.Fatalf("expected old display summary filtered from archived messages, got %+v", generator.calls[0].ArchivedMessages)
	}
	if strings.HasPrefix(strings.TrimSpace(renderTranscriptParts(generator.calls[0].ArchivedMessages[0].Parts)), "[compact_summary]") {
		t.Fatalf("expected compact summary message to be filtered, got %+v", generator.calls[0].ArchivedMessages)
	}
}

func TestReactiveCompactUsesKeepRecentAndReportsReactiveMode(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old requirement")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle request")}},
		{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: providertypes.RoleTool, ToolCallID: "call-old", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old result")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after tool")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("instruction to keep")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ack")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent follow up")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest result")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeReactive,
		SessionID: "session-reactive",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 8,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected reactive compact applied")
	}
	if result.Metrics.TriggerMode != string(ModeReactive) {
		t.Fatalf("expected trigger mode %q, got %q", ModeReactive, result.Metrics.TriggerMode)
	}
	if result.TranscriptID == "" || result.TranscriptPath == "" {
		t.Fatalf("expected transcript metadata, got %+v", result)
	}
	if len(result.Messages) != 10 {
		t.Fatalf("expected summary + 9 retained messages, got %d", len(result.Messages))
	}
	if result.Messages[1].Role != providertypes.RoleAssistant || len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected retained tool call block start, got %+v", result.Messages[1])
	}
	if result.Messages[2].Role != providertypes.RoleTool || result.Messages[2].ToolCallID != "call-old" {
		t.Fatalf("expected retained tool result, got %+v", result.Messages[2])
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected generator to run once, got %d", len(generator.calls))
	}
	if generator.calls[0].Mode != ModeReactive {
		t.Fatalf("expected summary input mode %q, got %q", ModeReactive, generator.calls[0].Mode)
	}
	if generator.calls[0].Config.ManualStrategy != config.CompactManualStrategyKeepRecent {
		t.Fatalf("expected reactive compact to force keep_recent, got %q", generator.calls[0].Config.ManualStrategy)
	}
	if len(generator.calls[0].ArchivedMessages) != 3 || len(generator.calls[0].RetainedMessages) != 9 {
		t.Fatalf("unexpected generator input: %+v", generator.calls[0])
	}
}

func TestAutoCompactUsesManualStrategyAndReportsAutoMode(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old requirement")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent answer")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeAuto,
		SessionID: "session-auto",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 4,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected auto compact applied")
	}
	if result.Metrics.TriggerMode != string(ModeAuto) {
		t.Fatalf("expected trigger mode %q, got %q", ModeAuto, result.Metrics.TriggerMode)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected generator to run once, got %d", len(generator.calls))
	}
	if generator.calls[0].Mode != ModeAuto {
		t.Fatalf("expected summary input mode %q, got %q", ModeAuto, generator.calls[0].Mode)
	}
	if generator.calls[0].Config.ManualStrategy != config.CompactManualStrategyKeepRecent {
		t.Fatalf("expected auto compact to retain manual strategy, got %q", generator.calls[0].Config.ManualStrategy)
	}
}

func TestManualCompactKeepRecentProtectsLatestExplicitUserInstruction(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old requirement")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ack")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow up 1")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow up 2")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow up 3")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow up 4")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow up 5")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-keep-last-user",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 3,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compact applied")
	}
	if len(generator.calls) != 1 {
		t.Fatalf("expected generator called once, got %d", len(generator.calls))
	}
	if len(generator.calls[0].ArchivedMessages) != 2 || len(generator.calls[0].RetainedMessages) != 7 {
		t.Fatalf("expected protected tail to start at latest user instruction, got %+v", generator.calls[0])
	}
	if result.Messages[1].Role != providertypes.RoleUser || renderTranscriptParts(result.Messages[1].Parts) != "latest explicit instruction" {
		t.Fatalf("expected retained latest explicit instruction, got %+v", result.Messages[1])
	}
}

func TestManualCompactWritesTranscriptJSONL(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{output: validSummaryOutput()})
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-jsonl",
		Workdir:   filepath.Join(home, "workspace"),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	data, err := os.ReadFile(result.TranscriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if !strings.Contains(string(data), `"role":"user"`) {
		t.Fatalf("expected jsonl content, got %q", string(data))
	}
	expectedDir := transcriptDirectory(home, hashProject(filepath.Join(home, "workspace")))
	if filepath.Dir(result.TranscriptPath) != expectedDir {
		t.Fatalf("expected transcript path under %q, got %q", expectedDir, result.TranscriptPath)
	}
	if filepath.Ext(result.TranscriptPath) != transcriptFileExtension {
		t.Fatalf("expected transcript extension %q, got %q", transcriptFileExtension, result.TranscriptPath)
	}
}

func TestManualCompactFailsWhenTranscriptWriteFails(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{output: validSummaryOutput()})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }
	runner.mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("disk full")
	}

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-fail",
		Workdir:   t.TempDir(),
		Messages:  []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected transcript write failure, got %v", err)
	}
}

func TestManualCompactFullReplaceKeepsProtectedTail(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old requirement")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: providertypes.RoleTool, ToolCallID: "call-old", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old result")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-full-replace",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected full_replace compact applied")
	}
	if len(result.Messages) != 5 {
		t.Fatalf("expected summary plus protected tail, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != providertypes.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", result.Messages[0].Role)
	}
	if len(generator.calls) != 1 || len(generator.calls[0].RetainedMessages) != 4 {
		t.Fatalf("expected full_replace to summarize all messages, got %+v", generator.calls)
	}
	if generator.calls[0].ArchivedMessageCount != 2 {
		t.Fatalf("expected full_replace archived 2 messages, got %+v", generator.calls[0])
	}
}

func TestManualCompactFullReplaceWithoutArchivableMessagesSkipsGenerator(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-full-replace-skip",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied {
		t.Fatalf("expected full_replace to skip when nothing can be archived")
	}
	if len(generator.calls) != 0 {
		t.Fatalf("expected generator not to run, got %d calls", len(generator.calls))
	}
	if len(result.Messages) != len(messages) {
		t.Fatalf("expected original messages kept, got %+v", result.Messages)
	}
}

func TestRunManualRejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{output: validSummaryOutput()})
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }
	runner.randomToken = func() (string, error) { return "token0001", nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-invalid-strategy",
		Workdir:   t.TempDir(),
		Messages:  []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Config: config.CompactConfig{
			ManualStrategy:           "unknown_strategy",
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
	}
}

func TestRunRejectsUnsupportedMode(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{output: validSummaryOutput()})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      Mode("unexpected"),
		SessionID: "session-invalid-mode",
		Workdir:   t.TempDir(),
		Messages:  []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}

func TestCountMessageCharsUsesRunes(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: "用户", Parts: []providertypes.ContentPart{providertypes.NewTextPart("你好")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
	}
	got := countMessageChars(messages)
	want := len([]rune("用户")) + len([]rune("你好")) + len([]rune(providertypes.RoleAssistant)) + len([]rune("done"))
	if got != want {
		t.Fatalf("countMessageChars() = %d, want %d", got, want)
	}
}

func TestSaveTranscriptUsesUniqueIDWithinSameTimestamp(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{output: validSummaryOutput()})
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }
	fixedNow := time.Unix(1712052000, 123456789)
	runner.now = func() time.Time { return fixedNow }
	tokenSeq := []string{"a1b2c3d4", "b2c3d4e5"}
	runner.randomToken = func() (string, error) {
		next := tokenSeq[0]
		tokenSeq = tokenSeq[1:]
		return next, nil
	}

	input := Input{
		Mode:      ModeManual,
		SessionID: "session-dup-safe",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("world")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	}

	first, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	second, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if first.TranscriptID == second.TranscriptID {
		t.Fatalf("expected distinct transcript ids, got %q", first.TranscriptID)
	}
	if first.TranscriptPath == second.TranscriptPath {
		t.Fatalf("expected distinct transcript paths, got %q", first.TranscriptPath)
	}
}

func TestManualCompactGeneratorInvalidSummaryFails(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{
		output: SummaryOutput{
			TaskState: validSummaryOutput().TaskState,
			DisplaySummary: strings.Join([]string{
				"[compact_summary]",
				"done:",
				"- ok",
				"",
				"in_progress:",
				"- continue",
			}, "\n"),
		},
	})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-invalid-summary",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("newer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing required section") {
		t.Fatalf("expected strict summary validation failure, got %v", err)
	}
}

func TestManualCompactGeneratorEmptyBulletFails(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{
		output: SummaryOutput{
			TaskState: validSummaryOutput().TaskState,
			DisplaySummary: strings.Join([]string{
				"[compact_summary]",
				"done:",
				"- ok",
				"",
				"in_progress:",
				"- continue",
				"",
				"decisions:",
				"- ",
				"",
				"code_changes:",
				"- file updated",
				"",
				"constraints:",
				"- none",
			}, "\n"),
		},
	})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-empty-bullet",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("newer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "empty bullet") {
		t.Fatalf("expected empty bullet validation failure, got %v", err)
	}
}

func TestManualCompactRejectsEmptyTaskState(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{
		output: SummaryOutput{
			TaskState:      agentsession.TaskState{},
			DisplaySummary: validSemanticSummary(),
		},
	})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-empty-task-state",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("newer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "generated task_state is empty") {
		t.Fatalf("expected empty task_state rejection, got %v", err)
	}
}

func TestManualCompactTruncationFailsWhenStructureBreaks(t *testing.T) {
	t.Parallel()

	summary := validSemanticSummary()
	runner := NewRunner(&stubSummaryGenerator{output: SummaryOutput{
		TaskState:      validSummaryOutput().TaskState,
		DisplaySummary: summary,
	}})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-truncate-fail",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("newer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          40,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "max_summary_chars") {
		t.Fatalf("expected truncation validation failure, got %v", err)
	}
}

func TestManualCompactKeepRecentWithoutEnoughMessagesSkipsGenerator(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-no-compact",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("single message")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 2,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied {
		t.Fatalf("expected compact to be skipped")
	}
	if len(generator.calls) != 0 {
		t.Fatalf("expected generator not to run, got %d calls", len(generator.calls))
	}
}

func TestManualCompactReturnsErrorWhenSummaryGeneratorIsMissing(t *testing.T) {
	t.Parallel()

	runner := NewRunner(nil)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-missing-generator",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("newer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           config.CompactManualStrategyFullReplace,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "summary generator is nil") {
		t.Fatalf("expected missing generator error, got %v", err)
	}
}

func TestManualCompactDefaultsToKeepRecentStrategyWhenManualStrategyIsEmpty(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{output: validSummaryOutput()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-default-strategy",
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old request")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old answer")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest request")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
		},
		Config: config.CompactConfig{
			ManualStrategy:           "",
			ManualKeepRecentMessages: 2,
			MaxSummaryChars:          1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compact to apply with default keep_recent strategy")
	}
	if len(generator.calls) != 1 || generator.calls[0].Config.ManualStrategy != config.CompactManualStrategyKeepRecent {
		t.Fatalf("expected keep_recent default strategy, got %+v", generator.calls)
	}
}
