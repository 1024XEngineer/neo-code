package runtime

import (
	"context"
	"errors"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/repository"
	agentsession "neo-code/internal/session"
)

func TestBuildRepositoryContextEarlyReturnAndFatalPaths(t *testing.T) {
	t.Parallel()

	service := &Service{repositoryService: &stubRepositoryFactService{}, events: make(chan RuntimeEvent, 8)}
	state := newRepositoryTestState(t.TempDir(), "review 当前改动")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.buildRepositoryContext(ctx, &state, state.session.Workdir); !errors.Is(err, context.Canceled) {
		t.Fatalf("buildRepositoryContext(canceled) err = %v", err)
	}

	if got, err := service.buildRepositoryContext(context.Background(), nil, state.session.Workdir); err != nil || got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("buildRepositoryContext(nil state) = (%+v, %v)", got, err)
	}
	if got, err := service.buildRepositoryContext(context.Background(), &state, " "); err != nil || got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("buildRepositoryContext(empty workdir) = (%+v, %v)", got, err)
	}

	nonUserState := newRepositoryTestState(t.TempDir(), "ignored")
	nonUserState.session.Messages = []providertypes.Message{{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("assistant")},
	}}
	if got, err := service.buildRepositoryContext(context.Background(), &nonUserState, nonUserState.session.Workdir); err != nil || got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("buildRepositoryContext(no user text) = (%+v, %v)", got, err)
	}

	fatalFromChanged := &Service{
		repositoryService: &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{}, context.DeadlineExceeded
			},
		},
		events: make(chan RuntimeEvent, 8),
	}
	if _, err := fatalFromChanged.buildRepositoryContext(context.Background(), &state, state.session.Workdir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected fatal changed-files error, got %v", err)
	}

	fatalFromRetrieval := &Service{
		repositoryService: &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "a.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    1,
				}, nil
			},
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
				return nil, context.Canceled
			},
		},
		events: make(chan RuntimeEvent, 8),
	}
	retrievalState := newRepositoryTestState(t.TempDir(), "review 当前改动并看 internal/runtime/run.go")
	_, err := fatalFromRetrieval.buildRepositoryContext(context.Background(), &retrievalState, retrievalState.session.Workdir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected fatal retrieval error, got %v", err)
	}
}

func TestRepositoryContextBranchFunctions(t *testing.T) {
	t.Parallel()

	service := &Service{repositoryService: &stubRepositoryFactService{}, events: make(chan RuntimeEvent, 8)}
	workdir := t.TempDir()

	t.Run("repositoryFacts fallback", func(t *testing.T) {
		t.Parallel()

		if got := ((*Service)(nil)).repositoryFacts(); got == nil {
			t.Fatalf("expected default repository service for nil runtime")
		}
		if got := (&Service{}).repositoryFacts(); got == nil {
			t.Fatalf("expected default repository service for missing repositoryService")
		}
	})

	t.Run("changed files context decisions", func(t *testing.T) {
		t.Parallel()

		noIntent, err := service.maybeBuildChangedFilesContext(context.Background(), service.repositoryFacts(), workdir, "解释一下架构")
		if err != nil || noIntent != nil {
			t.Fatalf("maybeBuildChangedFilesContext(no intent) = (%+v, %v)", noIntent, err)
		}

		repoService := &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "a.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    1,
				}, nil
			},
		}
		section, err := service.maybeBuildChangedFilesContext(context.Background(), repoService, workdir, "当前改动有哪些")
		if err != nil || section == nil {
			t.Fatalf("maybeBuildChangedFilesContext(explicit) = (%+v, %v)", section, err)
		}
		if repoService.lastChangedOptions.IncludeSnippets {
			t.Fatalf("expected snippets disabled for explicit intent without snippet keywords")
		}

		repoService = &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "a.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    maxAutoChangedFilesCount + 1,
				}, nil
			},
		}
		section, err = service.maybeBuildChangedFilesContext(context.Background(), repoService, workdir, "请修复这个 bug")
		if err != nil || section != nil {
			t.Fatalf("expected oversized implicit changed files to be skipped, got (%+v, %v)", section, err)
		}

		repoService = &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{}, errors.New("changed failed")
			},
		}
		if _, err := service.maybeBuildChangedFilesContext(context.Background(), repoService, workdir, "请修复 bug"); err == nil {
			t.Fatalf("expected changed-files error")
		}

		repoService = &stubRepositoryFactService{
			changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
				return repository.ChangedFilesContext{}, nil
			},
		}
		section, err = service.maybeBuildChangedFilesContext(context.Background(), repoService, workdir, "请修复 bug 并 review diff")
		if err != nil || section != nil {
			t.Fatalf("expected nil changed-files section when no files returned, got (%+v, %v)", section, err)
		}
	})

	t.Run("retrieval context decisions", func(t *testing.T) {
		t.Parallel()

		repoService := &stubRepositoryFactService{}
		if query, ok := autoRetrievalQueryFromUserText("解释这个模块"); ok {
			t.Fatalf("expected no query, got %+v", query)
		}
		if repoService.retrieveCalls != 0 {
			t.Fatalf("expected no retrieval calls without anchors")
		}

		repoService = &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
				return nil, errors.New("retrieve failed")
			},
		}
		query, ok := autoRetrievalQueryFromUserText("请看 internal/runtime/run.go")
		if !ok {
			t.Fatalf("expected path query")
		}
		if _, err := service.buildRetrievalContextForQuery(context.Background(), repoService, workdir, query); err == nil {
			t.Fatalf("expected retrieval error")
		}

		repoService = &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
				return []repository.RetrievalHit{}, nil
			},
		}
		section, err := service.buildRetrievalContextForQuery(context.Background(), repoService, workdir, query)
		if err != nil || section != nil {
			t.Fatalf("expected nil retrieval section when no hits, got (%+v, %v)", section, err)
		}
	})
}

func TestRepositoryContextTextExtractionAndAnchors(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{
				providertypes.NewTextPart("assistant"),
			},
		},
		{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				{Kind: providertypes.ContentPartImage},
				providertypes.NewTextPart("  foo  "),
				providertypes.NewTextPart("bar"),
			},
		},
	}
	if got := latestUserText(messages); got != "foo\nbar" {
		t.Fatalf("latestUserText() = %q, want %q", got, "foo\nbar")
	}
	if got := latestUserText(nil); got != "" {
		t.Fatalf("latestUserText(nil) = %q, want empty", got)
	}

	if !shouldAutoInjectChangedFiles("请看 changed files") || shouldAutoInjectChangedFiles("just chat") {
		t.Fatalf("shouldAutoInjectChangedFiles() mismatch")
	}
	if shouldAutoInjectChangedFiles("   ") {
		t.Fatalf("expected empty input to not trigger changed-files injection")
	}
	if !shouldAutoIncludeChangedFileSnippets("please review diff") || shouldAutoIncludeChangedFileSnippets("just explain") {
		t.Fatalf("shouldAutoIncludeChangedFileSnippets() mismatch")
	}
	if shouldAutoIncludeChangedFileSnippets(" ") {
		t.Fatalf("expected empty input to not trigger snippet inclusion")
	}
	if !mentionsFixOrReviewIntent("debug this bug") || mentionsFixOrReviewIntent("architecture overview") {
		t.Fatalf("mentionsFixOrReviewIntent() mismatch")
	}
	if mentionsFixOrReviewIntent(" ") {
		t.Fatalf("expected empty input to not trigger fix/review intent")
	}

	if _, ok := autoPathRetrievalQuery("no path here"); ok {
		t.Fatalf("expected no path query")
	}
	if query, ok := autoPathRetrievalQuery("`internal\\runtime\\run.go`"); !ok || query.Mode != repository.RetrievalModePath {
		t.Fatalf("autoPathRetrievalQuery() = (%+v, %t)", query, ok)
	}

	if _, ok := autoSymbolRetrievalQuery("BuildWidget 在吗"); ok {
		t.Fatalf("expected symbol query to require intent words")
	}
	if query, ok := autoSymbolRetrievalQuery("where is BuildWidget"); !ok || query.Value != "BuildWidget" {
		t.Fatalf("autoSymbolRetrievalQuery() = (%+v, %t)", query, ok)
	}

	if _, ok := autoTextRetrievalQuery("find `internal/runtime/run.go`"); ok {
		t.Fatalf("expected path-like quoted text to be ignored")
	}
	if query, ok := autoTextRetrievalQuery("find `permission_requested`"); !ok || query.Value != "permission_requested" {
		t.Fatalf("autoTextRetrievalQuery() = (%+v, %t)", query, ok)
	}

	if query, ok := autoRetrievalQueryFromUserText("看看 internal/runtime/run.go 的 BuildWidget 和 `permission_requested`"); !ok || query.Mode != repository.RetrievalModePath {
		t.Fatalf("expected path query to win priority, got (%+v, %t)", query, ok)
	}

	if !isRepositoryContextFatalError(context.Canceled) || !isRepositoryContextFatalError(context.DeadlineExceeded) || isRepositoryContextFatalError(errors.New("x")) {
		t.Fatalf("isRepositoryContextFatalError() mismatch")
	}
}

func TestBuildRepositoryContextWithoutUserText(t *testing.T) {
	t.Parallel()

	session := agentsession.NewWithWorkdir("repo test", t.TempDir())
	session.Messages = []providertypes.Message{{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			{Kind: providertypes.ContentPartImage},
		},
	}}
	state := newRunState("run-no-user-text", session)
	service := &Service{repositoryService: &stubRepositoryFactService{}, events: make(chan RuntimeEvent, 8)}

	got, err := service.buildRepositoryContext(context.Background(), &state, session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() err = %v", err)
	}
	if got.ChangedFiles != nil || got.Retrieval != nil {
		t.Fatalf("expected empty repository context, got %+v", got)
	}
}
