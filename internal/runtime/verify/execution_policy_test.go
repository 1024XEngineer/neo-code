package verify

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
)

func defaultExecutionPolicy() config.VerificationExecutionPolicyConfig {
	return config.StaticDefaults().Runtime.Verification.ExecutionPolicy
}

func assertExecutionDenied(t *testing.T, command string) {
	t.Helper()

	_, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{
		Command: command,
		Policy:  defaultExecutionPolicy(),
	})
	if err == nil {
		t.Fatal("expected command to be denied")
	}
	if !errors.Is(err, ErrVerificationExecutionDenied) {
		t.Fatalf("error = %v, want ErrVerificationExecutionDenied", err)
	}
}

func TestPolicyCommandExecutorAllowsWhitelistedCommand(t *testing.T) {
	t.Parallel()

	result, err := PolicyCommandExecutor{}.Execute(context.Background(), CommandExecutionRequest{
		Command: "go version",
		Policy:  defaultExecutionPolicy(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", result.ExitCode)
	}
}

func TestPolicyCommandExecutorRejectsDeniedCommand(t *testing.T) {
	t.Parallel()

	assertExecutionDenied(t, "rm -rf .")
}

func TestPolicyCommandExecutorRejectsGitWriteSubcommand(t *testing.T) {
	t.Parallel()

	assertExecutionDenied(t, "git checkout .")
}

func TestPolicyCommandExecutorRejectsShellMetacharacter(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"go test ./...; rm -rf .",
		"git diff && git checkout .",
		"echo $HOME",
	}
	for _, command := range testCases {
		command := command
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			assertExecutionDenied(t, command)
		})
	}
}
