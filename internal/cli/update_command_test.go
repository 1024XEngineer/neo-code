package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/updater"
	"neo-code/internal/version"
)

func TestUpdateCommandPassesPrereleaseFlag(t *testing.T) {
	originalRunner := runUpdateCommand
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { runUpdateCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	runGlobalPreload = func(context.Context) error { return nil }
	runSilentUpdateCheck = func(context.Context) {}

	var received updateCommandOptions
	runUpdateCommand = func(_ context.Context, options updateCommandOptions) (updater.UpdateResult, error) {
		received = options
		return updater.UpdateResult{Updated: false, LatestVersion: "v0.2.1"}, nil
	}

	command := NewRootCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"update", "--prerelease"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if !received.IncludePrerelease {
		t.Fatal("expected IncludePrerelease to be true")
	}
	if got := stdout.String(); got == "" {
		t.Fatal("expected update command output")
	}
}

func TestUpdateCommandShowsSuccessMessage(t *testing.T) {
	originalRunner := runUpdateCommand
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { runUpdateCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	runGlobalPreload = func(context.Context) error { return nil }
	runSilentUpdateCheck = func(context.Context) {}
	runUpdateCommand = func(context.Context, updateCommandOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{
			CurrentVersion: "\x1b[31mv0.1.0\x1b[0m",
			LatestVersion:  "\x1b[32mv0.2.1\x1b[0m\t",
			Updated:        true,
		}, nil
	}

	command := NewRootCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"update"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if got := stdout.String(); got == "" || !bytes.Contains(stdout.Bytes(), []byte("Updated successfully")) {
		t.Fatalf("unexpected output: %q", got)
	}
	if strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("expected sanitized output without ANSI sequence, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "v0.1.0 -> v0.2.1") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestUpdateCommandShowsUnknownLatestWhenLatestVersionEmpty(t *testing.T) {
	originalRunner := runUpdateCommand
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { runUpdateCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	runGlobalPreload = func(context.Context) error { return nil }
	runSilentUpdateCheck = func(context.Context) {}
	runUpdateCommand = func(context.Context, updateCommandOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{Updated: false, LatestVersion: " \t "}, nil
	}

	command := NewRootCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"update"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "latest: unknown") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestUpdateCommandSanitizesLatestVersionInUpToDateMessage(t *testing.T) {
	originalRunner := runUpdateCommand
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { runUpdateCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	runGlobalPreload = func(context.Context) error { return nil }
	runSilentUpdateCheck = func(context.Context) {}
	runUpdateCommand = func(context.Context, updateCommandOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{Updated: false, LatestVersion: "\x1b[31mv0.2.1\x1b[0m\t\n"}, nil
	}

	command := NewRootCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetArgs([]string{"update"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("expected sanitized output without ANSI sequence, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "latest: v0.2.1") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestUpdateCommandReturnsRunnerError(t *testing.T) {
	originalRunner := runUpdateCommand
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { runUpdateCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	expected := errors.New("update failed")
	runGlobalPreload = func(context.Context) error { return nil }
	runSilentUpdateCheck = func(context.Context) {}
	runUpdateCommand = func(context.Context, updateCommandOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{}, expected
	}

	command := NewRootCommand()
	command.SetArgs([]string{"update"})
	err := command.ExecuteContext(context.Background())
	if !errors.Is(err, expected) {
		t.Fatalf("expected runner error %v, got %v", expected, err)
	}
}

func TestConsumeUpdateNoticeOnce(t *testing.T) {
	_ = ConsumeUpdateNotice()
	setUpdateNotice("  new version  ")

	if got := ConsumeUpdateNotice(); got != "new version" {
		t.Fatalf("ConsumeUpdateNotice() = %q, want %q", got, "new version")
	}
	if got := ConsumeUpdateNotice(); got != "" {
		t.Fatalf("ConsumeUpdateNotice() second call = %q, want empty", got)
	}
}

func TestSetUpdateNoticeIgnoresEmptyMessage(t *testing.T) {
	_ = ConsumeUpdateNotice()
	setUpdateNotice(" \n\t")
	if got := ConsumeUpdateNotice(); got != "" {
		t.Fatalf("ConsumeUpdateNotice() = %q, want empty", got)
	}
}

func TestDefaultUpdateCommandRunnerTimeout(t *testing.T) {
	originalDoUpdate := doUpdate
	originalTimeout := updateCommandTimeout
	t.Cleanup(func() { doUpdate = originalDoUpdate })
	t.Cleanup(func() { updateCommandTimeout = originalTimeout })

	updateCommandTimeout = 20 * time.Millisecond
	doUpdate = func(ctx context.Context, options updater.UpdateOptions) (updater.UpdateResult, error) {
		<-ctx.Done()
		return updater.UpdateResult{}, ctx.Err()
	}

	_, err := defaultUpdateCommandRunner(context.Background(), updateCommandOptions{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "\u66f4\u65b0\u8d85\u65f6") {
		t.Fatalf("expected friendly timeout message, got %v", err)
	}
}

func TestDefaultUpdateCommandRunnerPassesOptionsAndResult(t *testing.T) {
	originalDoUpdate := doUpdate
	originalTimeout := updateCommandTimeout
	t.Cleanup(func() { doUpdate = originalDoUpdate })
	t.Cleanup(func() { updateCommandTimeout = originalTimeout })

	updateCommandTimeout = time.Second
	expected := updater.UpdateResult{
		CurrentVersion: "v0.1.0",
		LatestVersion:  "v0.2.0",
		Updated:        true,
	}
	var captured updater.UpdateOptions
	doUpdate = func(ctx context.Context, options updater.UpdateOptions) (updater.UpdateResult, error) {
		captured = options
		return expected, nil
	}

	result, err := defaultUpdateCommandRunner(context.Background(), updateCommandOptions{IncludePrerelease: true})
	if err != nil {
		t.Fatalf("defaultUpdateCommandRunner() error = %v", err)
	}
	if result != expected {
		t.Fatalf("result = %+v, want %+v", result, expected)
	}
	if !captured.IncludePrerelease {
		t.Fatal("expected IncludePrerelease to be forwarded")
	}
	if captured.CurrentVersion != version.Current() {
		t.Fatalf("CurrentVersion = %q, want %q", captured.CurrentVersion, version.Current())
	}
}

func TestDefaultUpdateCommandRunnerReturnsUnderlyingError(t *testing.T) {
	originalDoUpdate := doUpdate
	originalTimeout := updateCommandTimeout
	t.Cleanup(func() { doUpdate = originalDoUpdate })
	t.Cleanup(func() { updateCommandTimeout = originalTimeout })

	updateCommandTimeout = time.Second
	expected := errors.New("network failed")
	doUpdate = func(context.Context, updater.UpdateOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{}, expected
	}

	_, err := defaultUpdateCommandRunner(context.Background(), updateCommandOptions{})
	if !errors.Is(err, expected) {
		t.Fatalf("expected underlying error %v, got %v", expected, err)
	}
}

func TestDisplayVersionForTerminal(t *testing.T) {
	if got := displayVersionForTerminal("\x1b[31mv0.2.1\x1b[0m\t"); got != "v0.2.1" {
		t.Fatalf("displayVersionForTerminal() = %q, want %q", got, "v0.2.1")
	}
	if got := displayVersionForTerminal(" \n\t"); got != "unknown" {
		t.Fatalf("displayVersionForTerminal() empty = %q, want %q", got, "unknown")
	}
}
