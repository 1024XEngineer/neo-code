package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSummaryReturnsStableEmptyForNonGitDirectory(t *testing.T) {
	t.Parallel()

	service := newTestService(func(ctx context.Context, workdir string, args ...string) (string, error) {
		return "fatal: not a git repository", errors.New("exit status 128")
	})

	summary, err := service.Summary(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.InGitRepo {
		t.Fatalf("expected non-git summary, got %+v", summary)
	}
}

func TestSummaryParsesBranchDirtyAheadBehindAndRepresentativeFiles(t *testing.T) {
	t.Parallel()

	service := newTestService(func(ctx context.Context, workdir string, args ...string) (string, error) {
		return strings.Join([]string{
			"## feature/repository...origin/feature/repository [ahead 2, behind 1]",
			" M internal/context/source_system.go",
			"R  old/name.go -> new/name.go",
			"?? internal/repository/service.go",
		}, "\n"), nil
	})

	summary, err := service.Summary(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if !summary.InGitRepo || !summary.Dirty {
		t.Fatalf("expected git repo summary, got %+v", summary)
	}
	if summary.Branch != "feature/repository" {
		t.Fatalf("expected branch parsed, got %q", summary.Branch)
	}
	if summary.Ahead != 2 || summary.Behind != 1 {
		t.Fatalf("expected ahead=2 behind=1, got %+v", summary)
	}
	if summary.ChangedFileCount != 3 {
		t.Fatalf("expected 3 changed files, got %d", summary.ChangedFileCount)
	}
	expected := []string{
		filepath.Clean("internal/context/source_system.go"),
		filepath.Clean("new/name.go"),
		filepath.Clean("internal/repository/service.go"),
	}
	for index, path := range expected {
		if summary.RepresentativeChangedFiles[index] != path {
			t.Fatalf("expected representative path %q, got %q", path, summary.RepresentativeChangedFiles[index])
		}
	}
}

func TestChangedFilesRespectsStatusNormalizationAndSnippetRules(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "pkg", "changed.go"), []byte("package pkg\n\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "pkg", "new.go"), []byte("package pkg\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "pkg", "untracked.go"), []byte("package pkg\n\nfunc Untracked() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "pkg", "renamed.go"), []byte("package pkg\n\nfunc Renamed() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	service := newTestService(func(ctx context.Context, dir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "status --porcelain=v1 --branch --untracked-files=normal":
			return strings.Join([]string{
				"## main...origin/main [ahead 1]",
				" M pkg/changed.go",
				"A  pkg/new.go",
				"?? pkg/untracked.go",
				"D  pkg/deleted.go",
				"R  pkg/old.go -> pkg/renamed.go",
				"UU pkg/conflicted.go",
			}, "\n"), nil
		case "diff --unified=3 HEAD -- pkg/changed.go":
			return "@@ -1,1 +1,1 @@\n-func Old() {}\n+func Changed() {}\n", nil
		case "diff --unified=3 HEAD -- pkg/new.go":
			return "@@ -0,0 +1,3 @@\n+package pkg\n+\n+func Added() {}\n", nil
		case "diff --unified=3 HEAD -- pkg/renamed.go":
			return "", nil
		default:
			return "", nil
		}
	})

	ctx, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets: true,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if ctx.TotalCount != 6 || ctx.ReturnedCount != 6 {
		t.Fatalf("unexpected count summary: %+v", ctx)
	}
	assertChangedFile(t, ctx.Files[0], filepath.Clean("pkg/changed.go"), "", StatusModified, "Changed")
	assertChangedFile(t, ctx.Files[1], filepath.Clean("pkg/new.go"), "", StatusAdded, "Added")
	assertChangedFile(t, ctx.Files[2], filepath.Clean("pkg/untracked.go"), "", StatusUntracked, "Untracked")
	assertChangedFile(t, ctx.Files[3], filepath.Clean("pkg/deleted.go"), "", StatusDeleted, "")
	assertChangedFile(t, ctx.Files[4], filepath.Clean("pkg/renamed.go"), filepath.Clean("pkg/old.go"), StatusRenamed, "")
	assertChangedFile(t, ctx.Files[5], filepath.Clean("pkg/conflicted.go"), "", StatusConflicted, "")
}

func TestChangedFilesAppliesLimitAndTruncation(t *testing.T) {
	t.Parallel()

	service := newTestService(func(ctx context.Context, workdir string, args ...string) (string, error) {
		lines := []string{"## main"}
		for i := 0; i < 60; i++ {
			lines = append(lines, " M file"+strconv.Itoa(i)+".go")
		}
		return strings.Join(lines, "\n"), nil
	})

	result, err := service.ChangedFiles(context.Background(), t.TempDir(), ChangedFilesOptions{})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if !result.Truncated {
		t.Fatalf("expected truncation for oversized changed files list")
	}
	if result.ReturnedCount != defaultChangedFilesLimit {
		t.Fatalf("expected default limit %d, got %d", defaultChangedFilesLimit, result.ReturnedCount)
	}
}

func TestChangedFilesMarksTruncatedWhenSingleSnippetExceedsLineLimit(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "long.go"), "package pkg\n")
	service := newTestService(func(ctx context.Context, workdir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "status --porcelain=v1 --branch --untracked-files=normal":
			return "## main\n M pkg/long.go\n", nil
		case "diff --unified=3 HEAD -- pkg/long.go":
			lines := []string{"@@ -1,1 +1,25 @@"}
			for i := 0; i < 25; i++ {
				lines = append(lines, "+line "+strconv.Itoa(i))
			}
			return strings.Join(lines, "\n"), nil
		default:
			return "", nil
		}
	})

	result, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets: true,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if !result.Truncated {
		t.Fatalf("expected snippet truncation to set Truncated")
	}
	if got := len(splitNonEmptyLines(result.Files[0].Snippet)); got != maxChangedSnippetLinesPerFile {
		t.Fatalf("expected snippet to be trimmed to %d lines, got %d", maxChangedSnippetLinesPerFile, got)
	}
}

func TestChangedFilesMarksTruncatedWhenTotalSnippetBudgetExceeded(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	lines := make([]string, 0, maxChangedSnippetLinesPerFile+2)
	lines = append(lines, "package pkg")
	for i := 0; i < maxChangedSnippetLinesPerFile+1; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n")

	statusLines := []string{"## main"}
	for i := 0; i < 11; i++ {
		fileName := filepath.Join("pkg", "file"+strconv.Itoa(i)+".txt")
		mustWriteFile(t, filepath.Join(workdir, fileName), content)
		statusLines = append(statusLines, "?? "+filepath.ToSlash(fileName))
	}

	service := newTestService(func(ctx context.Context, dir string, args ...string) (string, error) {
		if strings.Join(args, " ") == "status --porcelain=v1 --branch --untracked-files=normal" {
			return strings.Join(statusLines, "\n"), nil
		}
		return "", nil
	})

	result, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets: true,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if !result.Truncated {
		t.Fatalf("expected total snippet budget truncation to set Truncated")
	}
	last := result.Files[len(result.Files)-1]
	if last.Snippet != "" {
		t.Fatalf("expected last snippet to be dropped after total budget is exhausted, got %q", last.Snippet)
	}
}

func TestChangedFilesBlocksSensitiveLargeAndBinarySnippets(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, ".env"), "API_KEY=secret\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "cert.pem"), "-----BEGIN PRIVATE KEY-----\nsecret\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "bin.dat"), string([]byte{0x00, 0x01, 0x02}))
	mustWriteFile(t, filepath.Join(workdir, "pkg", "large.txt"), strings.Repeat("x", maxRepositorySnippetFileBytes+1))

	service := newTestService(func(ctx context.Context, dir string, args ...string) (string, error) {
		if strings.Join(args, " ") == "status --porcelain=v1 --branch --untracked-files=normal" {
			return strings.Join([]string{
				"## main",
				"?? .env",
				"?? pkg/cert.pem",
				"?? pkg/bin.dat",
				"?? pkg/large.txt",
			}, "\n"), nil
		}
		return "", nil
	})

	result, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets: true,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	for _, file := range result.Files {
		if file.Snippet != "" {
			t.Fatalf("expected filtered file to have empty snippet, got %+v", file)
		}
	}
}

func TestChangedFilesBlocksModifiedSensitiveDiffSnippet(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, ".env"), "API_KEY=secret\n")

	service := newTestService(func(ctx context.Context, dir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "status --porcelain=v1 --branch --untracked-files=normal":
			return "## main\n M .env\n", nil
		case "diff --unified=3 HEAD -- .env":
			return "@@ -1,1 +1,1 @@\n-API_KEY=old\n+API_KEY=new\n", nil
		default:
			return "", nil
		}
	})

	result, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets: true,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected single changed file, got %+v", result.Files)
	}
	if result.Files[0].Snippet != "" {
		t.Fatalf("expected sensitive modified file to have empty snippet, got %+v", result.Files[0])
	}
}

func TestChangedFilesRespectsSnippetFileCountLimit(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "one.go"), "package pkg\n\nfunc One() {}\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "two.go"), "package pkg\n\nfunc Two() {}\n")

	service := newTestService(func(ctx context.Context, dir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "status --porcelain=v1 --branch --untracked-files=normal":
			return "## main\n?? pkg/one.go\n?? pkg/two.go\n", nil
		default:
			return "", nil
		}
	})

	allowed, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets:       true,
		SnippetFileCountLimit: 2,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() allow error = %v", err)
	}
	if allowed.Files[0].Snippet == "" || allowed.Files[1].Snippet == "" {
		t.Fatalf("expected snippets when total count does not exceed limit, got %+v", allowed.Files)
	}

	blocked, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{
		IncludeSnippets:       true,
		SnippetFileCountLimit: 1,
	})
	if err != nil {
		t.Fatalf("ChangedFiles() block error = %v", err)
	}
	if blocked.Files[0].Snippet != "" || blocked.Files[1].Snippet != "" {
		t.Fatalf("expected snippets to be suppressed after count limit, got %+v", blocked.Files)
	}
}

func TestSummaryReturnsErrorForUnexpectedGitFailure(t *testing.T) {
	t.Parallel()

	service := newTestService(func(ctx context.Context, workdir string, args ...string) (string, error) {
		return "fatal: permission denied", errors.New("exit status 128")
	})

	_, err := service.Summary(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("expected unexpected git failure to be returned")
	}
}

func TestRetrieveSupportsPathGlobTextAndSymbol(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "target.go"), "package pkg\n\ntype Widget struct{}\n\nfunc BuildWidget() Widget {\n\treturn Widget{}\n}\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "notes.txt"), "Widget appears here too\n")

	service := NewService()

	pathHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModePath,
		Value: "pkg/target.go",
	})
	if err != nil {
		t.Fatalf("Retrieve(path) error = %v", err)
	}
	if len(pathHits) != 1 || pathHits[0].Kind != string(RetrievalModePath) {
		t.Fatalf("unexpected path hits: %+v", pathHits)
	}

	globHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeGlob,
		Value: "*.go",
	})
	if err != nil {
		t.Fatalf("Retrieve(glob) error = %v", err)
	}
	if len(globHits) == 0 {
		t.Fatalf("expected glob hits")
	}

	textHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeText,
		Value: "Widget",
	})
	if err != nil {
		t.Fatalf("Retrieve(text) error = %v", err)
	}
	if len(textHits) < 2 {
		t.Fatalf("expected text hits across files, got %+v", textHits)
	}

	symbolHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeSymbol,
		Value: "BuildWidget",
	})
	if err != nil {
		t.Fatalf("Retrieve(symbol) error = %v", err)
	}
	if len(symbolHits) != 1 || symbolHits[0].LineHint <= 0 {
		t.Fatalf("unexpected symbol hits: %+v", symbolHits)
	}
}

func TestRetrieveRejectsPathEscapeAndSymlinkEscape(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join(workdir, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	linkPath := filepath.Join(workdir, "pkg", "outside.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	service := NewService()

	_, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModePath,
		Value: "..\\outside.txt",
	})
	if err == nil {
		t.Fatalf("expected path traversal to be rejected")
	}

	_, err = service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModePath,
		Value: "pkg/outside.txt",
	})
	if err == nil {
		t.Fatalf("expected symlink escape to be rejected")
	}
}

func TestRetrieveSymbolFallsBackToWholeWordTextSearch(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "notes.txt"), "searchWidget searchWidget\n")

	service := NewService()
	hits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeSymbol,
		Value: "searchWidget",
	})
	if err != nil {
		t.Fatalf("Retrieve(symbol fallback) error = %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected fallback whole-word hit, got %+v", hits)
	}
}

func TestRetrieveSkipsSensitiveLargeAndBinaryFiles(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, ".env"), "API_KEY=secret\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "notes.key"), "private")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "bin.dat"), string([]byte{0x00, 0x01, 0x02, 0x03}))
	mustWriteFile(t, filepath.Join(workdir, "pkg", "target.txt"), "match line\n")

	largeContent := strings.Repeat("x", maxRepositorySnippetFileBytes+1)
	mustWriteFile(t, filepath.Join(workdir, "pkg", "large.txt"), largeContent)

	service := NewService()

	pathHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModePath,
		Value: ".env",
	})
	if err != nil {
		t.Fatalf("Retrieve(path sensitive) error = %v", err)
	}
	if len(pathHits) != 0 {
		t.Fatalf("expected sensitive path retrieval to be filtered, got %+v", pathHits)
	}

	textHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeText,
		Value: "match",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Retrieve(text) error = %v", err)
	}
	if len(textHits) != 1 || textHits[0].Path != filepath.Clean("pkg/target.txt") {
		t.Fatalf("expected only safe text hit, got %+v", textHits)
	}

	globHits, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
		Mode:  RetrievalModeGlob,
		Value: "pkg/*",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Retrieve(glob) error = %v", err)
	}
	for _, hit := range globHits {
		if hit.Path == filepath.Clean("pkg/large.txt") || hit.Path == filepath.Clean("pkg/notes.key") || hit.Path == filepath.Clean("pkg/bin.dat") {
			t.Fatalf("expected filtered file to be excluded, got %+v", globHits)
		}
	}
}

func assertChangedFile(t *testing.T, file ChangedFile, path string, oldPath string, status ChangedFileStatus, snippetContains string) {
	t.Helper()
	if file.Path != path || file.OldPath != oldPath || file.Status != status {
		t.Fatalf("unexpected changed file: %+v", file)
	}
	if snippetContains == "" {
		if file.Snippet != "" {
			t.Fatalf("expected empty snippet, got %q", file.Snippet)
		}
		return
	}
	if !strings.Contains(file.Snippet, snippetContains) {
		t.Fatalf("expected snippet to contain %q, got %q", snippetContains, file.Snippet)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func newTestService(gitRunner func(ctx context.Context, workdir string, args ...string) (string, error)) *Service {
	return &Service{
		gitRunner: gitRunner,
		readFile:  readFile,
	}
}
