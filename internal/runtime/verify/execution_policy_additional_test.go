package verify

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
)

func TestPolicyCommandExecutorExecuteBranches(t *testing.T) {
	t.Parallel()

	t.Run("empty command denied", func(t *testing.T) {
		t.Parallel()
		_, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{Command: "  "})
		if err == nil || !errors.Is(err, ErrVerificationExecutionDenied) {
			t.Fatalf("expected ErrVerificationExecutionDenied, got %v", err)
		}
	})

	t.Run("invalid workdir returns execution error", func(t *testing.T) {
		t.Parallel()
		policy := config.StaticDefaults().Runtime.Verification.ExecutionPolicy
		result, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{
			Command: "go version",
			Workdir: "/path/not/exist",
			Policy:  policy,
		})
		if err == nil || !errors.Is(err, ErrVerificationExecutionError) {
			t.Fatalf("expected ErrVerificationExecutionError, got result=%+v err=%v", result, err)
		}
	})

	t.Run("command timeout", func(t *testing.T) {
		t.Parallel()
		policy := config.VerificationExecutionPolicyConfig{
			Mode:             "non_interactive",
			AllowedCommands:  []string{"sh"},
			DefaultTimeout:   1,
			DefaultOutputCap: 1024,
		}
		result, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{
			Command: "sh -c 'sleep 2'",
			Policy:  policy,
		})
		if err == nil || !errors.Is(err, ErrVerificationExecutionError) {
			t.Fatalf("expected timeout execution error, got result=%+v err=%v", result, err)
		}
		if !result.TimedOut {
			t.Fatalf("result.TimedOut = false, want true")
		}
	})

	t.Run("non-zero exit code", func(t *testing.T) {
		t.Parallel()
		policy := config.VerificationExecutionPolicyConfig{
			Mode:             "non_interactive",
			AllowedCommands:  []string{"sh"},
			DefaultTimeout:   5,
			DefaultOutputCap: 1024,
		}
		result, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{
			Command: "sh -c 'exit 7'",
			Policy:  policy,
		})
		if err != nil {
			t.Fatalf("expected nil err for exit status, got %v", err)
		}
		if result.ExitCode != 7 {
			t.Fatalf("exit code = %d, want 7", result.ExitCode)
		}
	})
}

func TestExecutionPolicyHelpers(t *testing.T) {
	t.Parallel()

	t.Run("isCommandAllowed branches", func(t *testing.T) {
		t.Parallel()
		policy := config.VerificationExecutionPolicyConfig{
			Mode:            "non_interactive",
			AllowedCommands: []string{"go", "git"},
			DeniedCommands:  []string{"rm"},
		}
		if ok, _ := isCommandAllowed("go", "go test ./...", policy); !ok {
			t.Fatalf("go should be allowed")
		}
		if ok, reason := isCommandAllowed("rm", "rm -rf .", policy); ok || reason == "" {
			t.Fatalf("rm should be denied")
		}
		if ok, _ := isCommandAllowed("go", "go test ./... && echo x", policy); ok {
			t.Fatalf("metacharacter command should be denied")
		}
		if ok, _ := isCommandAllowed("python", "python -V", policy); ok {
			t.Fatalf("python should not pass allow list")
		}
		if ok, _ := isCommandAllowed("git", "git", policy); ok {
			t.Fatalf("git without subcommand should be denied")
		}
		if ok, _ := isCommandAllowed("git", "git checkout .", policy); ok {
			t.Fatalf("git write subcommand should be denied")
		}
	})

	t.Run("basic parser helpers", func(t *testing.T) {
		t.Parallel()
		if commandHead("  ") != "" {
			t.Fatalf("commandHead blank should be empty")
		}
		if commandHead("Go Test") != "go" {
			t.Fatalf("commandHead lower-case mismatch")
		}
		if gitSubcommand("git status") != "status" {
			t.Fatalf("gitSubcommand mismatch")
		}
		if gitSubcommand("go test") != "" {
			t.Fatalf("gitSubcommand should be empty for non-git")
		}
		set := normalizedCommandSet([]string{" Go  ", "", "GIT status"})
		if _, ok := set["go"]; !ok {
			t.Fatalf("normalized set should include go")
		}
		if _, ok := set["git"]; !ok {
			t.Fatalf("normalized set should include git")
		}
		if got := firstPositive(-1, 0, 9, 10); got != 9 {
			t.Fatalf("firstPositive() = %d, want 9", got)
		}
		if got := firstPositive(-1, 0); got != 0 {
			t.Fatalf("firstPositive() = %d, want 0", got)
		}
	})

	t.Run("cappedBuffer branches", func(t *testing.T) {
		t.Parallel()
		buffer := newCappedBuffer(3)
		if _, err := buffer.Write([]byte("abc")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if _, err := buffer.Write([]byte("def")); err != nil {
			t.Fatalf("Write() overflow error = %v", err)
		}
		if !buffer.Truncated() {
			t.Fatalf("buffer should be truncated")
		}
		if buffer.String() != "abc" {
			t.Fatalf("buffer content = %q, want %q", buffer.String(), "abc")
		}

		var nilBuffer *cappedBuffer
		if _, err := nilBuffer.Write([]byte("x")); err != nil {
			t.Fatalf("nil buffer Write() error = %v", err)
		}
		if nilBuffer.String() != "" {
			t.Fatalf("nil buffer String() should be empty")
		}
		if nilBuffer.Truncated() {
			t.Fatalf("nil buffer Truncated() should be false")
		}
	})
}
