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
	"unicode/utf8"

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

func TestRunRejectsUnsupportedMode(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	_, err := runner.Run(context.Background(), Input{
		Mode:      Mode("invalid"),
		SessionID: "session-invalid-mode",
		Workdir:   t.TempDir(),
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
		Config: config.CompactConfig{
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
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
			ManualStrategy:                "unknown_strategy",
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
	}
}

func TestRunManualFullReplaceNoMessagesIsNoop(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }
	runner.randomToken = func() (string, error) { return "token0002", nil }

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeManual,
		SessionID: "session-empty-full-replace",
		Workdir:   t.TempDir(),
		Messages:  nil,
		Config: config.CompactConfig{
			ManualStrategy:                config.CompactManualStrategyFullReplace,
			ManualKeepRecentSpans:         6,
			ToolResultKeepRecent:          3,
			ToolResultPlaceholderMinChars: 100,
			MaxSummaryChars:               1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied {
		t.Fatalf("expected no-op when full_replace runs on empty messages")
	}
	if len(result.Messages) != 0 {
		t.Fatalf("expected no messages, got %+v", result.Messages)
	}
}

func TestSaveTranscriptHandlesHomeDirAndRenameFailures(t *testing.T) {
	t.Parallel()

	t.Run("user home lookup failure", func(t *testing.T) {
		t.Parallel()

		runner := NewRunner()
		runner.userHomeDir = func() (string, error) { return "", errors.New("no home") }
		_, _, err := runner.saveTranscript([]provider.Message{{Role: provider.RoleUser, Content: "hello"}}, "s1", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "resolve user home") {
			t.Fatalf("expected home dir failure, got %v", err)
		}
	})

	t.Run("rename failure removes temp file", func(t *testing.T) {
		t.Parallel()

		runner := NewRunner()
		home := t.TempDir()
		runner.userHomeDir = func() (string, error) { return home, nil }
		runner.randomToken = func() (string, error) { return "token0003", nil }

		removedPath := ""
		runner.rename = func(oldPath, newPath string) error {
			return errors.New("rename failed")
		}
		runner.remove = func(path string) error {
			removedPath = path
			return nil
		}

		_, _, err := runner.saveTranscript(
			[]provider.Message{{Role: provider.RoleUser, Content: "hello"}},
			"s2",
			filepath.Join(home, "workspace"),
		)
		if err == nil || !strings.Contains(err.Error(), "commit transcript") {
			t.Fatalf("expected rename commit failure, got %v", err)
		}
		if strings.TrimSpace(removedPath) == "" || !strings.HasSuffix(removedPath, ".tmp") {
			t.Fatalf("expected temp file cleanup, got %q", removedPath)
		}
	})
}

func TestValidateSummaryRequiresDoneOrInProgress(t *testing.T) {
	t.Parallel()

	_, err := validateSummary(strings.Join([]string{
		"[compact_summary]",
		"decisions:",
		"- none",
		"constraints:",
		"- keep safety checks",
	}, "\n"), 0)
	if err == nil || !strings.Contains(err.Error(), "requires done or in_progress content") {
		t.Fatalf("expected summary section validation failure, got %v", err)
	}
}

func TestValidateSummaryTruncatesByRune(t *testing.T) {
	t.Parallel()

	summary := strings.Join([]string{
		"[compact_summary]",
		"done:",
		"- 已完成：修复 compact 中文截断问题并补齐注释。",
		"",
		"in_progress:",
		"- 继续执行回归测试。",
	}, "\n")
	maxChars := 40

	got, err := validateSummary(summary, maxChars)
	if err != nil {
		t.Fatalf("validateSummary() error = %v", err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8 summary, got %q", got)
	}
	if utf8.RuneCountInString(got) > maxChars {
		t.Fatalf("expected rune count <= %d, got %d", maxChars, utf8.RuneCountInString(got))
	}
	if utf8.RuneCountInString(got) >= utf8.RuneCountInString(summary) {
		t.Fatalf("expected summary to be truncated, got %q", got)
	}
}

func TestMicroCompactThresholdUsesRunes(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	home := t.TempDir()
	runner.userHomeDir = func() (string, error) { return home, nil }

	// "你" 是单个 rune，但占用多个字节。
	longChinese := strings.Repeat("你", 12)
	messages := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: longChinese},
		{Role: provider.RoleTool, ToolCallID: "recent", Content: "keep recent"},
	}

	result, err := runner.Run(context.Background(), Input{
		Mode:      ModeMicro,
		SessionID: "session-rune-threshold",
		Workdir:   t.TempDir(),
		Messages:  messages,
		Config: config.CompactConfig{
			MicroEnabled:                  true,
			ToolResultKeepRecent:          1,
			ToolResultPlaceholderMinChars: 12,
			ManualStrategy:                config.CompactManualStrategyKeepRecent,
			ManualKeepRecentSpans:         6,
			MaxSummaryChars:               1200,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected chinese content to match rune threshold and be compacted")
	}
}

func TestCountMessageCharsUsesRunes(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{
			Role:       "用户",
			Content:    "你好",
			ToolCallID: "工具",
			ToolCalls: []provider.ToolCall{
				{ID: "调用", Name: "读取", Arguments: `{"路径":"文件"}`},
			},
		},
	}

	got := countMessageChars(messages)
	want := utf8.RuneCountInString("用户") +
		utf8.RuneCountInString("你好") +
		utf8.RuneCountInString("工具") +
		utf8.RuneCountInString("调用") +
		utf8.RuneCountInString("读取") +
		utf8.RuneCountInString(`{"路径":"文件"}`)
	if got != want {
		t.Fatalf("countMessageChars() = %d, want %d", got, want)
	}
}
