package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func assertVerifierStatus(t *testing.T, result VerificationResult, want VerificationStatus) {
	t.Helper()
	if result.Status != want {
		t.Fatalf("status = %q, want %q", result.Status, want)
	}
}

func assertPermissionDeniedClass(t *testing.T, result VerificationResult) {
	t.Helper()
	if result.ErrorClass != ErrorClassPermissionDenied {
		t.Fatalf("error_class = %q, want %q", result.ErrorClass, ErrorClassPermissionDenied)
	}
}

func TestFileExistsVerifierPathTraversalDenied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	outside := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	verifier := FileExistsVerifier{}
	result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
		Workdir:  workdir,
		Metadata: map[string]any{"expected_files": []string{"../secret.txt"}},
	})
	if err != nil {
		t.Fatalf("VerifyFinal() error = %v", err)
	}
	assertVerifierStatus(t, result, VerificationFail)
	assertPermissionDeniedClass(t, result)
}

func TestFileExistsVerifierSuccessWithinWorkdir(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	verifier := FileExistsVerifier{}
	result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
		Workdir:  workdir,
		Metadata: map[string]any{"expected_files": []string{"a.txt"}},
	})
	if err != nil {
		t.Fatalf("VerifyFinal() error = %v", err)
	}
	assertVerifierStatus(t, result, VerificationPass)
}
