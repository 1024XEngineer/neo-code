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
	"neo-code/internal/provider"
)

func TestManualCompactAddsSummaryAndKeepsRecentSpans(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-old", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-old", Content: "old result"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-c",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			ManualStrategy:        config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans: 1,
			MaxSummaryChars:       1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected manual compact applied")
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected summary + 1 kept span, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != provider.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", result.Messages[0].Role)
	}
	for _, section := range []string{"done:", "in_progress:", "decisions:", "code_changes:", "constraints:"} {
		if !strings.Contains(result.Messages[0].Content, section) {
			t.Fatalf("expected summary to include section %q, got %q", section, result.Messages[0].Content)
		}
	}
	if result.Messages[1].Content != "latest answer" {
		t.Fatalf("expected newest span kept, got %+v", result.Messages[1])
	}
}

func TestManualCompactWritesTranscriptJSONL(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
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
			ManualStrategy:        config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans: 6,
			MaxSummaryChars:       1200,
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
	if !strings.Contains(filepath.ToSlash(result.TranscriptPath), "/.neocode/projects/") {
		t.Fatalf("expected transcript path under .neocode/projects, got %q", result.TranscriptPath)
	}
}

func TestManualCompactFailsWhenTranscriptWriteFails(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
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
			ManualStrategy:        config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans: 6,
			MaxSummaryChars:       1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected transcript write failure, got %v", err)
	}
}

func TestManualCompactFullReplaceRewritesAllMessages(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "old requirement"},
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
			ManualStrategy:        config.CompactManualStrategyFullReplace,
			ManualKeepRecentSpans: 6,
			MaxSummaryChars:       1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected full_replace compact applied")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected single summary message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != provider.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", result.Messages[0].Role)
	}
}

func TestRunManualRejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }
	runner.randomToken = func() (string, error) { return "token0001", nil }

	_, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-invalid-strategy",
		Workdir:   t.TempDir(),
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
		Config: config.CompactConfig{
			ManualStrategy:        "unknown_strategy",
			ManualKeepRecentSpans: 6,
			MaxSummaryChars:       1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
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

	runner := NewRunner()
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
			ManualStrategy:        config.CompactManualStrategyFullReplace,
			ManualKeepRecentSpans: 6,
			MaxSummaryChars:       1200,
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
