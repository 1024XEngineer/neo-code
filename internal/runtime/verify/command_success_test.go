package verify

import (
	"context"
	"fmt"
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
	return s.result, s.err
}

func verifyConfigForCommandTests() config.VerificationConfig {
	cfg := config.StaticDefaults().Runtime.Verification
	cfg.ExecutionPolicy = config.VerificationExecutionPolicyConfig{
		Mode:             "non_interactive",
		DefaultTimeout:   1,
		DefaultOutputCap: 1,
	}
	return cfg
}

func TestCommandSuccessVerifier(t *testing.T) {
	t.Parallel()

	t.Run("missing command fails closed", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		cfg.Verifiers[commandSuccessVerifierName] = config.VerifierConfig{}
		result, err := (CommandSuccessVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassEnvMissing {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("execution denied maps to fail", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		cfg.Verifiers[commandSuccessVerifierName] = config.VerifierConfig{Command: []string{"go", "test", "./..."}}
		executor := &stubCommandExecutor{err: fmt.Errorf("%w: blocked", ErrVerificationExecutionDenied)}
		result, err := (CommandSuccessVerifier{Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want fail", result.Status)
		}
	})

	t.Run("non-zero exit becomes soft block", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		cfg.Verifiers["build"] = config.VerifierConfig{Command: []string{"go", "build", "./..."}}
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 1}}
		result, err := (CommandSuccessVerifier{VerifierName: "build", Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassCompileError {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("zero exit passes and forwards argv", func(t *testing.T) {
		t.Parallel()
		cfg := verifyConfigForCommandTests()
		cfg.Verifiers["test"] = config.VerifierConfig{Command: []string{"go", "test", "./..."}}
		executor := &stubCommandExecutor{result: CommandExecutionResult{ExitCode: 0}}
		result, err := (CommandSuccessVerifier{VerifierName: "test", Executor: executor}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir:            "/workspace",
			VerificationConfig: cfg,
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want pass", result.Status)
		}
		if len(executor.requests) != 1 || len(executor.requests[0].Argv) != 3 {
			t.Fatalf("unexpected request argv: %+v", executor.requests)
		}
	})
}
