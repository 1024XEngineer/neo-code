package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadGitDiffFile(t *testing.T) {
	t.Parallel()

	const statusOutput = "## main\x00 M pkg/changed.go\x00R  pkg/renamed.go\x00pkg/old.go\x00A  pkg/new.go\x00D  pkg/deleted.go\x00?? pkg/untracked.go\x00UU pkg/conflicted.go\x00"

	newGitDiffService := func() *Service {
		return &Service{
			gitRunner: func(ctx context.Context, workdir string, opts GitCommandOptions, args ...string) (GitCommandOutput, error) {
				return GitCommandOutput{Text: statusOutput}, nil
			},
			gitBlobSizer: func(ctx context.Context, workdir string, spec string) (int64, error) {
				switch spec {
				case "HEAD:pkg/changed.go":
					return int64(len("before changed")), nil
				case "HEAD:pkg/old.go":
					return int64(len("before rename")), nil
				case "HEAD:pkg/deleted.go":
					return int64(len("before deleted")), nil
				case "HEAD:pkg/conflicted.go":
					return int64(len("before conflict")), nil
				default:
					return 0, errors.New("missing blob")
				}
			},
			gitBlobReader: func(ctx context.Context, workdir string, spec string) ([]byte, error) {
				switch spec {
				case "HEAD:pkg/changed.go":
					return []byte("before changed"), nil
				case "HEAD:pkg/old.go":
					return []byte("before rename"), nil
				case "HEAD:pkg/deleted.go":
					return []byte("before deleted"), nil
				case "HEAD:pkg/conflicted.go":
					return []byte("before conflict"), nil
				default:
					return nil, errors.New("missing blob")
				}
			},
			readFile: func(path string) ([]byte, error) {
				switch {
				case strings.HasSuffix(path, "pkg\\changed.go") || strings.HasSuffix(path, "pkg/changed.go"):
					return []byte("after changed"), nil
				case strings.HasSuffix(path, "pkg\\renamed.go") || strings.HasSuffix(path, "pkg/renamed.go"):
					return []byte("after rename"), nil
				case strings.HasSuffix(path, "pkg\\new.go") || strings.HasSuffix(path, "pkg/new.go"):
					return []byte("after new"), nil
				case strings.HasSuffix(path, "pkg\\untracked.go") || strings.HasSuffix(path, "pkg/untracked.go"):
					return []byte("after untracked"), nil
				case strings.HasSuffix(path, "pkg\\conflicted.go") || strings.HasSuffix(path, "pkg/conflicted.go"):
					return []byte("after conflict"), nil
				default:
					return nil, errors.New("missing file")
				}
			},
		}
	}

	t.Run("returns modified file with original and modified content", func(t *testing.T) {
		service := newGitDiffService()
		workdir := t.TempDir()
		mustWriteRepositoryFile(t, workdir+"/pkg/changed.go", "after changed")

		result, err := service.ReadGitDiffFile(context.Background(), workdir, "pkg/changed.go", 1024)
		if err != nil {
			t.Fatalf("ReadGitDiffFile() error = %v", err)
		}
		if result.Status != StatusModified || result.OriginalContent != "before changed" || result.ModifiedContent != "after changed" {
			t.Fatalf("unexpected modified diff result: %+v", result)
		}
	})

	t.Run("returns renamed file with old path as original source", func(t *testing.T) {
		service := newGitDiffService()
		workdir := t.TempDir()
		mustWriteRepositoryFile(t, workdir+"/pkg/renamed.go", "after rename")

		result, err := service.ReadGitDiffFile(context.Background(), workdir, "pkg/renamed.go", 1024)
		if err != nil {
			t.Fatalf("ReadGitDiffFile() error = %v", err)
		}
		if result.Status != StatusRenamed || result.OldPath != filepath.Clean("pkg/old.go") || result.OriginalContent != "before rename" || result.ModifiedContent != "after rename" {
			t.Fatalf("unexpected renamed diff result: %+v", result)
		}
	})

	t.Run("returns deleted file with empty modified content", func(t *testing.T) {
		service := newGitDiffService()
		workdir := t.TempDir()

		result, err := service.ReadGitDiffFile(context.Background(), workdir, "pkg/deleted.go", 1024)
		if err != nil {
			t.Fatalf("ReadGitDiffFile() error = %v", err)
		}
		if result.Status != StatusDeleted || result.OriginalContent != "before deleted" || result.ModifiedContent != "" {
			t.Fatalf("unexpected deleted diff result: %+v", result)
		}
	})

	t.Run("marks oversized files as truncated", func(t *testing.T) {
		service := newGitDiffService()
		workdir := t.TempDir()
		mustWriteRepositoryFile(t, workdir+"/pkg/changed.go", "after changed")

		result, err := service.ReadGitDiffFile(context.Background(), workdir, "pkg/changed.go", 4)
		if err != nil {
			t.Fatalf("ReadGitDiffFile() error = %v", err)
		}
		if !result.Truncated || result.OriginalContent != "" || result.ModifiedContent != "" {
			t.Fatalf("expected truncated diff result, got %+v", result)
		}
	})

	t.Run("reads expanded files from untracked directories", func(t *testing.T) {
		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, opts GitCommandOptions, args ...string) (GitCommandOutput, error) {
				return GitCommandOutput{Text: "## main\x00?? handwrite_res\x00"}, nil
			},
			readFile: readFile,
		}
		workdir := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workdir, "handwrite_res", "nested", "result.txt"), "expanded content\n")

		result, err := service.ReadGitDiffFile(context.Background(), workdir, "handwrite_res/nested/result.txt", 1024)
		if err != nil {
			t.Fatalf("ReadGitDiffFile() error = %v", err)
		}
		if result.Status != StatusUntracked || result.ModifiedContent != "expanded content\n" {
			t.Fatalf("unexpected expanded untracked diff result: %+v", result)
		}
	})

	t.Run("returns explicit directory error for git diff directory path", func(t *testing.T) {
		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, opts GitCommandOptions, args ...string) (GitCommandOutput, error) {
				return GitCommandOutput{Text: "## main\x00?? handwrite_res\x00"}, nil
			},
			readFile: readFile,
		}
		workdir := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workdir, "handwrite_res", "nested", "result.txt"), "expanded content\n")

		_, err := service.ReadGitDiffFile(context.Background(), workdir, "handwrite_res", 1024)
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Fatalf("expected directory error, got %v", err)
		}
	})
}
