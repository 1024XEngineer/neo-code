package verify

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"neo-code/internal/config"
)

func verifyConfigForGitDiffTests() config.VerificationConfig {
	cfg := config.StaticDefaults().Runtime.Verification
	cfg.Enabled = boolPtrVerify(true)
	return cfg
}

func TestGitDiffVerifierVerifyFinal(t *testing.T) {
	t.Parallel()

	t.Run("execution error returns fail", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{err: errors.New("permission denied")}
		cfg := verifyConfigForGitDiffTests()

		result, err := (GitDiffVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassPermissionDenied {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("non zero exit code returns fail", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 1}}
		cfg := verifyConfigForGitDiffTests()

		result, err := (GitDiffVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("empty output returns fail", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 0, Stdout: "\n\t "}}
		cfg := verifyConfigForGitDiffTests()

		result, err := (GitDiffVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("changed files returns pass and evidence", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 0, Stdout: "a.go\n b.go \n\n"}}
		cfg := verifyConfigForGitDiffTests()
		verifierCfg := cfg.Verifiers[gitDiffVerifierName]
		verifierCfg.Command = "git diff --name-only"
		cfg.Verifiers[gitDiffVerifierName] = verifierCfg

		result, err := (GitDiffVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{Workdir: "/workspace", VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
		}
		if len(executor.requests) != 1 {
			t.Fatalf("expected one execute call, got %d", len(executor.requests))
		}
		if executor.requests[0].Command != "git diff --name-only" || executor.requests[0].Workdir != "/workspace" {
			t.Fatalf("unexpected request: %+v", executor.requests[0])
		}
		files, ok := result.Evidence["changed_files"].([]string)
		if !ok {
			t.Fatalf("changed_files should be []string, got %T", result.Evidence["changed_files"])
		}
		if !reflect.DeepEqual(files, []string{"a.go", "b.go"}) {
			t.Fatalf("changed_files = %#v, want %#v", files, []string{"a.go", "b.go"})
		}
	})

	t.Run("uses default command when verifier command empty", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForGitDiffTests()
		verifierCfg := cfg.Verifiers[gitDiffVerifierName]
		verifierCfg.Command = "   "
		cfg.Verifiers[gitDiffVerifierName] = verifierCfg

		result, err := (GitDiffVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir:            t.TempDir(),
			VerificationConfig: cfg,
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail && result.Status != VerificationPass {
			t.Fatalf("unexpected status %q", result.Status)
		}
	})
}

func TestGitDiffVerifierName(t *testing.T) {
	t.Parallel()
	if got := (GitDiffVerifier{}).Name(); got != gitDiffVerifierName {
		t.Fatalf("Name() = %q, want %q", got, gitDiffVerifierName)
	}
}

func TestNonEmptyLines(t *testing.T) {
	t.Parallel()

	got := nonEmptyLines("a\n\n b \n\t\n c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nonEmptyLines() = %#v, want %#v", got, want)
	}
}
