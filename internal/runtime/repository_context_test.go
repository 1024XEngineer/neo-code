package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	agentcontext "neo-code/internal/context"
	"neo-code/internal/context/repository"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type stubRepositoryFactService struct {
	inspectFn         func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error)
	retrieveFn        func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error)
	inspectCalls      int
	retrieveCalls     int
	lastInspectOpts   repository.InspectOptions
	lastRetrieveQuery repository.RetrievalQuery
}

func (s *stubRepositoryFactService) Inspect(
	ctx context.Context,
	workdir string,
	opts repository.InspectOptions,
) (repository.InspectResult, error) {
	s.inspectCalls++
	s.lastInspectOpts = opts
	if s.inspectFn != nil {
		return s.inspectFn(ctx, workdir, opts)
	}
	return repository.InspectResult{}, nil
}

func (s *stubRepositoryFactService) Retrieve(
	ctx context.Context,
	workdir string,
	query repository.RetrievalQuery,
) (repository.RetrievalResult, error) {
	s.retrieveCalls++
	s.lastRetrieveQuery = query
	if s.retrieveFn != nil {
		return s.retrieveFn(ctx, workdir, query)
	}
	return repository.RetrievalResult{}, nil
}

// newRepositoryTestState 构造带单条用户消息的最小 runState，便于验证 repository 触发条件。
func newRepositoryTestState(workdir string, text string) runState {
	session := agentsession.NewWithWorkdir("repo test", workdir)
	session.Messages = []providertypes.Message{{
		Role:  providertypes.RoleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(text)},
	}}
	return newRunState("run-repository-context", session)
}

func TestBuildRepositoryContextSkipsWithoutAnchors(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{}
	state := newRepositoryTestState(t.TempDir(), "解释一下 runtime 架构")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	summary, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if summary != nil {
		t.Fatalf("expected nil summary for non-git inspect result, got %+v", summary)
	}
	if repoContext.ChangedFiles != nil || repoContext.Retrieval != nil {
		t.Fatalf("expected empty repository context, got %+v", repoContext)
	}
	if repoService.inspectCalls != 1 || repoService.retrieveCalls != 0 {
		t.Fatalf("expected inspect once and no retrieval, got inspect=%d retrieve=%d", repoService.inspectCalls, repoService.retrieveCalls)
	}
}

func TestBuildRepositoryContextUsesInspectForSummaryAndChangedFiles(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{
					InGitRepo: true,
					Branch:    "feature/repository",
					Dirty:     true,
					Ahead:     2,
					Behind:    1,
				},
				ChangedFiles: repository.ChangedFilesContext{
					Files: []repository.ChangedFile{
						{Path: "internal/runtime/run.go", Status: repository.StatusModified, Snippet: "@@ snippet"},
					},
					ReturnedCount: 1,
					TotalCount:    1,
				},
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动并解释当前 diff")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	summary, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if summary == nil || summary.Branch != "feature/repository" || !summary.Dirty || summary.Ahead != 2 || summary.Behind != 1 {
		t.Fatalf("unexpected summary projection: %+v", summary)
	}
	if repoContext.ChangedFiles == nil || len(repoContext.ChangedFiles.Files) != 1 {
		t.Fatalf("expected changed files context, got %+v", repoContext.ChangedFiles)
	}
	if repoService.inspectCalls != 1 {
		t.Fatalf("expected a single inspect call, got %d", repoService.inspectCalls)
	}
	if !repoService.lastInspectOpts.IncludeChangedFileSnippets {
		t.Fatalf("expected snippets to be enabled, got %+v", repoService.lastInspectOpts)
	}
	if repoService.lastInspectOpts.ChangedFilesLimit != defaultAutoChangedFilesWithDiff {
		t.Fatalf("expected changed-files limit %d, got %+v", defaultAutoChangedFilesWithDiff, repoService.lastInspectOpts)
	}
	if repoService.lastInspectOpts.ChangedFileSnippetFileCountLimit != maxAutoSnippetChangedFilesCount {
		t.Fatalf("expected snippet file count limit %d, got %+v", maxAutoSnippetChangedFilesCount, repoService.lastInspectOpts)
	}
}

func TestBuildRepositoryContextSkipsImplicitLargeChangedSet(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{InGitRepo: true, Branch: "main", Dirty: true},
				ChangedFiles: repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    maxAutoChangedFilesCount + 1,
				},
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "fix 这个 bug")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles != nil {
		t.Fatalf("expected implicit large changed set to be skipped, got %+v", repoContext.ChangedFiles)
	}
	if repoService.inspectCalls != 1 {
		t.Fatalf("expected a single inspect call, got %d", repoService.inspectCalls)
	}
}

func TestBuildRepositoryContextInjectsExplicitLargeChangedSet(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{InGitRepo: true, Branch: "main", Dirty: true},
				ChangedFiles: repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    maxAutoChangedFilesCount + 5,
					Truncated:     true,
				},
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles == nil || repoContext.ChangedFiles.TotalCount <= maxAutoChangedFilesCount {
		t.Fatalf("expected explicit changed-files intent to keep truncated large set, got %+v", repoContext.ChangedFiles)
	}
}

func TestBuildRepositoryContextUsesPathRetrievalWithHighestPriority(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustRuntimeWriteFile(t, filepath.Join(workdir, "internal", "runtime", "run.go"), "package runtime\n")
	repoService := &stubRepositoryFactService{
		retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
			return repository.RetrievalResult{Hits: []repository.RetrievalHit{{
				Path:          "internal/runtime/run.go",
				Kind:          string(query.Mode),
				SymbolOrQuery: query.Value,
				Snippet:       "func ...",
				LineHint:      1,
			}}, Truncated: true}, nil
		},
	}
	state := newRepositoryTestState(workdir, "看看 internal/runtime/run.go 里 ExecuteSystemTool 是怎么处理的")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.Retrieval == nil {
		t.Fatalf("expected retrieval context")
	}
	if repoService.lastRetrieveQuery.Mode != repository.RetrievalModePath {
		t.Fatalf("expected path retrieval, got %+v", repoService.lastRetrieveQuery)
	}
	if !repoContext.Retrieval.Truncated {
		t.Fatalf("expected retrieval truncation to propagate")
	}
}

func TestBuildRepositoryContextSupportsRootFilePathAnchor(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustRuntimeWriteFile(t, filepath.Join(workdir, "README.md"), "# readme\n")
	repoService := &stubRepositoryFactService{
		retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
			return repository.RetrievalResult{Hits: []repository.RetrievalHit{{Path: "README.md", Kind: string(query.Mode), LineHint: 1}}}, nil
		},
	}
	state := newRepositoryTestState(workdir, "解释一下 README.md")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.Retrieval == nil || repoService.lastRetrieveQuery.Mode != repository.RetrievalModePath || repoService.lastRetrieveQuery.Value != "README.md" {
		t.Fatalf("expected root path retrieval, got context=%+v query=%+v", repoContext.Retrieval, repoService.lastRetrieveQuery)
	}
}

func TestBuildRepositoryContextUsesSymbolAndTextRetrievalAnchors(t *testing.T) {
	t.Parallel()

	t.Run("symbol anchor", func(t *testing.T) {
		repoService := &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
				return repository.RetrievalResult{Hits: []repository.RetrievalHit{{Path: "internal/runtime/system_tool.go", Kind: string(query.Mode), LineHint: 8}}}, nil
			},
		}
		state := newRepositoryTestState(t.TempDir(), "where is `ExecuteSystemTool`")
		service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

		_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
		if err != nil {
			t.Fatalf("buildRepositoryContext() error = %v", err)
		}
		if repoContext.Retrieval == nil || repoService.lastRetrieveQuery.Mode != repository.RetrievalModeSymbol {
			t.Fatalf("expected symbol retrieval, got context=%+v query=%+v", repoContext.Retrieval, repoService.lastRetrieveQuery)
		}
	})

	t.Run("quoted text anchor", func(t *testing.T) {
		repoService := &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
				return repository.RetrievalResult{Hits: []repository.RetrievalHit{{Path: "internal/runtime/events.go", Kind: string(query.Mode), LineHint: 14}}}, nil
			},
		}
		state := newRepositoryTestState(t.TempDir(), "找 `permission_requested` 在哪里处理")
		service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

		_, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
		if err != nil {
			t.Fatalf("buildRepositoryContext() error = %v", err)
		}
		if repoContext.Retrieval == nil || repoService.lastRetrieveQuery.Mode != repository.RetrievalModeText {
			t.Fatalf("expected text retrieval, got context=%+v query=%+v", repoContext.Retrieval, repoService.lastRetrieveQuery)
		}
	})
}

func TestPrepareTurnBudgetSnapshotPassesRepositoryContextToBuilder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	builder := &stubContextBuilder{}
	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{InGitRepo: true, Branch: "main", Dirty: true},
				ChangedFiles: repository.ChangedFilesContext{
					Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
					ReturnedCount: 1,
					TotalCount:    1,
				},
			}, nil
		},
	}

	service := &Service{
		configManager:     manager,
		contextBuilder:    builder,
		toolManager:       tools.NewRegistry(),
		repositoryService: repoService,
		providerFactory:   &scriptedProviderFactory{provider: &scriptedProvider{}},
		events:            make(chan RuntimeEvent, 8),
	}
	state := newRepositoryTestState(t.TempDir(), "请 review 当前改动")

	if _, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("expected rebuilt=false")
	}
	if builder.lastInput.Repository.ChangedFiles == nil {
		t.Fatalf("expected builder to receive changed files context")
	}
	if builder.lastInput.RepositorySummary == nil || builder.lastInput.RepositorySummary.Branch != "main" {
		t.Fatalf("expected builder to receive repository summary, got %+v", builder.lastInput.RepositorySummary)
	}
}

func TestBuildRepositoryContextEmitsUnavailableEventForSummaryFailure(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{}, errors.New("git unavailable")
		},
	}
	service := &Service{
		repositoryService: repoService,
		events:            make(chan RuntimeEvent, 8),
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动")

	summary, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if summary != nil || repoContext != (agentcontext.RepositoryContext{}) {
		t.Fatalf("expected empty repository projections on inspect failure, got summary=%+v context=%+v", summary, repoContext)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventRepositoryContextUnavailable)
	for _, event := range events {
		if event.Type != EventRepositoryContextUnavailable {
			continue
		}
		payload, ok := event.Payload.(RepositoryContextUnavailablePayload)
		if !ok {
			t.Fatalf("payload type = %T, want RepositoryContextUnavailablePayload", event.Payload)
		}
		if payload.Stage != "summary" || payload.Mode != "" || payload.Reason == "" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		return
	}
	t.Fatalf("expected repository unavailable event payload")
}

func TestBuildRepositoryContextEmitsUnavailableEventForRetrievalFailure(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mustRuntimeWriteFile(t, filepath.Join(workdir, "README.md"), "# readme\n")
	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{InGitRepo: true, Branch: "main"},
			}, nil
		},
		retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error) {
			return repository.RetrievalResult{}, errors.New("read failed")
		},
	}
	service := &Service{
		repositoryService: repoService,
		events:            make(chan RuntimeEvent, 8),
	}
	state := newRepositoryTestState(workdir, "看看 README.md")

	summary, repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if summary == nil || summary.Branch != "main" {
		t.Fatalf("expected summary to survive retrieval failure, got %+v", summary)
	}
	if repoContext != (agentcontext.RepositoryContext{}) {
		t.Fatalf("expected empty repository context on retrieval failure, got %+v", repoContext)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventRepositoryContextUnavailable)
	for _, event := range events {
		if event.Type != EventRepositoryContextUnavailable {
			continue
		}
		payload, ok := event.Payload.(RepositoryContextUnavailablePayload)
		if !ok {
			t.Fatalf("payload type = %T, want RepositoryContextUnavailablePayload", event.Payload)
		}
		if payload.Stage != "retrieval" || payload.Mode != "path" || payload.Reason == "" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		return
	}
	t.Fatalf("expected repository unavailable event payload")
}

func mustRuntimeWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
