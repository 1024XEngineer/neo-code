package context

import (
	"context"
	"strings"
	"testing"

	"neo-code/internal/repository"
)

func TestRepositoryContextSourceSkipsEmptyRepositoryContext(t *testing.T) {
	t.Parallel()

	source := repositoryContextSource{}
	sections, err := source.Sections(context.Background(), BuildInput{})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("expected no sections, got %d", len(sections))
	}
}

func TestRepositoryContextSourceRendersChangedFilesAndRetrieval(t *testing.T) {
	t.Parallel()

	source := repositoryContextSource{}
	sections, err := source.Sections(context.Background(), BuildInput{
		Repository: RepositoryContext{
			ChangedFiles: &RepositoryChangedFilesSection{
				Files: []repository.ChangedFile{
					{Path: "internal/runtime/run.go", Status: repository.StatusModified, Snippet: "@@ line"},
					{Path: "internal/repository/git.go", OldPath: "internal/old_repo.go", Status: repository.StatusRenamed},
				},
				Truncated:     true,
				ReturnedCount: 2,
				TotalCount:    4,
			},
			Retrieval: &RepositoryRetrievalSection{
				Mode:      "symbol",
				Query:     "ExecuteSystemTool",
				Truncated: false,
				Hits: []repository.RetrievalHit{
					{
						Path:          "internal/runtime/system_tool.go",
						Kind:          "symbol",
						SymbolOrQuery: "ExecuteSystemTool",
						Snippet:       "func ExecuteSystemTool(...)",
						LineHint:      12,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected a single repository section, got %d", len(sections))
	}

	rendered := renderPromptSection(sections[0])
	if !strings.Contains(rendered, "## Repository Context") {
		t.Fatalf("expected repository section title, got %q", rendered)
	}
	if !strings.Contains(rendered, "### Changed Files") {
		t.Fatalf("expected changed files subsection, got %q", rendered)
	}
	if !strings.Contains(rendered, "`modified` internal/runtime/run.go") {
		t.Fatalf("expected changed file entry, got %q", rendered)
	}
	if !strings.Contains(rendered, "`renamed` internal/old_repo.go -> internal/repository/git.go") {
		t.Fatalf("expected renamed file entry, got %q", rendered)
	}
	if !strings.Contains(rendered, "### Targeted Retrieval") {
		t.Fatalf("expected retrieval subsection, got %q", rendered)
	}
	if !strings.Contains(rendered, "- mode: `symbol`") || !strings.Contains(rendered, "- query: `ExecuteSystemTool`") {
		t.Fatalf("expected retrieval metadata, got %q", rendered)
	}
	if !strings.Contains(rendered, "- internal/runtime/system_tool.go:12") {
		t.Fatalf("expected retrieval hit, got %q", rendered)
	}
}

func TestRepositoryContextSourceReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := repositoryContextSource{}
	_, err := source.Sections(ctx, BuildInput{})
	if err == nil {
		t.Fatalf("expected context error")
	}
}

func TestIndentBlockHandlesEmptyAndMultilineInput(t *testing.T) {
	t.Parallel()

	if got := indentBlock(" \n\t", "  "); got != "" {
		t.Fatalf("indentBlock(empty) = %q, want empty", got)
	}
	if got := indentBlock("a\r\nb", "--"); got != "--a\n--b" {
		t.Fatalf("indentBlock(multiline) = %q, want %q", got, "--a\n--b")
	}
}
