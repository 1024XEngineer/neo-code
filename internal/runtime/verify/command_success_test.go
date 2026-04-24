package verify

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
)

type stubCommandExecutor struct {
	result   CommandExecutionResult
	err      error
	requests []CommandExecutionRequest
}

func (s *stubCommandExecutor) Execute(ctx context.Context, req CommandExecutionRequest) (CommandExecutionResult, error) {
	_ = ctx
	s.requests = append(s.requests, req)
	if s.err != nil {
		return CommandExecutionResult{}, s.err
	}
	return s.result, nil
}

func verifyConfigForCommandTests() config.VerificationConfig {
	cfg := config.StaticDefaults().Runtime.Verification
	cfg.Enabled = boolPtrVerify(true)
	return cfg
}

func boolPtrVerify(value bool) *bool {
	v := value
	return &v
}

func TestCommandSuccessVerifierName(t *testing.T) {
	t.Parallel()

	if got := (CommandSuccessVerifier{}).Name(); got != commandSuccessVerifierName {
		t.Fatalf("Name() = %q, want %q", got, commandSuccessVerifierName)
	}
	if got := (CommandSuccessVerifier{VerifierName: "  build  "}).Name(); got != "build" {
		t.Fatalf("Name() = %q, want build", got)
	}
}

func TestCommandSuccessVerifierVerifyFinalMissingCommand(t *testing.T) {
	t.Parallel()

	t.Run("required missing command returns soft_block", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Required = true
		verifierCfg.Command = ""
		verifierCfg.FailClosed = false
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationSoftBlock {
			t.Fatalf("status = %q, want %q", result.Status, VerificationSoftBlock)
		}
		if result.ErrorClass != ErrorClassEnvMissing {
			t.Fatalf("error_class = %q, want %q", result.ErrorClass, ErrorClassEnvMissing)
		}
	})

	t.Run("required missing command with fail_closed returns fail", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Required = true
		verifierCfg.Command = ""
		verifierCfg.FailClosed = true
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("required missing command with fail_open returns pass", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Required = true
		verifierCfg.Command = ""
		verifierCfg.FailOpen = true
		verifierCfg.FailClosed = false
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
		}
	})

	t.Run("optional missing command returns pass", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Required = false
		verifierCfg.Command = ""
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
		}
	})
}

func TestCommandSuccessVerifierVerifyFinalExecutionOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("denied error maps to permission denied", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{err: ErrVerificationExecutionDenied}
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Command = "go test ./..."
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassPermissionDenied {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("timeout execution error is retryable", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{err: errors.New("command timeout exceeded")}
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Command = "go test ./..."
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.ErrorClass != ErrorClassTimeout || !result.Retryable {
			t.Fatalf("unexpected timeout mapping: %+v", result)
		}
	})

	t.Run("exit code zero returns pass", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{CommandName: "go", ExitCode: 0, Stdout: " ok ", Stderr: " "}}
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Command = "go test ./..."
		verifierCfg.TimeoutSec = 30
		verifierCfg.OutputCapBytes = 2048
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{Workdir: "/workspace", VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
		}
		if len(executor.requests) != 1 {
			t.Fatalf("expected one execute call, got %d", len(executor.requests))
		}
		if executor.requests[0].Command != "go test ./..." || executor.requests[0].Workdir != "/workspace" {
			t.Fatalf("unexpected request: %+v", executor.requests[0])
		}
		if got := result.Evidence["stdout"]; got != "ok" {
			t.Fatalf("stdout evidence = %v, want ok", got)
		}
	})

	t.Run("named verifier falls back to command_success config", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 0}}
		cfg := verifyConfigForCommandTests()
		verifierCfg := cfg.Verifiers[commandSuccessVerifierName]
		verifierCfg.Command = "go version"
		cfg.Verifiers[commandSuccessVerifierName] = verifierCfg

		result, err := (CommandSuccessVerifier{VerifierName: "custom", Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Name != "custom" || result.Status != VerificationPass {
			t.Fatalf("unexpected result: %+v", result)
		}
		if len(executor.requests) != 1 || executor.requests[0].Command != "go version" {
			t.Fatalf("unexpected request: %+v", executor.requests)
		}
	})

	t.Run("non-zero exit maps verifier-specific error class", func(t *testing.T) {
		t.Parallel()
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 2}}
		cfg := verifyConfigForCommandTests()
		cfg.Verifiers["build"] = config.VerifierConfig{Enabled: true, Command: "go build ./..."}

		result, err := (CommandSuccessVerifier{VerifierName: "build", Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassCompileError {
			t.Fatalf("unexpected failure mapping: %+v", result)
		}
	})
}

func TestClassifyCommandExecutionError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "timeout", err: errors.New("timeout"), want: ErrorClassTimeout},
		{name: "not found", err: errors.New("binary not found"), want: ErrorClassCommandNotFound},
		{name: "permission", err: errors.New("permission denied"), want: ErrorClassPermissionDenied},
		{name: "unknown", err: errors.New("other"), want: ErrorClassUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyCommandExecutionError(tc.err); got != tc.want {
				t.Fatalf("classifyCommandExecutionError() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyCommandFailure(t *testing.T) {
	t.Parallel()

	if got := classifyCommandFailure("any", CommandExecutionResult{TimedOut: true}); got != ErrorClassTimeout {
		t.Fatalf("timed out class = %q, want %q", got, ErrorClassTimeout)
	}

	cases := []struct {
		name string
		want ErrorClass
	}{
		{name: "build", want: ErrorClassCompileError},
		{name: "test", want: ErrorClassTestFailure},
		{name: "lint", want: ErrorClassLintFailure},
		{name: "typecheck", want: ErrorClassTypeError},
		{name: "other", want: ErrorClassUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyCommandFailure(tc.name, CommandExecutionResult{}); got != tc.want {
				t.Fatalf("classifyCommandFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}
