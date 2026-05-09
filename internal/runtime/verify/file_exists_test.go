package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileExistsVerifier(t *testing.T) {
	t.Parallel()

	t.Run("path outside workdir fails", func(t *testing.T) {
		t.Parallel()
		result, err := (FileExistsVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: t.TempDir(),
			TaskState: TaskStateSnapshot{
				KeyArtifacts: []string{"../secret.txt"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want fail", result.Status)
		}
	})

	t.Run("missing artifact soft blocks", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		result, err := (FileExistsVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			TaskState: TaskStateSnapshot{
				KeyArtifacts: []string{"a.txt"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want soft_block", result.Status)
		}
	})

	t.Run("required todo artifact passes when file exists", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		path := filepath.Join(workdir, "a.txt")
		if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		result, err := (FileExistsVerifier{}).VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Todos: []TodoSnapshot{
				{ID: "todo-1", Required: true, Artifacts: []string{"a.txt"}},
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
