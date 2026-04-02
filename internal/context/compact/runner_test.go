package compact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

func TestMicroCompactReplacesOnlyOldLongToolResults(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "start"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: strings.Repeat("A", 40)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-2", Name: "filesystem_grep", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "short"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-3", Name: "bash", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: strings.Repeat("B", 50)},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeMicro,
		SessionID: "session-a",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			MicroEnabled:                  true,
			ToolResultKeepRecent:          1,
			ToolResultPlaceholderMinChars: 10,
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			MaxSummaryChars:               1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected micro compact applied")
	}
	if !strings.Contains(result.Messages[2].Content, "filesystem_read_file") {
		t.Fatalf("expected first old tool result to be replaced, got %q", result.Messages[2].Content)
	}
	if result.Messages[4].Content != "short" {
		t.Fatalf("expected short old tool result unchanged, got %q", result.Messages[4].Content)
	}
	if result.Messages[6].Content == "[Previous tool used: bash]" {
		t.Fatalf("expected recent tool result to be retained")
	}
	if result.TranscriptID == "" || result.TranscriptPath == "" {
		t.Fatalf("expected transcript metadata, got %+v", result)
	}
	if _, err := os.Stat(result.TranscriptPath); err != nil {
		t.Fatalf("expected transcript file: %v", err)
	}
}

func TestMicroCompactFallsBackToUnknownTool(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "start"},
		{Role: provider.RoleTool, ToolCallID: "missing-call", Content: strings.Repeat("X", 24)},
		{Role: provider.RoleTool, ToolCallID: "recent-call", Content: strings.Repeat("Y", 24)},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeMicro,
		SessionID: "session-b",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			MicroEnabled:                  true,
			ToolResultKeepRecent:          1,
			ToolResultPlaceholderMinChars: 10,
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			MaxSummaryChars:               1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := result.Messages[1].Content; got != "[Previous tool used: unknown_tool]" {
		t.Fatalf("expected unknown tool placeholder, got %q", got)
	}
}

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
			MicroEnabled:                  false,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         1,
			MaxSummaryChars:               1200,
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
	summary := result.Messages[0]
	if summary.Role != provider.RoleAssistant {
		t.Fatalf("expected summary role assistant, got %q", summary.Role)
	}
	for _, section := range []string{"done:", "in_progress:", "decisions:", "code_changes:", "constraints:"} {
		if !strings.Contains(summary.Content, section) {
			t.Fatalf("expected summary to include section %q, got %q", section, summary.Content)
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
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
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
	if !strings.HasPrefix(result.TranscriptID, "transcript_") {
		t.Fatalf("unexpected transcript id: %q", result.TranscriptID)
	}
	if goruntime.GOOS != "windows" {
		info, err := os.Stat(result.TranscriptPath)
		if err != nil {
			t.Fatalf("stat transcript: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("expected transcript mode 0600, got %04o", got)
		}
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
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
		},
		Config: config.CompactConfig{
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
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
			MicroEnabled:                  true,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			ManualStrategy:                config.CompactManualStrategyFullReplace,
			ManualKeepRecentSpans:         6,
			MaxSummaryChars:               1200,
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
	for _, section := range []string{"done:", "in_progress:", "decisions:", "code_changes:", "constraints:"} {
		if !strings.Contains(result.Messages[0].Content, section) {
			t.Fatalf("expected summary section %q, got %q", section, result.Messages[0].Content)
		}
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
		if len(tokenSeq) == 0 {
			return "", errors.New("empty token sequence")
		}
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
			ManualStrategy:                config.CompactManualStrategyFullReplace,
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
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
	if _, err := os.Stat(first.TranscriptPath); err != nil {
		t.Fatalf("first transcript file missing: %v", err)
	}
	if _, err := os.Stat(second.TranscriptPath); err != nil {
		t.Fatalf("second transcript file missing: %v", err)
	}
}
