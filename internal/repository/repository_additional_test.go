package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadGitSnapshotGuardsAndErrorFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("context canceled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		snapshot, err := (&Service{}).loadGitSnapshot(ctx, t.TempDir())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("loadGitSnapshot() err = %v, want context canceled", err)
		}
		if snapshot.InGitRepo || snapshot.Branch != "" || snapshot.Ahead != 0 || snapshot.Behind != 0 || len(snapshot.Entries) != 0 {
			t.Fatalf("expected empty snapshot, got %+v", snapshot)
		}
	})

	t.Run("empty workdir or nil runner", func(t *testing.T) {
		t.Parallel()

		service := &Service{}
		if snapshot, err := service.loadGitSnapshot(context.Background(), " "); err != nil || snapshot.InGitRepo || len(snapshot.Entries) != 0 {
			t.Fatalf("loadGitSnapshot(empty) = (%+v, %v), want empty nil", snapshot, err)
		}
	})

	t.Run("non git and generic error fallback to empty", func(t *testing.T) {
		t.Parallel()

		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
				return "fatal: not a git repository", errors.New("exit status 128")
			},
		}
		snapshot, err := service.loadGitSnapshot(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("loadGitSnapshot(non-git) err = %v", err)
		}
		if snapshot.InGitRepo || len(snapshot.Entries) != 0 {
			t.Fatalf("expected empty snapshot, got %+v", snapshot)
		}

		service.gitRunner = func(ctx context.Context, workdir string, args ...string) (string, error) {
			return "", errors.New("boom")
		}
		snapshot, err = service.loadGitSnapshot(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("loadGitSnapshot(generic err) err = %v", err)
		}
		if snapshot.InGitRepo || len(snapshot.Entries) != 0 {
			t.Fatalf("expected empty snapshot, got %+v", snapshot)
		}
	})

	t.Run("context error from runner bubbles up", func(t *testing.T) {
		t.Parallel()

		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
				return "", context.DeadlineExceeded
			},
		}
		_, err := service.loadGitSnapshot(context.Background(), t.TempDir())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("loadGitSnapshot() err = %v, want deadline exceeded", err)
		}
	})
}

func TestChangedFileSnippetBranches(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "added.go"), "package pkg\n\nfunc Added() {}\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "untracked.go"), "package pkg\n\nfunc NewFile() {}\n")

	service := &Service{
		gitRunner: func(ctx context.Context, dir string, args ...string) (string, error) {
			command := strings.Join(args, " ")
			switch command {
			case "diff --unified=3 HEAD -- pkg/modified.go":
				return "@@ -1,1 +1,1 @@\n-func Old(){}\n+func New(){}\n", nil
			case "diff --unified=3 HEAD -- pkg/renamed.go":
				return "@@ -1,1 +1,1 @@\n-old\n+new\n", nil
			case "diff --unified=3 HEAD -- pkg/added.go":
				return "", nil
			case "diff --unified=3 HEAD -- pkg/error.go":
				return "", context.Canceled
			default:
				return "", nil
			}
		},
		readFile: readFile,
	}

	tests := []struct {
		name        string
		entry       gitChangedEntry
		wantErr     error
		wantSnippet string
	}{
		{name: "deleted", entry: gitChangedEntry{Path: "pkg/deleted.go", Status: StatusDeleted}},
		{name: "conflicted", entry: gitChangedEntry{Path: "pkg/conflicted.go", Status: StatusConflicted}},
		{name: "modified", entry: gitChangedEntry{Path: "pkg/modified.go", Status: StatusModified}, wantSnippet: "func New"},
		{name: "renamed", entry: gitChangedEntry{Path: "pkg/renamed.go", Status: StatusRenamed}, wantSnippet: "+new"},
		{name: "added fallback to file", entry: gitChangedEntry{Path: "pkg/added.go", Status: StatusAdded}, wantSnippet: "func Added"},
		{name: "untracked file head", entry: gitChangedEntry{Path: "pkg/untracked.go", Status: StatusUntracked}, wantSnippet: "func NewFile"},
		{name: "context error", entry: gitChangedEntry{Path: "pkg/error.go", Status: StatusAdded}, wantErr: context.Canceled},
		{name: "unknown status", entry: gitChangedEntry{Path: "pkg/unknown.go", Status: ChangedFileStatus("other")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snippet, err := service.changedFileSnippet(context.Background(), workdir, tt.entry)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("changedFileSnippet() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("changedFileSnippet() err = %v", err)
			}
			if tt.wantSnippet != "" && !strings.Contains(snippet.text, tt.wantSnippet) {
				t.Fatalf("snippet %q does not contain %q", snippet.text, tt.wantSnippet)
			}
		})
	}
}

func TestSnippetReadersAndParsers(t *testing.T) {
	t.Parallel()

	t.Run("read diff snippet fallbacks", func(t *testing.T) {
		t.Parallel()

		if snippet, err := ((*Service)(nil)).readDiffSnippet(context.Background(), "", "a.go"); err != nil || snippet != (snippetResult{}) {
			t.Fatalf("nil service readDiffSnippet = (%+v, %v)", snippet, err)
		}

		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
				return "", errors.New("ignored")
			},
		}
		if snippet, err := service.readDiffSnippet(context.Background(), "", "a.go"); err != nil || snippet != (snippetResult{}) {
			t.Fatalf("readDiffSnippet(non-context err) = (%+v, %v)", snippet, err)
		}

		service.gitRunner = func(ctx context.Context, workdir string, args ...string) (string, error) {
			return "", context.DeadlineExceeded
		}
		_, err := service.readDiffSnippet(context.Background(), "", "a.go")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("readDiffSnippet() err = %v, want deadline exceeded", err)
		}
	})

	t.Run("read file head snippet fallbacks", func(t *testing.T) {
		t.Parallel()

		if snippet, err := ((*Service)(nil)).readFileHeadSnippet("", "a.go"); err != nil || snippet != (snippetResult{}) {
			t.Fatalf("nil service readFileHeadSnippet = (%+v, %v)", snippet, err)
		}
		workdir := t.TempDir()
		service := &Service{readFile: readFile}
		_, err := service.readFileHeadSnippet(workdir, "../escape.txt")
		if err == nil {
			t.Fatalf("expected path escape error")
		}

		service.readFile = func(path string) ([]byte, error) {
			return nil, errors.New("read failed")
		}
		snippet, err := service.readFileHeadSnippet(workdir, "missing.txt")
		if err != nil {
			t.Fatalf("readFileHeadSnippet() err = %v", err)
		}
		if snippet != (snippetResult{}) {
			t.Fatalf("expected empty snippet on read error, got %+v", snippet)
		}
	})
}

func TestGitParsingHelpers(t *testing.T) {
	t.Parallel()

	branch, ahead, behind := parseBranchLine("")
	if branch != "" || ahead != 0 || behind != 0 {
		t.Fatalf("parseBranchLine(empty) = (%q,%d,%d)", branch, ahead, behind)
	}
	branch, ahead, behind = parseBranchLine("No commits yet on feature/test")
	if branch != "feature/test" || ahead != 0 || behind != 0 {
		t.Fatalf("parseBranchLine(no commits) = (%q,%d,%d)", branch, ahead, behind)
	}
	branch, _, _ = parseBranchLine("HEAD (no branch)")
	if branch != "detached" {
		t.Fatalf("parseBranchLine(detached) = %q", branch)
	}
	branch, ahead, behind = parseBranchLine("feature/x...origin/feature/x [ahead 2, behind 1]")
	if branch != "feature/x" || ahead != 2 || behind != 1 {
		t.Fatalf("parseBranchLine(tracking) = (%q,%d,%d)", branch, ahead, behind)
	}
	branch, ahead, behind = parseBranchLine("main [ahead nope, behind 3]")
	if branch != "main" || ahead != 0 || behind != 3 {
		t.Fatalf("parseBranchLine(invalid ahead value) = (%q,%d,%d)", branch, ahead, behind)
	}

	tests := []struct {
		line    string
		ok      bool
		status  ChangedFileStatus
		path    string
		oldPath string
	}{
		{line: "", ok: false},
		{line: "?? ", ok: false},
		{line: "?? pkg/new.go", ok: true, status: StatusUntracked, path: filepath.Clean("pkg/new.go")},
		{line: "R  old.go -> new.go", ok: true, status: StatusRenamed, path: filepath.Clean("new.go"), oldPath: filepath.Clean("old.go")},
		{line: " M pkg/mod.go", ok: true, status: StatusModified, path: filepath.Clean("pkg/mod.go")},
		{line: " D pkg/deleted.go", ok: true, status: StatusDeleted, path: filepath.Clean("pkg/deleted.go")},
		{line: "?? \t", ok: false},
		{line: "XY file.txt", ok: false},
	}
	for _, tt := range tests {
		got, ok := parseChangedEntry(tt.line)
		if ok != tt.ok {
			t.Fatalf("parseChangedEntry(%q) ok=%t, want %t", tt.line, ok, tt.ok)
		}
		if !ok {
			continue
		}
		if got.Status != tt.status || got.Path != tt.path || got.OldPath != tt.oldPath {
			t.Fatalf("parseChangedEntry(%q) = %+v, want status=%q path=%q old=%q", tt.line, got, tt.status, tt.path, tt.oldPath)
		}
	}

	if normalizeStatus('U', 'A') != StatusConflicted ||
		normalizeStatus('R', ' ') != StatusRenamed ||
		normalizeStatus('D', ' ') != StatusDeleted ||
		normalizeStatus('A', ' ') != StatusAdded ||
		normalizeStatus('M', ' ') != StatusModified ||
		normalizeStatus('X', 'Y') != "" {
		t.Fatalf("normalizeStatus() mapping mismatch")
	}
}

func TestPathAndRetrievalHelpers(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "a.go"), "package pkg\n\nconst Name = \"Widget\"\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "b.txt"), "Widget appears twice\nWidget\n")
	if err := os.MkdirAll(filepath.Join(workdir, "node_modules"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	mustWriteFile(t, filepath.Join(workdir, "node_modules", "ignored.txt"), "ignored")

	t.Run("normalize retrieval query", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalModePath, Value: " "})
		if err == nil {
			t.Fatalf("expected empty query error")
		}
		_, _, _, err = normalizeRetrievalQuery(string([]byte{0}), RetrievalQuery{Mode: RetrievalModePath, Value: "a"})
		if err == nil {
			t.Fatalf("expected invalid workdir error")
		}
		_, _, _, err = normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalMode("x"), Value: "a"})
		if !errors.Is(err, errInvalidMode) {
			t.Fatalf("normalizeRetrievalQuery invalid mode err = %v", err)
		}
		_, _, _, err = normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalModePath, Value: "a", ScopeDir: ".."})
		if err == nil {
			t.Fatalf("expected scope traversal error")
		}
		_, _, _, err = normalizeRetrievalQuery(workdir, RetrievalQuery{Mode: RetrievalModePath, Value: "a", ScopeDir: "pkg/a.go"})
		if err == nil {
			t.Fatalf("expected scope is not dir error")
		}

		root, scope, normalized, err := normalizeRetrievalQuery(workdir, RetrievalQuery{
			Mode:         RetrievalModeText,
			Value:        "  Widget  ",
			Limit:        999,
			ContextLines: -1,
		})
		if err != nil {
			t.Fatalf("normalizeRetrievalQuery() err = %v", err)
		}
		if root == "" || scope == "" {
			t.Fatalf("expected resolved root/scope")
		}
		if normalized.Value != "Widget" || normalized.Limit != maxRetrievalLimit || normalized.ContextLines != defaultContextLines {
			t.Fatalf("unexpected normalized query: %+v", normalized)
		}
	})

	t.Run("line helpers and walkers", func(t *testing.T) {
		t.Parallel()

		lines := splitNonEmptyLines("a\r\n\n b \n\t\nc")
		if !slices.Equal(lines, []string{"a", " b ", "c"}) {
			t.Fatalf("splitNonEmptyLines() = %#v", lines)
		}
		if snippet := trimSnippetText("", 2); snippet != (snippetResult{}) {
			t.Fatalf("expected empty snippet for empty input")
		}
		if snippet := trimSnippetText("a\nb\nc", 2); !snippet.truncated || snippet.lines != 2 {
			t.Fatalf("trimSnippetText() = %+v, want truncated 2 lines", snippet)
		}

		text, hint := snippetAroundLine("line1\nline2\nline3", 99, 1)
		if hint != 3 || !strings.Contains(text, "line3") {
			t.Fatalf("snippetAroundLine() = (%q,%d)", text, hint)
		}
		if text, hint = snippetAroundLine("", 1, 1); text != "" || hint != 1 {
			t.Fatalf("snippetAroundLine(empty) = (%q,%d)", text, hint)
		}

		visited := make([]string, 0, 2)
		err := walkWorkspaceFiles(workdir, workdir, func(path string, entry fs.DirEntry) error {
			visited = append(visited, filepath.Base(path))
			return nil
		})
		if err != nil {
			t.Fatalf("walkWorkspaceFiles() err = %v", err)
		}
		if slices.Contains(visited, "ignored.txt") {
			t.Fatalf("expected node_modules file to be skipped, got %v", visited)
		}
		err = walkWorkspaceFiles(workdir, filepath.Join(workdir, "missing"), func(path string, entry fs.DirEntry) error {
			return nil
		})
		if err == nil {
			t.Fatalf("expected walkWorkspaceFiles to return walk error for missing scope")
		}

		if normalizeLimit(0, 3, 10) != 3 || normalizeLimit(11, 3, 10) != 10 || normalizeLimit(4, 3, 10) != 4 {
			t.Fatalf("normalizeLimit() mismatch")
		}
		if filepathSlashClean(" a/b ") != filepath.Clean(filepath.FromSlash("a/b")) {
			t.Fatalf("filepathSlashClean() mismatch")
		}
		if minInt(1, 2) != 1 || minInt(3, 2) != 2 {
			t.Fatalf("minInt() mismatch")
		}
	})
}

func TestRetrieveAndServiceEdgeCases(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustWriteFile(t, filepath.Join(workdir, "pkg", "defs.go"), "package pkg\n\ntype Widget struct{}\n\nfunc BuildWidget() {}\nconst WidgetName = \"x\"\nvar WidgetVar = 1\n")
	mustWriteFile(t, filepath.Join(workdir, "pkg", "notes.txt"), "Widget WidgetName")

	service := &Service{
		gitRunner: runGitCommand,
		readFile:  readFile,
	}

	t.Run("retrieve path guards and not exist", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := service.retrieveByPath(ctx, workdir, RetrievalQuery{Mode: RetrievalModePath, Value: "pkg/defs.go"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("retrieveByPath canceled err = %v", err)
		}

		hits, err := service.retrieveByPath(context.Background(), workdir, RetrievalQuery{Mode: RetrievalModePath, Value: "pkg/missing.go"})
		if err != nil {
			t.Fatalf("retrieveByPath missing err = %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("expected empty hits for missing file, got %+v", hits)
		}
	})

	t.Run("retrieve glob/text/symbol helpers", func(t *testing.T) {
		t.Parallel()

		_, err := service.retrieveByGlob(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:  RetrievalModeGlob,
			Value: "[",
			Limit: 5,
		})
		if err == nil {
			t.Fatalf("expected invalid glob pattern error")
		}

		textHits, err := service.retrieveByText(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:         RetrievalModeText,
			Value:        "Widget",
			Limit:        2,
			ContextLines: 1,
		}, false)
		if err != nil || len(textHits) == 0 {
			t.Fatalf("retrieveByText() = (%+v, %v), want hits", textHits, err)
		}

		wordHits, err := service.retrieveByText(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:         RetrievalModeText,
			Value:        "Widget",
			Limit:        5,
			ContextLines: 1,
		}, true)
		if err != nil || len(wordHits) == 0 {
			t.Fatalf("retrieveByText wholeWord() = (%+v, %v), want hits", wordHits, err)
		}

		symbolHits, err := service.retrieveBySymbol(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:         RetrievalModeSymbol,
			Value:        "BuildWidget",
			Limit:        5,
			ContextLines: 1,
		})
		if err != nil || len(symbolHits) == 0 {
			t.Fatalf("retrieveBySymbol() = (%+v, %v), want symbol hits", symbolHits, err)
		}

		fallbackHits, err := service.retrieveBySymbol(context.Background(), workdir, workdir, RetrievalQuery{
			Mode:         RetrievalModeSymbol,
			Value:        "WidgetName",
			Limit:        5,
			ContextLines: 1,
		})
		if err != nil || len(fallbackHits) == 0 {
			t.Fatalf("retrieveBySymbol fallback() = (%+v, %v), want hits", fallbackHits, err)
		}
		for _, hit := range fallbackHits {
			if hit.Kind != string(RetrievalModeSymbol) {
				t.Fatalf("expected fallback kind rewritten to symbol, got %+v", hit)
			}
		}
	})

	t.Run("find symbol definitions and sorting", func(t *testing.T) {
		t.Parallel()

		defs := findGoSymbolDefinitions(strings.Join([]string{
			"package p",
			"type Widget struct{}",
			"func BuildWidget(){}",
			"func (s *Svc) BuildWidget(){}",
			"const WidgetName = \"x\"",
			"var WidgetVar = 1",
			"const (",
			"WidgetInBlock = 1",
			")",
			"var (",
			"WidgetVarBlock = 2",
			")",
		}, "\n"), "BuildWidget")
		if len(defs) < 2 {
			t.Fatalf("expected function + method definitions, got %v", defs)
		}
		if got := findGoSymbolDefinitions("package p", " "); got != nil {
			t.Fatalf("expected nil for empty symbol, got %v", got)
		}

		hits := []RetrievalHit{
			{Path: "b.go", LineHint: 3},
			{Path: "a.go", LineHint: 8},
			{Path: "a.go", LineHint: 2},
		}
		sortRetrievalHits(hits)
		if hits[0].Path != "a.go" || hits[0].LineHint != 2 || hits[2].Path != "b.go" {
			t.Fatalf("sortRetrievalHits() unexpected order: %+v", hits)
		}
	})

	t.Run("summary and changed files error branches", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := service.Summary(ctx, workdir)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Summary() err = %v, want context canceled", err)
		}

		serviceWithCancelledDiff := &Service{
			gitRunner: func(ctx context.Context, dir string, args ...string) (string, error) {
				switch strings.Join(args, " ") {
				case "status --porcelain=v1 --branch --untracked-files=normal":
					return "## main\nA  pkg/new.go", nil
				case "diff --unified=3 HEAD -- pkg/new.go":
					return "", context.DeadlineExceeded
				default:
					return "", nil
				}
			},
			readFile: readFile,
		}
		_, err = serviceWithCancelledDiff.ChangedFiles(context.Background(), workdir, ChangedFilesOptions{IncludeSnippets: true})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ChangedFiles() err = %v, want deadline exceeded", err)
		}

		_, err = service.Retrieve(ctx, workdir, RetrievalQuery{Mode: RetrievalModeText, Value: "Widget"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Retrieve() err = %v, want context canceled", err)
		}

		if !isNotGitRepository("fatal: not a git repository", errors.New("x")) {
			t.Fatalf("expected not-git output to be recognized")
		}
		if isNotGitRepository("", nil) {
			t.Fatalf("expected nil error to return false")
		}
		if !isContextError(context.Canceled) || !isContextError(context.DeadlineExceeded) || isContextError(errors.New("x")) {
			t.Fatalf("isContextError() mismatch")
		}
	})
}

func TestRepositoryCoverageExtraBranches(t *testing.T) {
	t.Parallel()

	t.Run("runGitCommand success and failure", func(t *testing.T) {
		t.Parallel()

		out, err := runGitCommand(context.Background(), t.TempDir(), "--version")
		if err != nil {
			t.Fatalf("runGitCommand(--version) err = %v", err)
		}
		if !strings.Contains(strings.ToLower(out), "git version") {
			t.Fatalf("unexpected git --version output: %q", out)
		}

		_, err = runGitCommand(context.Background(), t.TempDir(), "unknown-subcommand-for-test")
		if err == nil {
			t.Fatalf("expected runGitCommand invalid subcommand to fail")
		}
	})

	t.Run("parse snapshot and counters", func(t *testing.T) {
		t.Parallel()

		emptySnapshot := parseGitSnapshot("")
		if emptySnapshot.InGitRepo || len(emptySnapshot.Entries) != 0 {
			t.Fatalf("parseGitSnapshot(empty) = %+v", emptySnapshot)
		}

		snapshot := parseGitSnapshot(" M a.go\n?? b.go")
		if !snapshot.InGitRepo || len(snapshot.Entries) != 2 {
			t.Fatalf("parseGitSnapshot(without branch line) = %+v", snapshot)
		}

		ahead, behind := parseTrackingCounters("main [ahead 2, weird, behind 1, ahead nope]")
		if ahead != 2 || behind != 1 {
			t.Fatalf("parseTrackingCounters() = (%d,%d), want (2,1)", ahead, behind)
		}
		ahead, behind = parseTrackingCounters("main []")
		if ahead != 0 || behind != 0 {
			t.Fatalf("parseTrackingCounters(empty segment) = (%d,%d), want (0,0)", ahead, behind)
		}
	})

	t.Run("scope and snippet boundaries", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		scope, err := resolveScopeDir(root, "")
		if err != nil || scope == "" {
			t.Fatalf("resolveScopeDir(empty) = (%q, %v)", scope, err)
		}
		_, err = resolveScopeDir(root, "missing")
		if err == nil {
			t.Fatalf("expected resolveScopeDir missing path error")
		}

		snippet, hint := snippetAroundLine("a\nb\nc", 0, 1)
		if hint != 1 || !strings.Contains(snippet, "a") {
			t.Fatalf("snippetAroundLine(line<=0) = (%q,%d)", snippet, hint)
		}
		if _, err := resolveScopeDir(root, ".."); err == nil {
			t.Fatalf("expected resolveScopeDir to reject traversal")
		}
	})

	t.Run("walk workspace callback and symlink escape", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "a.txt"), "a")
		expectedErr := errors.New("stop")
		err := walkWorkspaceFiles(root, root, func(path string, entry fs.DirEntry) error {
			return expectedErr
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("walkWorkspaceFiles(callback err) = %v", err)
		}

		outsideDir := t.TempDir()
		outsideFile := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		linkPath := filepath.Join(root, "escape.txt")
		if err := os.Symlink(outsideFile, linkPath); err == nil {
			err = walkWorkspaceFiles(root, root, func(path string, entry fs.DirEntry) error {
				return nil
			})
			if err == nil {
				t.Fatalf("expected symlink escape error from walkWorkspaceFiles")
			}
		}
	})

	t.Run("retrieve branches and service switches", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "pkg", "defs.go"), strings.Join([]string{
			"package pkg",
			"func BuildWidget(){}",
			"func BuildWidget2(){}",
			"func (s *Svc) BuildWidget(){}",
			"const (",
			"WidgetName = \"x\"",
			")",
		}, "\n"))
		mustWriteFile(t, filepath.Join(root, "pkg", "match.txt"), "hit\nhit\nhit")

		svc := &Service{
			gitRunner: runGitCommand,
			readFile:  readFile,
		}
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := svc.retrieveByGlob(canceledCtx, root, root, RetrievalQuery{Mode: RetrievalModeGlob, Value: "*.go", Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatalf("retrieveByGlob(canceled) err = %v", err)
		}
		if _, err := svc.retrieveByText(canceledCtx, root, root, RetrievalQuery{Mode: RetrievalModeText, Value: "hit", Limit: 1}, false); !errors.Is(err, context.Canceled) {
			t.Fatalf("retrieveByText(canceled) err = %v", err)
		}
		if _, err := svc.retrieveBySymbol(canceledCtx, root, root, RetrievalQuery{Mode: RetrievalModeSymbol, Value: "BuildWidget", Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatalf("retrieveBySymbol(canceled) err = %v", err)
		}

		// non-not-exist read error branch for retrieveByPath.
		failingReadSvc := &Service{
			readFile: func(path string) ([]byte, error) {
				return nil, fmt.Errorf("permission denied")
			},
		}
		_, err := failingReadSvc.retrieveByPath(context.Background(), root, RetrievalQuery{
			Mode:         RetrievalModePath,
			Value:        "pkg/defs.go",
			ContextLines: 1,
		})
		if err == nil {
			t.Fatalf("expected retrieveByPath non-not-exist error")
		}
		_, err = failingReadSvc.retrieveByGlob(context.Background(), root, root, RetrievalQuery{
			Mode:  RetrievalModeGlob,
			Value: "*.txt",
			Limit: 5,
		})
		if err != nil {
			t.Fatalf("retrieveByGlob(read err ignored) err = %v", err)
		}
		_, err = failingReadSvc.retrieveByText(context.Background(), root, root, RetrievalQuery{
			Mode:  RetrievalModeText,
			Value: "hit",
			Limit: 5,
		}, false)
		if err != nil {
			t.Fatalf("retrieveByText(read err ignored) err = %v", err)
		}
		_, err = failingReadSvc.retrieveBySymbol(context.Background(), root, root, RetrievalQuery{
			Mode:  RetrievalModeSymbol,
			Value: "BuildWidget",
			Limit: 5,
		})
		if err != nil {
			t.Fatalf("retrieveBySymbol(read err ignored) err = %v", err)
		}

		hits, err := svc.retrieveByGlob(context.Background(), root, root, RetrievalQuery{
			Mode:         RetrievalModeGlob,
			Value:        "pkg/*.txt",
			Limit:        1,
			ContextLines: 1,
		})
		if err != nil || len(hits) != 1 {
			t.Fatalf("retrieveByGlob(limit=1) = (%+v, %v)", hits, err)
		}

		textHits, err := svc.retrieveByText(context.Background(), root, root, RetrievalQuery{
			Mode:         RetrievalModeText,
			Value:        "hit",
			Limit:        1,
			ContextLines: 1,
		}, false)
		if err != nil || len(textHits) != 1 {
			t.Fatalf("retrieveByText(limit=1) = (%+v, %v)", textHits, err)
		}

		symbolHits, err := svc.retrieveBySymbol(context.Background(), root, root, RetrievalQuery{
			Mode:         RetrievalModeSymbol,
			Value:        "BuildWidget",
			Limit:        1,
			ContextLines: 1,
		})
		if err != nil || len(symbolHits) != 1 {
			t.Fatalf("retrieveBySymbol(limit=1) = (%+v, %v)", symbolHits, err)
		}
		_, err = svc.retrieveByText(context.Background(), root, filepath.Join(root, "missing"), RetrievalQuery{
			Mode:  RetrievalModeText,
			Value: "hit",
			Limit: 1,
		}, true)
		if err == nil {
			t.Fatalf("expected retrieveByText missing scope error")
		}
		_, err = svc.retrieveBySymbol(context.Background(), root, filepath.Join(root, "missing"), RetrievalQuery{
			Mode:  RetrievalModeSymbol,
			Value: "Unknown",
			Limit: 1,
		})
		if err == nil {
			t.Fatalf("expected retrieveBySymbol missing scope error")
		}

		_, err = svc.Retrieve(context.Background(), root, RetrievalQuery{Mode: RetrievalModeGlob, Value: "*.go"})
		if err != nil {
			t.Fatalf("Retrieve(glob) err = %v", err)
		}
		_, err = svc.Retrieve(context.Background(), root, RetrievalQuery{Mode: RetrievalModeText, Value: "BuildWidget"})
		if err != nil {
			t.Fatalf("Retrieve(text) err = %v", err)
		}
		_, err = svc.Retrieve(context.Background(), root, RetrievalQuery{Mode: RetrievalModeSymbol, Value: "BuildWidget"})
		if err != nil {
			t.Fatalf("Retrieve(symbol) err = %v", err)
		}
		_, err = svc.Retrieve(context.Background(), root, RetrievalQuery{Mode: RetrievalMode("invalid"), Value: "BuildWidget"})
		if !errors.Is(err, errInvalidMode) {
			t.Fatalf("Retrieve(invalid mode) err = %v", err)
		}
	})

	t.Run("summary representative limit and changed-files without snippets", func(t *testing.T) {
		t.Parallel()

		service := &Service{
			gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
				lines := []string{"## main"}
				for i := 0; i < representativeChangedFilesLimit+2; i++ {
					lines = append(lines, fmt.Sprintf(" M file%d.go", i))
				}
				return strings.Join(lines, "\n"), nil
			},
			readFile: readFile,
		}
		summary, err := service.Summary(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("Summary() err = %v", err)
		}
		if len(summary.RepresentativeChangedFiles) != representativeChangedFilesLimit {
			t.Fatalf("expected representative list to be capped at %d, got %d", representativeChangedFilesLimit, len(summary.RepresentativeChangedFiles))
		}

		changed, err := service.ChangedFiles(context.Background(), t.TempDir(), ChangedFilesOptions{IncludeSnippets: false})
		if err != nil {
			t.Fatalf("ChangedFiles(without snippets) err = %v", err)
		}
		for _, file := range changed.Files {
			if file.Snippet != "" {
				t.Fatalf("expected snippet empty when IncludeSnippets=false, got %q", file.Snippet)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := service.ChangedFiles(ctx, t.TempDir(), ChangedFilesOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("ChangedFiles(canceled) err = %v", err)
		}

		nonGitService := &Service{
			gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
				return "fatal: not a git repository", errors.New("exit status 128")
			},
			readFile: readFile,
		}
		ctxResult, err := nonGitService.ChangedFiles(context.Background(), t.TempDir(), ChangedFilesOptions{})
		if err != nil {
			t.Fatalf("ChangedFiles(non-git) err = %v", err)
		}
		if len(ctxResult.Files) != 0 || ctxResult.TotalCount != 0 || ctxResult.ReturnedCount != 0 {
			t.Fatalf("expected empty changed-files for non-git dir, got %+v", ctxResult)
		}
	})
}
