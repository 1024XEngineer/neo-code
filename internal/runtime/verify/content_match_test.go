package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContentMatchVerifierPathTraversalDenied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	outside := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outside, []byte("token"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	verifier := ContentMatchVerifier{}
	result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
		Workdir: workdir,
		Metadata: map[string]any{
			"content_match": map[string][]string{"../secret.txt": {"token"}},
		},
	})
	if err != nil {
		t.Fatalf("VerifyFinal() error = %v", err)
	}
	assertVerifierStatus(t, result, VerificationFail)
	assertPermissionDeniedClass(t, result)
}

func TestContentMatchVerifierSuccessWithinWorkdir(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	file := filepath.Join(workdir, "a.txt")
	if err := os.WriteFile(file, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	verifier := ContentMatchVerifier{}
	result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
		Workdir: workdir,
		Metadata: map[string]any{
			"content_match": map[string][]string{"a.txt": {"hello"}},
		},
	})
	if err != nil {
		t.Fatalf("VerifyFinal() error = %v", err)
	}
	assertVerifierStatus(t, result, VerificationPass)
}
