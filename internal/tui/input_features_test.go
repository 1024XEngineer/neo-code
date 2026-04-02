package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/config"
)

func TestWorkspaceCommandHelpers(t *testing.T) {
	t.Run("extract workspace command validates prefix and body", func(t *testing.T) {
		if _, err := extractWorkspaceCommand("git status"); err == nil {
			t.Fatalf("expected missing prefix to fail")
		}
		if _, err := extractWorkspaceCommand("&   "); err == nil {
			t.Fatalf("expected empty command to fail")
		}
		got, err := extractWorkspaceCommand("  & git status  ")
		if err != nil || got != "git status" {
			t.Fatalf("expected git status, got %q / %v", got, err)
		}
	})

	t.Run("shell args support bash sh and default powershell", func(t *testing.T) {
		if got := shellArgs("bash", "pwd"); len(got) != 3 || got[0] != "bash" || got[2] != "pwd" {
			t.Fatalf("unexpected bash args %+v", got)
		}
		if got := shellArgs("sh", "pwd"); len(got) != 3 || got[0] != "sh" || got[2] != "pwd" {
			t.Fatalf("unexpected sh args %+v", got)
		}
		if got := shellArgs("unknown", "git status"); len(got) != 4 || got[0] != "powershell" {
			t.Fatalf("expected fallback to powershell, got %+v", got)
		}
	})

	t.Run("format workspace command result handles failures and escapes code fences", func(t *testing.T) {
		got := formatWorkspaceCommandResult("git status", "before ``` after", context.DeadlineExceeded)
		for _, want := range []string{"Command Failed: & git status", "` ` `", "before"} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected formatted result to contain %q, got %q", want, got)
			}
		}
	})

	t.Run("default workspace command executor rejects empty commands", func(t *testing.T) {
		output, err := defaultWorkspaceCommandExecutor(context.Background(), config.Config{}, "   ")
		if err == nil || !strings.Contains(err.Error(), "empty") || output != "" {
			t.Fatalf("expected empty command error, got output=%q err=%v", output, err)
		}
	})
}

func TestWorkspaceFileHelpers(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("README.md")
	mustWrite("internal/tui/update.go")
	mustWrite("internal/tui/view.go")
	mustWrite("node_modules/skip.js")
	mustWrite(".git/config")

	t.Run("collect workspace files skips ignored directories and respects limit", func(t *testing.T) {
		files, err := collectWorkspaceFiles(root, 2)
		if err != nil {
			t.Fatalf("collectWorkspaceFiles() error = %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("expected limited result size 2, got %d (%v)", len(files), files)
		}

		files, err = collectWorkspaceFiles(root, 10)
		if err != nil {
			t.Fatalf("collectWorkspaceFiles() error = %v", err)
		}
		got := strings.Join(files, ",")
		if strings.Contains(got, "node_modules") || strings.Contains(got, ".git") {
			t.Fatalf("expected ignored directories to be skipped, got %v", files)
		}
		if !strings.Contains(got, "internal/tui/update.go") || !strings.Contains(got, "internal/tui/view.go") {
			t.Fatalf("expected workspace files to be collected, got %v", files)
		}
	})

	t.Run("current reference token detects token boundaries", func(t *testing.T) {
		if _, _, _, ok := currentReferenceToken("hello world"); ok {
			t.Fatalf("expected non-reference token to be ignored")
		}
		start, end, token, ok := currentReferenceToken("inspect @internal/tui/upd")
		if !ok || token != "@internal/tui/upd" || start >= end {
			t.Fatalf("unexpected token result start=%d end=%d token=%q ok=%v", start, end, token, ok)
		}
	})

	t.Run("matching references and apply suggestion handle both hit and miss cases", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		app.fileCandidates = []string{"README.md", "internal/tui/update.go", "internal/tui/view.go"}
		app.input.SetValue("inspect @internal/tui/upd")
		app.state.InputText = app.input.Value()

		suggestions := app.matchingFileReferences(app.input.Value())
		if len(suggestions) == 0 || suggestions[0] != "internal/tui/update.go" {
			t.Fatalf("unexpected suggestions %v", suggestions)
		}
		if !app.applyTopFileSuggestion() {
			t.Fatalf("expected top suggestion to be applied")
		}
		if app.state.InputText != "inspect @internal/tui/update.go" {
			t.Fatalf("unexpected completed input %q", app.state.InputText)
		}

		app.input.SetValue("inspect plain-text")
		app.state.InputText = app.input.Value()
		if app.applyTopFileSuggestion() {
			t.Fatalf("expected applyTopFileSuggestion to fail without @ token")
		}
	})
}
