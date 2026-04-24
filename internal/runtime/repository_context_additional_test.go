package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"neo-code/internal/context/repository"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestBuildRepositoryContextEarlyReturnAndFatalPaths(t *testing.T) {
	t.Parallel()

	service := &Service{repositoryService: &stubRepositoryFactService{}, events: make(chan RuntimeEvent, 8)}
	state := newRepositoryTestState(t.TempDir(), "review 当前改动")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := service.buildRepositoryContext(ctx, &state, state.session.Workdir); !errors.Is(err, context.Canceled) {
		t.Fatalf("buildRepositoryContext(canceled) err = %v", err)
	}

	if summary, got, err := service.buildRepositoryContext(context.Background(), nil, state.session.Workdir); err != nil || summary != nil || got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("buildRepositoryContext(nil state) = (%+v, %+v, %v)", summary, got, err)
	}
	if summary, got, err := service.buildRepositoryContext(context.Background(), &state, " "); err != nil || summary != nil || got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("buildRepositoryContext(empty workdir) = (%+v, %+v, %v)", summary, got, err)
	}

	nonUserState := newRepositoryTestState(t.TempDir(), "ignored")
	nonUserState.session.Messages = []providertypes.Message{{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("assistant")},
	}}
	if summary, got, err := service.buildRepositoryContext(context.Background(), &nonUserState, nonUserState.session.Workdir); err != nil || got.ChangedFiles != nil || got.Retrieval != nil || summary != nil {
		t.Fatalf("buildRepositoryContext(no user text) = (%+v, %+v, %v)", summary, got, err)
	}

	fatalFromInspect := &Service{
		repositoryService: &stubRepositoryFactService{
			inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
				return repository.InspectResult{}, context.DeadlineExceeded
			},
		},
		events: make(chan RuntimeEvent, 8),
	}
	if _, _, err := fatalFromInspect.buildRepositoryContext(context.Background(), &state, state.session.Workdir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected fatal inspect error, got %v", err)
	}

	workdir := t.TempDir()
	mustRuntimeWriteFile(t, filepath.Join(workdir, "README.md"), "# readme\n")
	fatalFromRetrieval := &Service{
		repositoryService: &stubRepositoryFactService{
			inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
				return repository.InspectResult{Summary: repository.Summary{InGitRepo: true, Branch: "main"}}, nil
			},
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
				return repository.RetrievalResult{}, context.Canceled
			},
		},
		events: make(chan RuntimeEvent, 8),
	}
	retrievalState := newRepositoryTestState(workdir, "看看 README.md")
	_, _, err := fatalFromRetrieval.buildRepositoryContext(context.Background(), &retrievalState, retrievalState.session.Workdir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected fatal retrieval error, got %v", err)
	}
}

func TestRepositoryContextHelpers(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustRuntimeWriteFile(t, filepath.Join(workdir, "README.md"), "# readme\n")
	mustRuntimeWriteFile(t, filepath.Join(workdir, "internal", "runtime", "run.go"), "package runtime\n")

	if got := changedFilesLimitForUserText(false); got != defaultAutoChangedFilesLimit {
		t.Fatalf("changedFilesLimitForUserText(false) = %d", got)
	}
	if got := changedFilesLimitForUserText(true); got != defaultAutoChangedFilesWithDiff {
		t.Fatalf("changedFilesLimitForUserText(true) = %d", got)
	}

	if projectRepositorySummary(repository.Summary{}) != nil {
		t.Fatalf("expected nil summary projection for non-git")
	}
	summary := projectRepositorySummary(repository.Summary{
		InGitRepo: true,
		Branch:    "main",
		Dirty:     true,
		Ahead:     2,
		Behind:    1,
	})
	if summary == nil || summary.Branch != "main" || !summary.Dirty || summary.Ahead != 2 || summary.Behind != 1 {
		t.Fatalf("unexpected summary projection: %+v", summary)
	}

	if changedFilesProjectionForUserText("解释架构", repository.ChangedFilesContext{
		Files:         []repository.ChangedFile{{Path: "a.go", Status: repository.StatusModified}},
		ReturnedCount: 1,
		TotalCount:    maxAutoChangedFilesCount + 1,
	}) != nil {
		t.Fatalf("expected implicit large changed-files set to be dropped")
	}
	if projection := changedFilesProjectionForUserText("review 我的改动", repository.ChangedFilesContext{
		Files:         []repository.ChangedFile{{Path: "a.go", Status: repository.StatusModified}},
		ReturnedCount: 1,
		TotalCount:    maxAutoChangedFilesCount + 1,
		Truncated:     true,
	}); projection == nil || !projection.Truncated {
		t.Fatalf("expected explicit changed-files projection, got %+v", projection)
	}

	if query, ok := autoRetrievalQueryFromUserText(workdir, "解释这个模块"); ok {
		t.Fatalf("expected no query, got %+v", query)
	}
	if query, ok := autoPathRetrievalQuery(workdir, "`internal/runtime/run.go`"); !ok || query.Mode != repository.RetrievalModePath {
		t.Fatalf("autoPathRetrievalQuery(subdir) = (%+v, %t)", query, ok)
	}
	if query, ok := autoPathRetrievalQuery(workdir, "README.md"); !ok || query.Value != "README.md" {
		t.Fatalf("autoPathRetrievalQuery(root) = (%+v, %t)", query, ok)
	}
	if _, ok := autoPathRetrievalQuery(workdir, "missing.go"); ok {
		t.Fatalf("expected missing root file to not trigger path retrieval")
	}
	if workspacePathAnchorExists(workdir, "README.md") == false {
		t.Fatalf("expected README.md to exist as anchor")
	}
	if workspacePathAnchorExists(workdir, "missing.go") {
		t.Fatalf("expected missing anchor to be rejected")
	}

	if _, ok := autoSymbolRetrievalQuery("BuildWidget 在吗"); ok {
		t.Fatalf("expected symbol query to require intent words")
	}
	if _, ok := autoSymbolRetrievalQuery("where is BuildWidget"); ok {
		t.Fatalf("expected bare capitalized word to not trigger symbol retrieval")
	}
	if query, ok := autoSymbolRetrievalQuery("where is `BuildWidget`"); !ok || query.Value != "BuildWidget" {
		t.Fatalf("autoSymbolRetrievalQuery() = (%+v, %t)", query, ok)
	}

	if _, ok := autoTextRetrievalQuery("find `internal/runtime/run.go`"); ok {
		t.Fatalf("expected path-like quoted text to be ignored")
	}
	if _, ok := autoTextRetrievalQuery("find `go`"); ok {
		t.Fatalf("expected short quoted text to be ignored")
	}
	if query, ok := autoTextRetrievalQuery("find `permission_requested`"); !ok || query.Value != "permission_requested" {
		t.Fatalf("autoTextRetrievalQuery() = (%+v, %t)", query, ok)
	}

	if query, ok := autoRetrievalQueryFromUserText(workdir, "看看 README.md 的 BuildWidget 和 `permission_requested`"); !ok || query.Mode != repository.RetrievalModePath {
		t.Fatalf("expected path query to win priority, got (%+v, %t)", query, ok)
	}

	if !shouldAutoInjectChangedFiles("请看 changed files") || shouldAutoInjectChangedFiles("just chat") {
		t.Fatalf("shouldAutoInjectChangedFiles() mismatch")
	}
	if !shouldAutoIncludeChangedFileSnippets("please review diff") || shouldAutoIncludeChangedFileSnippets("just explain") {
		t.Fatalf("shouldAutoIncludeChangedFileSnippets() mismatch")
	}
	if !mentionsFixOrReviewIntent("debug this bug") || mentionsFixOrReviewIntent("architecture overview") {
		t.Fatalf("mentionsFixOrReviewIntent() mismatch")
	}
	if !isRepositoryContextFatalError(context.Canceled) || !isRepositoryContextFatalError(context.DeadlineExceeded) || isRepositoryContextFatalError(errors.New("x")) {
		t.Fatalf("isRepositoryContextFatalError() mismatch")
	}
}

func TestBuildRepositoryContextWithoutUserTextStillProjectsSummary(t *testing.T) {
	t.Parallel()

	session := agentsession.NewWithWorkdir("repo test", t.TempDir())
	session.Messages = []providertypes.Message{{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			{Kind: providertypes.ContentPartImage},
		},
	}}
	state := newRunState("run-no-user-text", session)
	service := &Service{
		repositoryService: &stubRepositoryFactService{
			inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
				return repository.InspectResult{
					Summary: repository.Summary{InGitRepo: true, Branch: "main", Dirty: true},
				}, nil
			},
		},
		events: make(chan RuntimeEvent, 8),
	}

	summary, got, err := service.buildRepositoryContext(context.Background(), &state, session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() err = %v", err)
	}
	if summary == nil || summary.Branch != "main" {
		t.Fatalf("expected summary even without retrieval anchors, got %+v", summary)
	}
	if got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("expected empty repository context, got %+v", got)
	}
}
