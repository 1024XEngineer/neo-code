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
	"neo-code/internal/provider"
)

type stubSummaryGenerator struct {
	generateFn func(ctx context.Context, input SummaryInput) (string, error)
	calls      []SummaryInput
	summary    string
	err        error
}

func (g *stubSummaryGenerator) Generate(ctx context.Context, input SummaryInput) (string, error) {
	cloned := input
	cloned.ArchivedMessages = cloneMessages(input.ArchivedMessages)
	cloned.RetainedMessages = cloneMessages(input.RetainedMessages)
	g.calls = append(g.calls, cloned)
	if g.generateFn != nil {
		return g.generateFn(ctx, input)
	}
	return g.summary, g.err
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

func TestManualCompactKeepRecentRetainsRecentMessagesAndWholeToolBlock(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "middle request"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-old", Content: "old result"},
		{Role: provider.RoleAssistant, Content: "after tool"},
		{Role: provider.RoleUser, Content: "instruction to keep"},
		{Role: provider.RoleAssistant, Content: "ack"},
		{Role: provider.RoleUser, Content: "recent follow up"},
		{Role: provider.RoleAssistant, Content: "recent answer"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "latest result"},
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
	if result.Messages[0].Role != provider.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", result.Messages[0].Role)
	}
	for _, section := range []string{"done:", "in_progress:", "decisions:", "code_changes:", "constraints:"} {
		if !strings.Contains(result.Messages[0].Content, section) {
			t.Fatalf("expected summary to include section %q, got %q", section, result.Messages[0].Content)
		}
	}
	if result.Messages[1].Role != provider.RoleAssistant || len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected retained tool call block start, got %+v", result.Messages[1])
	}
	if result.Messages[2].Role != provider.RoleTool || result.Messages[2].ToolCallID != "call-old" {
		t.Fatalf("expected retained tool result, got %+v", result.Messages[2])
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

func TestReactiveCompactUsesKeepRecentAndReportsReactiveMode(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "middle request"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-old", Content: "old result"},
		{Role: provider.RoleAssistant, Content: "after tool"},
		{Role: provider.RoleUser, Content: "instruction to keep"},
		{Role: provider.RoleAssistant, Content: "ack"},
		{Role: provider.RoleUser, Content: "recent follow up"},
		{Role: provider.RoleAssistant, Content: "recent answer"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "latest result"},
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
	if result.Messages[1].Role != provider.RoleAssistant || len(result.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected retained tool call block start, got %+v", result.Messages[1])
	}
	if result.Messages[2].Role != provider.RoleTool || result.Messages[2].ToolCallID != "call-old" {
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

func TestManualCompactKeepRecentProtectsLatestExplicitUserInstruction(t *testing.T) {
	t.Parallel()

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "ack"},
		{Role: provider.RoleAssistant, Content: "follow up 1"},
		{Role: provider.RoleAssistant, Content: "follow up 2"},
		{Role: provider.RoleAssistant, Content: "follow up 3"},
		{Role: provider.RoleAssistant, Content: "follow up 4"},
		{Role: provider.RoleAssistant, Content: "follow up 5"},
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
	if result.Messages[1].Role != provider.RoleUser || result.Messages[1].Content != "latest explicit instruction" {
		t.Fatalf("expected retained latest explicit instruction, got %+v", result.Messages[1])
	}
}

func TestManualCompactWritesTranscriptJSONL(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{summary: validSemanticSummary()})
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-jsonl",
		Workdir:   filepath.Join(home, "workspace"),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
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

	runner := NewRunner(&stubSummaryGenerator{summary: validSemanticSummary()})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }
	runner.mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("disk full")
	}

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-fail",
		Workdir:   t.TempDir(),
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
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

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-old", Content: "old result"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
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
	if result.Messages[0].Role != provider.RoleAssistant {
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

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
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

	runner := NewRunner(&stubSummaryGenerator{summary: validSemanticSummary()})
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }
	runner.randomToken = func() (string, error) { return "token0001", nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-invalid-strategy",
		Workdir:   t.TempDir(),
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
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

	runner := NewRunner(&stubSummaryGenerator{summary: validSemanticSummary()})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      Mode("unexpected"),
		SessionID: "session-invalid-mode",
		Workdir:   t.TempDir(),
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
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

	messages := []provider.Message{
		{Role: "用户", Content: "你好"},
		{Role: provider.RoleAssistant, Content: "done"},
	}
	got := countMessageChars(messages)
	want := len([]rune("用户")) + len([]rune("你好")) + len([]rune(provider.RoleAssistant)) + len([]rune("done"))
	if got != want {
		t.Fatalf("countMessageChars() = %d, want %d", got, want)
	}
}

func TestSaveTranscriptUsesUniqueIDWithinSameTimestamp(t *testing.T) {
	t.Parallel()

	runner := NewRunner(&stubSummaryGenerator{summary: validSemanticSummary()})
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
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
			{Role: provider.RoleAssistant, Content: "world"},
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
		summary: strings.Join([]string{
			"[compact_summary]",
			"done:",
			"- ok",
			"",
			"in_progress:",
			"- continue",
		}, "\n"),
	})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-invalid-summary",
		Workdir:   t.TempDir(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "older"},
			{Role: provider.RoleAssistant, Content: "older answer"},
			{Role: provider.RoleUser, Content: "latest explicit instruction"},
			{Role: provider.RoleAssistant, Content: "newer"},
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
		summary: strings.Join([]string{
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
	})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-empty-bullet",
		Workdir:   t.TempDir(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "older"},
			{Role: provider.RoleAssistant, Content: "older answer"},
			{Role: provider.RoleUser, Content: "latest explicit instruction"},
			{Role: provider.RoleAssistant, Content: "newer"},
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

func TestManualCompactTruncationFailsWhenStructureBreaks(t *testing.T) {
	t.Parallel()

	summary := validSemanticSummary()
	runner := NewRunner(&stubSummaryGenerator{summary: summary})
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-truncate-fail",
		Workdir:   t.TempDir(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "older"},
			{Role: provider.RoleAssistant, Content: "older answer"},
			{Role: provider.RoleUser, Content: "latest explicit instruction"},
			{Role: provider.RoleAssistant, Content: "newer"},
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

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-no-compact",
		Workdir:   t.TempDir(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "single message"},
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
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "older"},
			{Role: provider.RoleAssistant, Content: "older answer"},
			{Role: provider.RoleUser, Content: "latest explicit instruction"},
			{Role: provider.RoleAssistant, Content: "newer"},
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

	generator := &stubSummaryGenerator{summary: validSemanticSummary()}
	runner := NewRunner(generator)
	runner.userHomeDir = func() (string, error) { return t.TempDir(), nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-default-strategy",
		Workdir:   t.TempDir(),
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "old request"},
			{Role: provider.RoleAssistant, Content: "old answer"},
			{Role: provider.RoleUser, Content: "latest request"},
			{Role: provider.RoleAssistant, Content: "latest answer"},
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
