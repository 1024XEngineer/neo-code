package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRepositoryServiceSummaryChangedFilesAndRetrieve(t *testing.T) {
	t.Parallel()

	t.Run("summary non git and parsed git snapshot", func(t *testing.T) {
		t.Parallel()

		service := newRepositoryTestService(func(ctx context.Context, workdir string, args ...string) (GitCommandOutput, error) {
			return GitCommandOutput{Text: "fatal: not a git repository"}, errors.New("exit status 128")
		})
		summary, err := service.Summary(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("Summary() error = %v", err)
		}
		if summary.InGitRepo {
			t.Fatalf("expected non-git summary, got %+v", summary)
		}

		service = newRepositoryTestService(func(ctx context.Context, workdir string, args ...string) (GitCommandOutput, error) {
			return GitCommandOutput{Text: nulJoin(
				"## feature/repository...origin/feature/repository [ahead 2, behind 1]",
				" M pkg/changed.go",
				"R  pkg/new.go",
				"pkg/old.go",
				"?? pkg/untracked.go",
			)}, nil
		})
		summary, err = service.Summary(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("Summary() error = %v", err)
		}
		if !summary.InGitRepo || !summary.Dirty {
			t.Fatalf("expected git repo summary, got %+v", summary)
		}
		if summary.Branch != "feature/repository" || summary.Ahead != 2 || summary.Behind != 1 {
			t.Fatalf("unexpected summary counters: %+v", summary)
		}
		if summary.ChangedFileCount != 3 {
			t.Fatalf("expected 3 changed files, got %d", summary.ChangedFileCount)
		}
	})

	t.Run("inspect reuses snapshot and changed files respect snippet rules", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "changed.go"), "package pkg\n\nfunc Changed() {}\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "new.go"), "package pkg\n\nfunc Added() {}\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "untracked.go"), "package pkg\n\nfunc Untracked() {}\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "renamed.go"), "package pkg\n\nfunc Renamed() {}\n")

		calls := 0
		service := newRepositoryTestService(func(ctx context.Context, dir string, args ...string) (GitCommandOutput, error) {
			calls++
			switch strings.Join(args, " ") {
			case "status --porcelain=v1 -z --branch --untracked-files=normal":
				return GitCommandOutput{Text: nulJoin(
					"## main...origin/main [ahead 1]",
					" M pkg/changed.go",
					"A  pkg/new.go",
					"?? pkg/untracked.go",
					"D  pkg/deleted.go",
					"R  pkg/renamed.go",
					"pkg/old.go",
					"C  pkg/copied.go",
					"pkg/source.go",
					"UU pkg/conflicted.go",
				)}, nil
			case "diff --unified=3 HEAD -- pkg/changed.go":
				return GitCommandOutput{Text: "@@ -1,1 +1,1 @@\n-func Old() {}\n+func Changed() {}\n"}, nil
			case "diff --unified=3 HEAD -- pkg/new.go":
				return GitCommandOutput{Text: "@@ -0,0 +1,3 @@\n+package pkg\n+\n+func Added() {}\n"}, nil
			case "diff --unified=3 HEAD -- pkg/renamed.go":
				return GitCommandOutput{}, nil
			default:
				return GitCommandOutput{}, nil
			}
		})

		result, err := service.Inspect(context.Background(), workdir, InspectOptions{
			ChangedFilesLimit:                10,
			IncludeChangedFileSnippets:       true,
			ChangedFileSnippetFileCountLimit: 10,
		})
		if err != nil {
			t.Fatalf("Inspect() error = %v", err)
		}
		if calls != 3 {
			t.Fatalf("expected one status + two diff snippet calls, got %d", calls)
		}
		if result.Summary.Branch != "main" || result.ChangedFiles.TotalCount != 7 {
			t.Fatalf("unexpected inspect result: %+v", result)
		}
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[0], filepath.Clean("pkg/changed.go"), "", StatusModified, "Changed")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[1], filepath.Clean("pkg/new.go"), "", StatusAdded, "Added")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[2], filepath.Clean("pkg/untracked.go"), "", StatusUntracked, "Untracked")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[3], filepath.Clean("pkg/deleted.go"), "", StatusDeleted, "")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[4], filepath.Clean("pkg/renamed.go"), filepath.Clean("pkg/old.go"), StatusRenamed, "")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[5], filepath.Clean("pkg/copied.go"), filepath.Clean("pkg/source.go"), StatusCopied, "")
		assertChangedRepositoryFile(t, result.ChangedFiles.Files[6], filepath.Clean("pkg/conflicted.go"), "", StatusConflicted, "")
	})

	t.Run("changed files truncation and snippet filters", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		lines := []string{"package pkg"}
		for i := 0; i < maxChangedSnippetLinesPerFile+1; i++ {
			lines = append(lines, fmt.Sprintf("line %d", i))
		}
		content := strings.Join(lines, "\n")
		for i := 0; i < 11; i++ {
			mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", fmt.Sprintf("file%d.txt", i)), content)
		}
		mustWriteRepositoryFile(t, filepath.Join(workdir, ".env"), "API_KEY=secret\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "bin.dat"), string([]byte{0x00, 0x01}))
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "large.txt"), strings.Repeat("x", maxRepositorySnippetFileBytes+1))

		service := newRepositoryTestService(func(ctx context.Context, dir string, args ...string) (GitCommandOutput, error) {
			if strings.Join(args, " ") != "status --porcelain=v1 -z --branch --untracked-files=normal" {
				return GitCommandOutput{}, nil
			}
			records := []string{"## main", "?? .env", "?? pkg/bin.dat", "?? pkg/large.txt"}
			for i := 0; i < 11; i++ {
				records = append(records, "?? "+filepath.ToSlash(filepath.Join("pkg", fmt.Sprintf("file%d.txt", i))))
			}
			return GitCommandOutput{Text: nulJoin(records...)}, nil
		})

		got, err := service.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{IncludeSnippets: true})
		if err != nil {
			t.Fatalf("ChangedFiles() error = %v", err)
		}
		if !got.Truncated {
			t.Fatalf("expected truncation after total snippet budget exhaustion")
		}
		for _, file := range got.Files[:3] {
			if file.Snippet != "" {
				t.Fatalf("expected filtered file to have empty snippet, got %+v", file)
			}
		}
		if got.Files[len(got.Files)-1].Snippet != "" {
			t.Fatalf("expected last snippet to be dropped after budget exhaustion, got %+v", got.Files[len(got.Files)-1])
		}
	})

	t.Run("retrieve path glob text symbol and guards", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "target.go"), "package pkg\n\ntype Widget struct{}\n\nfunc BuildWidget() Widget {\n\treturn Widget{}\n}\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "notes.txt"), "Widget appears here too\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "word.txt"), "Food Foo FooBar\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, ".env"), "SECRET=1\n")

		service := NewService()

		pathResult, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalModePath,
			Value: "pkg/target.go",
		})
		if err != nil || len(pathResult.Hits) != 1 || pathResult.Hits[0].Kind != string(RetrievalModePath) {
			t.Fatalf("unexpected path result: (%+v, %v)", pathResult, err)
		}

		globResult, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalModeGlob,
			Value: "*.go",
		})
		if err != nil || len(globResult.Hits) == 0 {
			t.Fatalf("Retrieve(glob) = (%+v, %v), want hits", globResult, err)
		}

		textResult, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalModeText,
			Value: "Widget",
		})
		if err != nil || len(textResult.Hits) < 2 {
			t.Fatalf("Retrieve(text) = (%+v, %v), want hits", textResult, err)
		}

		symbolResult, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalModeSymbol,
			Value: "BuildWidget",
		})
		if err != nil || len(symbolResult.Hits) != 1 || symbolResult.Hits[0].LineHint <= 0 {
			t.Fatalf("Retrieve(symbol) = (%+v, %v), want one symbol hit", symbolResult, err)
		}

		fallbackResult, err := service.retrieveBySymbol(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:         RetrievalModeSymbol,
			Value:        "Foo",
			Limit:        5,
			ContextLines: 1,
		})
		if err != nil {
			t.Fatalf("retrieveBySymbol fallback error = %v", err)
		}
		if len(fallbackResult.Hits) != 1 || !strings.Contains(fallbackResult.Hits[0].Snippet, "Food Foo FooBar") {
			t.Fatalf("unexpected whole-word fallback hits: %+v", fallbackResult.Hits)
		}
		for _, hit := range fallbackResult.Hits {
			if hit.Kind != string(RetrievalModeSymbol) {
				t.Fatalf("expected symbol fallback kind, got %+v", hit)
			}
		}

		filteredPath, err := service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalModePath,
			Value: ".env",
		})
		if err != nil || len(filteredPath.Hits) != 0 {
			t.Fatalf("expected sensitive file to be filtered, got (%+v, %v)", filteredPath, err)
		}

		_, err = service.Retrieve(context.Background(), workdir, RetrievalQuery{
			Mode:  RetrievalMode("invalid"),
			Value: "x",
		})
		if !errors.Is(err, errInvalidMode) {
			t.Fatalf("expected invalid mode error, got %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := service.Retrieve(ctx, workdir, RetrievalQuery{Mode: RetrievalModeText, Value: "Widget"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled retrieve, got %v", err)
		}
	})
}

func TestRepositoryHelpersAndGitParsing(t *testing.T) {
	t.Parallel()

	t.Run("git parsing helpers", func(t *testing.T) {
		t.Parallel()

		if branch, ahead, behind := parseBranchLine(""); branch != "" || ahead != 0 || behind != 0 {
			t.Fatalf("parseBranchLine(empty) = (%q,%d,%d)", branch, ahead, behind)
		}
		if branch, _, _ := parseBranchLine("HEAD (no branch)"); branch != "detached" {
			t.Fatalf("parseBranchLine(detached) = %q", branch)
		}
		if branch, ahead, behind := parseBranchLine("main [ahead nope, behind 3]"); branch != "main" || ahead != 0 || behind != 3 {
			t.Fatalf("parseBranchLine(invalid tracking) = (%q,%d,%d)", branch, ahead, behind)
		}
		if ahead, behind := parseTrackingCounters("main [ahead 2, weird, behind 1, ahead nope]"); ahead != 2 || behind != 1 {
			t.Fatalf("parseTrackingCounters() = (%d,%d)", ahead, behind)
		}
		if got := splitNulRecords("a\x00b\x00\x00"); !slices.Equal(got, []string{"a", "b"}) {
			t.Fatalf("splitNulRecords() = %#v", got)
		}

		tests := []struct {
			records  []string
			ok       bool
			consumed int
		}{
			{records: nil, consumed: 1},
			{records: []string{"?? pkg/new.go"}, ok: true, consumed: 1},
			{records: []string{"R  new.go", "old.go"}, ok: true, consumed: 2},
			{records: []string{"C  copied.go", "source.go"}, ok: true, consumed: 2},
			{records: []string{"XY file.txt"}, consumed: 1},
		}
		for _, tt := range tests {
			_, consumed, ok := parseChangedRecord(tt.records)
			if ok != tt.ok || consumed != tt.consumed {
				t.Fatalf("parseChangedRecord(%v) = (ok=%v, consumed=%d)", tt.records, ok, consumed)
			}
		}
		if normalizeStatus('U', 'A') != StatusConflicted ||
			normalizeStatus('R', ' ') != StatusRenamed ||
			normalizeStatus('C', ' ') != StatusCopied ||
			normalizeStatus('D', ' ') != StatusDeleted ||
			normalizeStatus('A', ' ') != StatusAdded ||
			normalizeStatus('M', ' ') != StatusModified ||
			normalizeStatus('X', 'Y') != "" {
			t.Fatalf("normalizeStatus() mapping mismatch")
		}
	})

	t.Run("path, snippet, workspace, and context helpers", func(t *testing.T) {
		t.Parallel()

		workdir := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "a.go"), "package pkg\n\nconst Name = \"Widget\"\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "b.txt"), "Widget appears twice\nWidget\n")
		mustWriteRepositoryFile(t, filepath.Join(workdir, "node_modules", "ignored.txt"), "ignored")

		if _, _, _, err := normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalModePath, Value: " "}); err == nil {
			t.Fatal("expected empty query error")
		}
		if _, _, _, err := normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalMode("x"), Value: "a"}); !errors.Is(err, errInvalidMode) {
			t.Fatalf("expected invalid mode error, got %v", err)
		}
		if _, _, _, err := normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalModePath, Value: "a", ScopeDir: "pkg/a.go"}); err == nil {
			t.Fatal("expected scope is not directory error")
		}

		root, scope, normalized, err := normalizeRetrievalQuery(workdir, RetrievalQuery{
			Mode:         RetrievalModeText,
			Value:        "  Widget  ",
			Limit:        999,
			ContextLines: -1,
		})
		if err != nil || root == "" || scope == "" {
			t.Fatalf("normalizeRetrievalQuery() = (%q, %q, %+v, %v)", root, scope, normalized, err)
		}
		if normalized.Value != "Widget" || normalized.Limit != maxRetrievalLimit || normalized.ContextLines != defaultContextLines {
			t.Fatalf("unexpected normalized query: %+v", normalized)
		}

		lines := splitNonEmptyLines("a\r\n\n b \n\t\nc")
		if !slices.Equal(lines, []string{"a", " b ", "c"}) {
			t.Fatalf("splitNonEmptyLines() = %#v", lines)
		}
		if snippet := trimSnippetText("a\nb\nc", 2); !snippet.truncated || snippet.lines != 2 {
			t.Fatalf("trimSnippetText() = %+v", snippet)
		}
		if text, changed := truncateSnippetLine(strings.Repeat("x", maxSnippetLineRunes+1), maxSnippetLineRunes); !changed || len([]rune(text)) != maxSnippetLineRunes {
			t.Fatalf("truncateSnippetLine() = (%q, %v)", text, changed)
		}
		if snippet, hint := snippetAroundLine("a\nb\nc", 99, 1); hint != 3 || !strings.Contains(snippet, "c") {
			t.Fatalf("snippetAroundLine() = (%q,%d)", snippet, hint)
		}

		var visited []string
		err = walkWorkspaceFiles(context.Background(), workdir, workdir, func(path string) error {
			visited = append(visited, filepath.Base(path))
			return nil
		})
		if err != nil {
			t.Fatalf("walkWorkspaceFiles() error = %v", err)
		}
		if slices.Contains(visited, "ignored.txt") {
			t.Fatalf("expected node_modules to be skipped, got %v", visited)
		}
		stopErr := errors.New("stop")
		if err := walkWorkspaceFiles(context.Background(), workdir, workdir, func(path string) error { return stopErr }); !errors.Is(err, stopErr) {
			t.Fatalf("expected callback error to bubble up, got %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := walkWorkspaceFiles(ctx, workdir, workdir, func(path string) error { return nil }); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled walk, got %v", err)
		}

		if normalizeLimit(0, 3, 10) != 3 || normalizeLimit(11, 3, 10) != 10 || normalizeLimit(4, 3, 10) != 4 {
			t.Fatalf("normalizeLimit() mismatch")
		}
		if filepathSlashClean("a/b") != filepath.Clean(filepath.FromSlash("a/b")) {
			t.Fatalf("filepathSlashClean() mismatch")
		}
		if minInt(1, 2) != 1 || minInt(3, 2) != 2 {
			t.Fatalf("minInt() mismatch")
		}
	})

	t.Run("run git command and utility helpers", func(t *testing.T) {
		t.Parallel()

		if out, err := runGitCommand(context.Background(), t.TempDir(), GitCommandOptions{}, "--version"); err != nil || !strings.Contains(strings.ToLower(out.Text), "git version") {
			t.Fatalf("runGitCommand(--version) = (%+v, %v)", out, err)
		}
		if _, err := runGitCommand(context.Background(), t.TempDir(), GitCommandOptions{}, "unknown-subcommand-for-test"); err == nil {
			t.Fatal("expected invalid git subcommand to fail")
		}

		if !isNotGitRepository("fatal: not a git repository", errors.New("x")) {
			t.Fatal("expected not git repository detection")
		}
		if isNotGitRepository("", nil) {
			t.Fatal("expected nil error to return false")
		}
		if !isContextError(context.Canceled) || !isContextError(context.DeadlineExceeded) || isContextError(errors.New("x")) {
			t.Fatal("isContextError() mismatch")
		}

		var buf gitOutputBuffer
		if n, err := buf.Write([]byte("abc")); err != nil || n != 3 || buf.String() != "abc" {
			t.Fatalf("gitOutputBuffer write = (%d, %v, %q)", n, err, buf.String())
		}
		buf = gitOutputBuffer{maxBytes: 2}
		if n, err := buf.Write([]byte("abcd")); err != nil || n != 4 || !buf.truncated || buf.String() != "ab" {
			t.Fatalf("gitOutputBuffer limited write = (%d, %v, %q, truncated=%v)", n, err, buf.String(), buf.truncated)
		}
		buf = gitOutputBuffer{maxBytes: 2}
		if _, err := buf.Write([]byte("ab")); err != nil {
			t.Fatalf("gitOutputBuffer fill error = %v", err)
		}
		if n, err := buf.Write([]byte("c")); err != nil || n != 1 || !buf.truncated {
			t.Fatalf("gitOutputBuffer overflow write = (%d, %v, truncated=%v)", n, err, buf.truncated)
		}
		root := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		if !hasGitMetadataAncestor(filepath.Join(root, ".git", "objects")) {
			t.Fatal("expected .git ancestor detection")
		}
		if !isAmbiguousGitStatusOutsideRepo(t.TempDir(), "", errors.New("exit status 128")) {
			t.Fatal("expected ambiguous outside-repo status to be treated as non-git")
		}
		if isAmbiguousGitStatusOutsideRepo(root, "", errors.New("exit status 128")) {
			t.Fatal("expected git ancestor to disable ambiguous outside-repo fallback")
		}
	})
}

func TestRepositoryReadSearchAndServiceEntrypoints(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "readable.go"), "package pkg\n\nfunc Readable() {}\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "search.txt"), "alpha beta\nalpha\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "search2.txt"), "alpha again\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "defs.go"), "package pkg\n\nfunc BuildWidget(\n\tname string,\n) string {\n\treturn name\n}\n\ntype Widget struct{}\nconst WidgetName = \"x\"\nvar WidgetVar = 1\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, "notes.py"), "def py_symbol():\n    return 1\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, ".env"), "SECRET=1\n")
	mustWriteRepositoryFile(t, filepath.Join(workdir, "pkg", "bin.dat"), string([]byte{0x00, 0x01, 0x02}))

	service := NewService()

	readResult, err := service.Read(context.Background(), workdir, "pkg/readable.go", ReadOptions{MaxBytes: 12})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if readResult.Path != filepath.Clean("pkg/readable.go") || !readResult.Truncated || readResult.IsBinary {
		t.Fatalf("unexpected read result: %+v", readResult)
	}

	binaryRead, err := service.Read(context.Background(), workdir, "pkg/bin.dat", ReadOptions{})
	if err != nil {
		t.Fatalf("Read(binary) error = %v", err)
	}
	if !binaryRead.IsBinary {
		t.Fatalf("expected binary read result, got %+v", binaryRead)
	}

	filteredRead, err := service.Read(context.Background(), workdir, ".env", ReadOptions{})
	if err != nil || filteredRead.Content != "" {
		t.Fatalf("expected sensitive read to be filtered, got (%+v, %v)", filteredRead, err)
	}

	textResult, err := service.SearchText(context.Background(), workdir, "alpha", SearchOptions{Limit: 1})
	if err != nil {
		t.Fatalf("SearchText() error = %v", err)
	}
	if len(textResult.Hits) != 1 || !textResult.Truncated || textResult.TotalCount == 0 {
		t.Fatalf("unexpected text search result: %+v", textResult)
	}

	symbolResult, err := service.SearchSymbol(context.Background(), workdir, "BuildWidget", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchSymbol(go) error = %v", err)
	}
	if len(symbolResult.Hits) == 0 || symbolResult.Hits[0].Kind != "function" || !strings.Contains(symbolResult.Hits[0].Signature, "func BuildWidget") {
		t.Fatalf("unexpected go symbol result: %+v", symbolResult)
	}

	treeResult, err := service.SearchSymbol(context.Background(), workdir, "py_symbol", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchSymbol(tree-sitter) error = %v", err)
	}
	if len(treeResult.Hits) == 0 || treeResult.Hits[0].Kind == "" {
		t.Fatalf("unexpected tree-sitter symbol result: %+v", treeResult)
	}

	fallbackResult, err := service.SearchSymbol(context.Background(), workdir, "alpha", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchSymbol(fallback) error = %v", err)
	}
	if len(fallbackResult.Hits) == 0 || fallbackResult.Hits[0].Kind != "reference" {
		t.Fatalf("unexpected fallback symbol result: %+v", fallbackResult)
	}

	if got := extractGoSignature("func BuildWidget(\n\tname string,\n) string {\n\treturn name\n}\n", 1); !strings.Contains(got, "name string") {
		t.Fatalf("extractGoSignature(multiline) = %q", got)
	}
	if got := classifyGoSignature("func (*Svc).BuildWidget() {}"); got != "method" {
		t.Fatalf("classifyGoSignature(method) = %q", got)
	}
	if got := classifyGoSignature("const Widget = 1"); got != "constant" {
		t.Fatalf("classifyGoSignature(const) = %q", got)
	}
	if got := classifyGoSignature("var Widget = 1"); got != "variable" {
		t.Fatalf("classifyGoSignature(var) = %q", got)
	}
	if got := classifyGoSignature("type Widget struct{}"); got != "type" {
		t.Fatalf("classifyGoSignature(type) = %q", got)
	}
	if got := classifyGoSignature("???"); got != "unknown" {
		t.Fatalf("classifyGoSignature(unknown) = %q", got)
	}

	root, scope, err := resolveSearchScope(workdir, "pkg")
	if err != nil || root == "" || scope == "" {
		t.Fatalf("resolveSearchScope() = (%q, %q, %v)", root, scope, err)
	}
	if _, _, err := resolveSearchScope(workdir, "../bad"); err == nil {
		t.Fatal("expected invalid search scope error")
	}
}

func newRepositoryTestService(runner func(ctx context.Context, workdir string, args ...string) (GitCommandOutput, error)) *Service {
	return &Service{
		gitRunner: func(ctx context.Context, workdir string, opts GitCommandOptions, args ...string) (GitCommandOutput, error) {
			return runner(ctx, workdir, args...)
		},
		readFile: readFile,
	}
}

func assertChangedRepositoryFile(t *testing.T, file ChangedFile, path string, oldPath string, status ChangedFileStatus, snippetContains string) {
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

func mustWriteRepositoryFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func nulJoin(records ...string) string {
	if len(records) == 0 {
		return ""
	}
	return strings.Join(records, "\x00") + "\x00"
}
