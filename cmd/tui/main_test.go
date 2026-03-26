package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-llm-demo/configs"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeProgram struct {
	runErr error
	called bool
}

func (p *fakeProgram) Run() (tea.Model, error) {
	p.called = true
	return nil, p.runErr
}

func TestLoadDotEnvSetsMissingVarsOnly(t *testing.T) {
	t.Setenv("EXISTING_KEY", "keep-me")
	t.Setenv("NEW_KEY", "")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "EXISTING_KEY=override\nNEW_KEY= new-value \n# comment\nINVALID_LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := os.Getenv("EXISTING_KEY"); got != "keep-me" {
		t.Fatalf("expected existing env var to be preserved, got %q", got)
	}
	if got := os.Getenv("NEW_KEY"); got != "new-value" {
		t.Fatalf("expected new env var to load, got %q", got)
	}
}

func TestLoadDotEnvIgnoresMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	if err := loadDotEnv(missing); err != nil {
		t.Fatalf("expected missing file to be ignored, got %v", err)
	}
}

func TestLoadDotEnvReturnsNonENOENTError(t *testing.T) {
	if err := loadDotEnv(t.TempDir()); err == nil {
		t.Fatal("expected non-ENOENT read error")
	}
}

func TestLoadDotEnvTrimsQuotedValuesAndSkipsEmptyKeys(t *testing.T) {
	t.Setenv("QUOTED_KEY", "")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "QUOTED_KEY=' spaced value '\n =ignored\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := os.Getenv("QUOTED_KEY"); got != " spaced value " {
		t.Fatalf("expected quoted value to be trimmed, got %q", got)
	}
}

func TestLoadPromptReturnsTrimmedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(path, []byte("\n hello prompt \n"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	if got := loadPersonaPrompt(path); got != "hello prompt" {
		t.Fatalf("expected trimmed prompt, got %q", got)
	}
}

func TestLoadPromptReturnsEmptyForMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if got := loadPersonaPrompt(missing); got != "" {
		t.Fatalf("expected empty string for missing file, got %q", got)
	}
}

func TestLoadPromptReturnsEmptyForBlankPath(t *testing.T) {
	if got := loadPersonaPrompt("   "); got != "" {
		t.Fatalf("expected empty string for blank path, got %q", got)
	}
}

func TestParseWorkspaceFlagParsesWorkspaceValue(t *testing.T) {
	stderr := &bytes.Buffer{}

	got, err := parseWorkspaceFlag([]string{"-workspace", "D:/neo-code/workspace"}, stderr)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "D:/neo-code/workspace" {
		t.Fatalf("unexpected workspace flag value %q", got)
	}
}

func TestDefaultRunDepsWiresStandardStreamsAndFunctions(t *testing.T) {
	deps := defaultRunDeps(strings.NewReader("in"), &bytes.Buffer{}, &bytes.Buffer{})

	if deps.stdin == nil || deps.stdout == nil || deps.stderr == nil {
		t.Fatal("expected stdio to be preserved in deps")
	}
	if deps.setUTF8Mode == nil || deps.prepareWorkspace == nil || deps.ensureAPIKeyInteractive == nil || deps.loadAppConfig == nil || deps.loadPersonaPrompt == nil || deps.newProgram == nil {
		t.Fatal("expected default dependencies to be populated")
	}
}

func TestRunUsesInjectableDepBuilder(t *testing.T) {
	origBuildRunDeps := buildRunDeps
	t.Cleanup(func() { buildRunDeps = origBuildRunDeps })

	called := false
	buildRunDeps = func(stdin io.Reader, stdout, stderr io.Writer) runDeps {
		called = true
		return runDeps{
			stdin:            stdin,
			stdout:           stdout,
			stderr:           stderr,
			setUTF8Mode:      func() {},
			prepareWorkspace: func(string) (string, error) { return "D:/neo-code", nil },
			ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) {
				return false, nil
			},
		}
	}

	err := run("D:/neo-code", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected run to use buildRunDeps")
	}
}

func TestRunWithDepsReturnsWorkspacePreparationError(t *testing.T) {
	stderr := &bytes.Buffer{}

	err := runWithDeps("", runDeps{
		stdin:            strings.NewReader(""),
		stdout:           &bytes.Buffer{},
		stderr:           stderr,
		setUTF8Mode:      func() {},
		prepareWorkspace: func(string) (string, error) { return "", errors.New("workspace failed") },
	})
	if err == nil || !strings.Contains(err.Error(), "workspace failed") {
		t.Fatalf("expected workspace error, got %v", err)
	}
}

func TestRunWithDepsStopsCleanlyWhenSetupNotReady(t *testing.T) {
	stdout := &bytes.Buffer{}
	loadCalled := false

	err := runWithDeps("D:/neo-code", runDeps{
		stdin:            strings.NewReader(""),
		stdout:           stdout,
		stderr:           &bytes.Buffer{},
		setUTF8Mode:      func() {},
		prepareWorkspace: func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(_ context.Context, _ *bufio.Scanner, path string) (bool, error) {
			if path != defaultConfigPath {
				t.Fatalf("expected config path %q, got %q", defaultConfigPath, path)
			}
			return false, nil
		},
		loadAppConfig: func(string) error {
			loadCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loadCalled {
		t.Fatal("loadAppConfig should not run when setup is not ready")
	}
	if !strings.Contains(stdout.String(), "NeoCode") {
		t.Fatalf("expected exit message in stdout, got %q", stdout.String())
	}
}

func TestRunWithDepsReturnsBootstrapError(t *testing.T) {
	err := runWithDeps("D:/neo-code", runDeps{
		stdin:       strings.NewReader(""),
		stdout:      &bytes.Buffer{},
		stderr:      &bytes.Buffer{},
		setUTF8Mode: func() {},
		prepareWorkspace: func(string) (string, error) {
			return "D:/neo-code", nil
		},
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) {
			return false, errors.New("bootstrap failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "bootstrap failed") {
		t.Fatalf("expected bootstrap error, got %v", err)
	}
}

func TestRunWithDepsReturnsLoadAppConfigError(t *testing.T) {
	err := runWithDeps("D:/neo-code", runDeps{
		stdin:       strings.NewReader(""),
		stdout:      &bytes.Buffer{},
		stderr:      &bytes.Buffer{},
		setUTF8Mode: func() {},
		prepareWorkspace: func(string) (string, error) {
			return "D:/neo-code", nil
		},
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) {
			return true, nil
		},
		loadAppConfig: func(string) error { return errors.New("load failed") },
	})
	if err == nil || !strings.Contains(err.Error(), "load failed") {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestRunWithDepsPrintsPromptFallbackHintAndRunsProgram(t *testing.T) {
	origGlobalConfig := configs.GlobalAppConfig
	t.Cleanup(func() { configs.GlobalAppConfig = origGlobalConfig })

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cfg := configs.DefaultAppConfig()
	cfg.Persona.FilePath = "./persona.txt"

	program := &fakeProgram{}
	newProgramCalled := false
	err := runWithDeps("D:/neo-code", runDeps{
		stdin:                   strings.NewReader(""),
		stdout:                  stdout,
		stderr:                  stderr,
		setUTF8Mode:             func() {},
		prepareWorkspace:        func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) { return true, nil },
		loadAppConfig: func(string) error {
			configs.GlobalAppConfig = cfg
			return nil
		},
		loadPersonaPrompt: func(path string) (string, string, error) {
			if path != "./persona.txt" {
				t.Fatalf("expected configured prompt path, got %q", path)
			}
			return "prompt text", "./configs/prompt.md", nil
		},
		newProgram: func(persona string, historyTurns int, configPath, workspaceRoot string) (programRunner, error) {
			newProgramCalled = true
			if persona != "prompt text" {
				t.Fatalf("unexpected prompt %q", persona)
			}
			if historyTurns != cfg.History.ShortTermTurns {
				t.Fatalf("unexpected history turns %d", historyTurns)
			}
			if configPath != defaultConfigPath || workspaceRoot != "D:/neo-code" {
				t.Fatalf("unexpected program args: %q %q", configPath, workspaceRoot)
			}
			return program, nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !newProgramCalled || !program.called {
		t.Fatal("expected program to be created and run")
	}
	if !strings.Contains(stderr.String(), "./configs/prompt.md") {
		t.Fatalf("expected fallback prompt hint, got %q", stderr.String())
	}
}

func TestRunWithDepsContinuesWhenPromptLoadFails(t *testing.T) {
	origGlobalConfig := configs.GlobalAppConfig
	t.Cleanup(func() { configs.GlobalAppConfig = origGlobalConfig })

	cfg := configs.DefaultAppConfig()
	stderr := &bytes.Buffer{}
	program := &fakeProgram{}

	err := runWithDeps("D:/neo-code", runDeps{
		stdin:                   strings.NewReader(""),
		stdout:                  &bytes.Buffer{},
		stderr:                  stderr,
		setUTF8Mode:             func() {},
		prepareWorkspace:        func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) { return true, nil },
		loadAppConfig: func(string) error {
			configs.GlobalAppConfig = cfg
			return nil
		},
		loadPersonaPrompt: func(string) (string, string, error) {
			return "", "", errors.New("prompt failed")
		},
		newProgram: func(persona string, historyTurns int, configPath, workspaceRoot string) (programRunner, error) {
			if persona != "" {
				t.Fatalf("expected empty prompt on load failure, got %q", persona)
			}
			return program, nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !program.called {
		t.Fatal("expected program to still run")
	}
	if !strings.Contains(stderr.String(), "prompt failed") {
		t.Fatalf("expected prompt warning, got %q", stderr.String())
	}
}

func TestRunWithDepsReturnsNewProgramError(t *testing.T) {
	origGlobalConfig := configs.GlobalAppConfig
	t.Cleanup(func() { configs.GlobalAppConfig = origGlobalConfig })

	cfg := configs.DefaultAppConfig()

	err := runWithDeps("D:/neo-code", runDeps{
		stdin:                   strings.NewReader(""),
		stdout:                  &bytes.Buffer{},
		stderr:                  &bytes.Buffer{},
		setUTF8Mode:             func() {},
		prepareWorkspace:        func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) { return true, nil },
		loadAppConfig: func(string) error {
			configs.GlobalAppConfig = cfg
			return nil
		},
		loadPersonaPrompt: func(string) (string, string, error) { return "prompt", "", nil },
		newProgram:        func(string, int, string, string) (programRunner, error) { return nil, errors.New("new program failed") },
	})
	if err == nil || !strings.Contains(err.Error(), "new program failed") {
		t.Fatalf("expected new program error, got %v", err)
	}
}

func TestRunWithDepsReturnsProgramRunError(t *testing.T) {
	origGlobalConfig := configs.GlobalAppConfig
	t.Cleanup(func() { configs.GlobalAppConfig = origGlobalConfig })

	cfg := configs.DefaultAppConfig()
	program := &fakeProgram{runErr: errors.New("program failed")}

	err := runWithDeps("D:/neo-code", runDeps{
		stdin:                   strings.NewReader(""),
		stdout:                  &bytes.Buffer{},
		stderr:                  &bytes.Buffer{},
		setUTF8Mode:             func() {},
		prepareWorkspace:        func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) { return true, nil },
		loadAppConfig: func(string) error {
			configs.GlobalAppConfig = cfg
			return nil
		},
		loadPersonaPrompt: func(string) (string, string, error) { return "", "", nil },
		newProgram:        func(string, int, string, string) (programRunner, error) { return program, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "program failed") {
		t.Fatalf("expected run error, got %v", err)
	}
}

func TestRunWithDepsHappyPathCallsUTF8Hook(t *testing.T) {
	origGlobalConfig := configs.GlobalAppConfig
	t.Cleanup(func() { configs.GlobalAppConfig = origGlobalConfig })

	cfg := configs.DefaultAppConfig()
	utf8Called := false
	program := &fakeProgram{}

	err := runWithDeps("D:/neo-code", runDeps{
		stdin:  strings.NewReader(""),
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		setUTF8Mode: func() {
			utf8Called = true
		},
		prepareWorkspace:        func(string) (string, error) { return "D:/neo-code", nil },
		ensureAPIKeyInteractive: func(context.Context, *bufio.Scanner, string) (bool, error) { return true, nil },
		loadAppConfig: func(string) error {
			configs.GlobalAppConfig = cfg
			return nil
		},
		loadPersonaPrompt: func(string) (string, string, error) { return "prompt", "", nil },
		newProgram:        func(string, int, string, string) (programRunner, error) { return program, nil },
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !utf8Called {
		t.Fatal("expected UTF8 hook to be called")
	}
	if !program.called {
		t.Fatal("expected program to run")
	}
}
