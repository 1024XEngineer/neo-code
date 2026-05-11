package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContentMatchVerifier(t *testing.T) {
	t.Parallel()

	t.Run("empty rules pass", func(t *testing.T) {
		t.Parallel()
		result, err := (ContentMatchVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want pass", result.Status)
		}
	})

	t.Run("invalid rule fails", func(t *testing.T) {
		t.Parallel()
		result, err := (ContentMatchVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: t.TempDir(),
			Todos: []TodoSnapshot{
				{ID: "todo-1", Required: true, ContentChecks: []TodoContentCheckSnapshot{{Artifact: "a.txt"}}},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want fail", result.Status)
		}
	})

	t.Run("missing token soft blocks", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		path := filepath.Join(workdir, "a.txt")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		result, err := (ContentMatchVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Todos: []TodoSnapshot{
				{ID: "todo-1", Required: true, ContentChecks: []TodoContentCheckSnapshot{{Artifact: "a.txt", Contains: []string{"world"}}}},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want soft_block", result.Status)
		}
	})

	t.Run("matching tokens pass", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		path := filepath.Join(workdir, "a.txt")
		if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		result, err := (ContentMatchVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Todos: []TodoSnapshot{
				{ID: "todo-1", Required: true, ContentChecks: []TodoContentCheckSnapshot{{Artifact: "a.txt", Contains: []string{"hello", "world"}}}},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want pass", result.Status)
		}
	})
}
