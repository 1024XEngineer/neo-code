package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/config"
)

func TestExecuteLocalCommand(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		expectErr string
		assert    func(t *testing.T, manager *config.Manager, notice string)
	}{
		{
			name:    "set url updates selected provider",
			command: "/set url https://test.example/v1",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				cfg := manager.Get()
				selected, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("SelectedProviderConfig() error = %v", err)
				}
				if selected.BaseURL != "https://test.example/v1" {
					t.Fatalf("expected updated base url, got %q", selected.BaseURL)
				}
				if !strings.Contains(notice, "Base URL updated") {
					t.Fatalf("expected update notice, got %q", notice)
				}
			},
		},
		{
			name:    "set model updates current model",
			command: "/set model gpt-5.4",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				cfg := manager.Get()
				if cfg.CurrentModel != "gpt-5.4" {
					t.Fatalf("expected current model gpt-5.4, got %q", cfg.CurrentModel)
				}
				selected, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("SelectedProviderConfig() error = %v", err)
				}
				if selected.Model != "gpt-5.4" {
					t.Fatalf("expected selected provider model gpt-5.4, got %q", selected.Model)
				}
				if !strings.Contains(notice, "Current model switched") {
					t.Fatalf("expected model switch notice, got %q", notice)
				}
			},
		},
		{
			name:    "set key writes managed env and reloads",
			command: "/set key secret-key",
			assert: func(t *testing.T, manager *config.Manager, notice string) {
				t.Helper()
				if got := strings.TrimSpace(os.Getenv(config.DefaultOpenAIAPIKeyEnv)); got != "secret-key" {
					t.Fatalf("expected env to be reloaded, got %q", got)
				}
				if !strings.Contains(notice, "updated and loaded") {
					t.Fatalf("expected key reload notice, got %q", notice)
				}
			},
		},
		{
			name:      "unknown command is rejected",
			command:   "/unknown",
			expectErr: `unknown command "/unknown"`,
		},
		{
			name:      "unknown command with suffix is rejected",
			command:   "/unknown_cmd",
			expectErr: `unknown command "/unknown_cmd"`,
		},
		{
			name:      "set usage requires enough arguments",
			command:   "/set url",
			expectErr: "usage:",
		},
		{
			name:      "unsupported set field is rejected",
			command:   "/set nope value",
			expectErr: `unsupported /set field "nope"`,
		},
		{
			name:      "invalid url is rejected",
			command:   "/set url not-a-url",
			expectErr: "invalid url",
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
			notice, err := executeLocalCommand(context.Background(), manager, tt.command)
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
			name:        "bare slash returns all commands",
			input:       "/",
			expectCount: len(builtinSlashCommands),
			expectUsage: slashUsageModel,
		},
		{
			name:        "prefix narrows suggestions",
			input:       "/set u",
			expectCount: 1,
			expectUsage: slashUsageSetURL,
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
