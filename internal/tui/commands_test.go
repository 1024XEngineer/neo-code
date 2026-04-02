package tui

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"neo-code/internal/config"
)

func TestExecuteLocalCommand(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		expectErr string
		assert    func(t *testing.T, manager *config.Manager, notice string)
	}{
		{
			name:    "help lists supported slash commands",
			command: "/help",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				for _, want := range []string{
					slashUsageHelp,
					slashUsageClear,
					slashUsageCompact,
					slashUsageStatus,
					slashUsageProvider,
					slashUsageModel,
					slashUsageExit,
				} {
					if !strings.Contains(notice, want) {
						t.Fatalf("expected help output to contain %q, got %q", want, notice)
					}
				}
				for _, unwanted := range []string{"/run", "/git", "/file", "/plan", "/undo", "/setting", "/set"} {
					if strings.Contains(notice, unwanted) {
						t.Fatalf("expected help output not to contain %q, got %q", unwanted, notice)
					}
				}
			},
		},
		{
			name:    "status includes current tui snapshot",
			command: "/status",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				for _, want := range []string{
					"Status:",
					"Session: Draft",
					"Running: no",
					"Provider: " + manager.Get().SelectedProvider,
					"Model: " + manager.Get().CurrentModel,
					"Focus: " + focusLabelComposer,
					"Picker: none",
					"Messages: 0",
				} {
					if !strings.Contains(notice, want) {
						t.Fatalf("expected status output to contain %q, got %q", want, notice)
					}
				}
			},
		},
		{
			name:    "provider switches current provider when arg is provided",
			command: "/provider gemini",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				cfg := manager.Get()
				if cfg.SelectedProvider != config.GeminiName {
					t.Fatalf("expected selected provider gemini, got %q", cfg.SelectedProvider)
				}
				if !strings.Contains(notice, "Current provider switched") {
					t.Fatalf("expected provider switch notice, got %q", notice)
				}
			},
		},
		{
			name:      "provider without arg returns usage",
			command:   "/provider",
			expectErr: "usage:",
		},
		{
			name:      "unknown command is rejected",
			command:   "/unknown",
			expectErr: `unknown command "/unknown"`,
		},
		{
			name:      "empty command is rejected",
			command:   "   ",
			expectErr: "empty command",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			providerSvc := newTestProviderService(t, manager)
			notice, err := executeLocalCommand(context.Background(), manager, providerSvc, defaultTestStatusSnapshot(manager), tt.command)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.assert != nil {
				tt.assert(t, manager, notice)
			}
		})
	}
}

func TestMatchingSlashCommands(t *testing.T) {
	t.Parallel()

	app := App{}
	tests := []struct {
		name        string
		input       string
		expectCount int
		expectUsage string
	}{
		{
			name:        "non slash input returns no suggestions",
			input:       "hello",
			expectCount: 0,
		},
		{
			name:        "bare slash returns supported commands only",
			input:       "/",
			expectCount: len(builtinSlashCommands),
			expectUsage: slashUsageHelp,
		},
		{
			name:        "prefix narrows suggestions",
			input:       "/mo",
			expectCount: 1,
			expectUsage: slashUsageModel,
		},
		{
			name:        "complete slash command hides suggestions",
			input:       "/status",
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := app.matchingSlashCommands(tt.input)
			if len(got) != tt.expectCount {
				t.Fatalf("expected %d suggestions, got %d", tt.expectCount, len(got))
			}
			if tt.expectUsage != "" && (len(got) == 0 || got[0].Command.Usage != tt.expectUsage && !containsUsage(got, tt.expectUsage)) {
				t.Fatalf("expected suggestions to contain %q, got %+v", tt.expectUsage, got)
			}
		})
	}
}

func containsUsage(suggestions []commandSuggestion, usage string) bool {
	for _, suggestion := range suggestions {
		if suggestion.Command.Usage == usage {
			return true
		}
	}
	return false
}

func defaultTestStatusSnapshot(manager *config.Manager) statusSnapshot {
	cfg := manager.Get()
	return statusSnapshot{
		ActiveSessionTitle: draftSessionTitle,
		CurrentProvider:    cfg.SelectedProvider,
		CurrentModel:       cfg.CurrentModel,
		CurrentWorkdir:     cfg.Workdir,
		FocusLabel:         focusLabelComposer,
		PickerLabel:        "none",
	}
}

func TestCommandHelperFunctions(t *testing.T) {
	t.Run("splitFirstWord handles empty and remainder", func(t *testing.T) {
		if first, rest := splitFirstWord("   "); first != "" || rest != "" {
			t.Fatalf("expected empty split, got %q / %q", first, rest)
		}
		if first, rest := splitFirstWord("alpha beta gamma"); first != "alpha" || rest != "beta gamma" {
			t.Fatalf("unexpected split result %q / %q", first, rest)
		}
	})

	t.Run("powershell shell args force utf8 output", func(t *testing.T) {
		got := shellArgs("powershell", "git status")
		if len(got) != 4 || got[0] != "powershell" {
			t.Fatalf("unexpected powershell args %+v", got)
		}
		if !strings.Contains(got[3], "65001") || !strings.Contains(got[3], "git status") {
			t.Fatalf("expected utf8 powershell wrapper, got %q", got[3])
		}
	})

	t.Run("sanitize workspace output strips ansi and invalid bytes", func(t *testing.T) {
		raw := "\x1b[31mfatal\x1b[0m:\xff bad\r\nnext\x00line"
		got := sanitizeWorkspaceOutput([]byte(raw))
		if strings.Contains(got, "\x1b") || strings.Contains(got, "\x00") {
			t.Fatalf("expected control chars to be removed, got %q", got)
		}
		for _, want := range []string{"fatal", "bad", "nextline"} {
			if !strings.Contains(strings.ReplaceAll(got, "\n", ""), want) {
				t.Fatalf("expected sanitized output to contain %q, got %q", want, got)
			}
		}
	})

	t.Run("sanitize workspace output decodes utf16le launcher errors", func(t *testing.T) {
		text := "适用于 Linux 的 Windows 子系统没有已安装的分发版。\r\n请运行 wsl.exe --list --online"
		encoded := utf16.Encode([]rune(text))
		raw := make([]byte, 0, len(encoded)*2)
		for _, word := range encoded {
			buf := make([]byte, 2)
			binary.LittleEndian.PutUint16(buf, word)
			raw = append(raw, buf...)
		}

		got := sanitizeWorkspaceOutput(raw)
		for _, want := range []string{"适用于 Linux", "Windows 子系统", "wsl.exe --list --online"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected decoded output to contain %q, got %q", want, got)
			}
		}
	})

	t.Run("decode workspace output prefers utf16 when chinese prefix has no zero bytes", func(t *testing.T) {
		text := "拒绝访问。 \r\n错误代码: Bash/Service/CreateInstance/E_ACCESSDENIED"
		encoded := utf16.Encode([]rune(text))
		raw := make([]byte, 0, len(encoded)*2)
		for _, word := range encoded {
			buf := make([]byte, 2)
			binary.LittleEndian.PutUint16(buf, word)
			raw = append(raw, buf...)
		}

		got := decodeWorkspaceOutput(raw)
		for _, want := range []string{"拒绝访问", "错误代码", "E_ACCESSDENIED"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected decoded utf16 output to contain %q, got %q", want, got)
			}
		}
	})
}

func TestLocalCommandWrappers(t *testing.T) {
	manager := newTestConfigManager(t)
	providerSvc := newTestProviderService(t, manager)

	msg := runLocalCommand(manager, providerSvc, defaultTestStatusSnapshot(manager), "/help")()
	result, ok := msg.(localCommandResultMsg)
	if !ok || result.err != nil || !strings.Contains(result.notice, "Available slash commands") {
		t.Fatalf("expected help command result, got %+v", msg)
	}

	msg = runProviderSelection(providerSvc, "missing-provider")()
	result, ok = msg.(localCommandResultMsg)
	if !ok || result.err == nil {
		t.Fatalf("expected provider selection error, got %+v", msg)
	}
}

func TestExecuteStatusCommandSnapshot(t *testing.T) {
	notice := executeStatusCommand(statusSnapshot{
		ActiveSessionID:    "session-123",
		ActiveSessionTitle: "Implement slash UX",
		IsAgentRunning:     true,
		CurrentProvider:    "openai",
		CurrentModel:       "gpt-5.4",
		CurrentWorkdir:     `D:\repo`,
		CurrentTool:        "bash",
		ExecutionError:     "tool failed",
		FocusLabel:         focusLabelTranscript,
		PickerLabel:        "model",
		MessageCount:       7,
	})
	for _, want := range []string{
		"Session: Implement slash UX",
		"Session ID: session-123",
		"Running: yes",
		"Provider: openai",
		"Model: gpt-5.4",
		"Focus: Transcript",
		"Picker: model",
		"Current Tool: bash",
		"Messages: 7",
		"Error: tool failed",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("expected status output to contain %q, got %q", want, notice)
		}
	}
}
