package runtime

import (
	"context"
	"errors"
	"testing"

	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/repository"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type stubRepositoryFactService struct {
	changedFilesFn     func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error)
	retrieveFn         func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error)
	changedFilesCalls  int
	retrieveCalls      int
	lastChangedOptions repository.ChangedFilesOptions
	lastRetrieveQuery  repository.RetrievalQuery
}

func (s *stubRepositoryFactService) ChangedFiles(
	ctx context.Context,
	workdir string,
	opts repository.ChangedFilesOptions,
) (repository.ChangedFilesContext, error) {
	s.changedFilesCalls++
	s.lastChangedOptions = opts
	if s.changedFilesFn != nil {
		return s.changedFilesFn(ctx, workdir, opts)
	}
	return repository.ChangedFilesContext{}, nil
}

func (s *stubRepositoryFactService) Retrieve(
	ctx context.Context,
	workdir string,
	query repository.RetrievalQuery,
) ([]repository.RetrievalHit, error) {
	s.retrieveCalls++
	s.lastRetrieveQuery = query
	if s.retrieveFn != nil {
		return s.retrieveFn(ctx, workdir, query)
	}
	return nil, nil
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

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles != nil || repoContext.Retrieval != nil {
		t.Fatalf("expected empty repository context, got %+v", repoContext)
	}
	if repoService.changedFilesCalls != 0 || repoService.retrieveCalls != 0 {
		t.Fatalf("expected no repository calls, got changed=%d retrieve=%d", repoService.changedFilesCalls, repoService.retrieveCalls)
	}
}

func TestBuildRepositoryContextUsesChangedFilesForCurrentDiffRequest(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
			return repository.ChangedFilesContext{
				Files: []repository.ChangedFile{
					{Path: "internal/runtime/run.go", Status: repository.StatusModified, Snippet: "@@ snippet"},
				},
				ReturnedCount: 1,
				TotalCount:    1,
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动并解释当前 diff")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles == nil || len(repoContext.ChangedFiles.Files) != 1 {
		t.Fatalf("expected changed files context, got %+v", repoContext.ChangedFiles)
	}
	if repoService.changedFilesCalls != 1 {
		t.Fatalf("expected a single changed-files scan, got %d", repoService.changedFilesCalls)
	}
	if !repoService.lastChangedOptions.IncludeSnippets {
		t.Fatalf("expected snippets to be enabled, got %+v", repoService.lastChangedOptions)
	}
	if repoService.lastChangedOptions.Limit != defaultAutoChangedFilesWithDiff {
		t.Fatalf("expected changed-files limit %d, got %+v", defaultAutoChangedFilesWithDiff, repoService.lastChangedOptions)
	}
	if repoService.lastChangedOptions.SnippetFileCountLimit != maxAutoSnippetChangedFilesCount {
		t.Fatalf("expected snippet file count limit %d, got %+v", maxAutoSnippetChangedFilesCount, repoService.lastChangedOptions)
	}
}

func TestBuildRepositoryContextSkipsImplicitLargeChangedSet(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
			return repository.ChangedFilesContext{
				Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
				ReturnedCount: 1,
				TotalCount:    maxAutoChangedFilesCount + 1,
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "fix 这个 bug")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles != nil {
		t.Fatalf("expected implicit large changed set to be skipped, got %+v", repoContext.ChangedFiles)
	}
	if repoService.changedFilesCalls != 1 {
		t.Fatalf("expected a single changed-files call, got %d", repoService.changedFilesCalls)
	}
}

func TestBuildRepositoryContextInjectsExplicitLargeChangedSet(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
			return repository.ChangedFilesContext{
				Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
				ReturnedCount: 1,
				TotalCount:    maxAutoChangedFilesCount + 5,
				Truncated:     true,
			}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.ChangedFiles == nil || repoContext.ChangedFiles.TotalCount <= maxAutoChangedFilesCount {
		t.Fatalf("expected explicit changed-files intent to keep truncated large set, got %+v", repoContext.ChangedFiles)
	}
}

func TestBuildRepositoryContextUsesPathRetrievalWithHighestPriority(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
			return []repository.RetrievalHit{{
				Path:          "internal/runtime/run.go",
				Kind:          string(query.Mode),
				SymbolOrQuery: query.Value,
				Snippet:       "func ...",
				LineHint:      1,
			}}, nil
		},
	}
	state := newRepositoryTestState(t.TempDir(), "看看 internal/runtime/run.go 里 ExecuteSystemTool 是怎么处理的")
	service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext.Retrieval == nil {
		t.Fatalf("expected retrieval context")
	}
	if repoService.lastRetrieveQuery.Mode != repository.RetrievalModePath {
		t.Fatalf("expected path retrieval, got %+v", repoService.lastRetrieveQuery)
	}
}

func TestBuildRepositoryContextUsesSymbolAndTextRetrievalAnchors(t *testing.T) {
	t.Parallel()

	t.Run("symbol anchor", func(t *testing.T) {
		repoService := &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
				return []repository.RetrievalHit{{Path: "internal/runtime/system_tool.go", Kind: string(query.Mode), LineHint: 8}}, nil
			},
		}
		state := newRepositoryTestState(t.TempDir(), "ExecuteSystemTool 在哪定义，帮我解释一下")
		service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

		repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
		if err != nil {
			t.Fatalf("buildRepositoryContext() error = %v", err)
		}
		if repoContext.Retrieval == nil || repoService.lastRetrieveQuery.Mode != repository.RetrievalModeSymbol {
			t.Fatalf("expected symbol retrieval, got context=%+v query=%+v", repoContext.Retrieval, repoService.lastRetrieveQuery)
		}
	})

	t.Run("quoted text anchor", func(t *testing.T) {
		repoService := &stubRepositoryFactService{
			retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
				return []repository.RetrievalHit{{Path: "internal/runtime/events.go", Kind: string(query.Mode), LineHint: 14}}, nil
			},
		}
		state := newRepositoryTestState(t.TempDir(), "找 `permission_requested` 在哪里处理")
		service := &Service{repositoryService: repoService, events: make(chan RuntimeEvent, 8)}

		repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
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
		changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
			return repository.ChangedFilesContext{
				Files:         []repository.ChangedFile{{Path: "internal/runtime/run.go", Status: repository.StatusModified}},
				ReturnedCount: 1,
				TotalCount:    1,
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
}

func TestBuildRepositoryContextEmitsUnavailableEventForChangedFilesFailure(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		changedFilesFn: func(ctx context.Context, workdir string, opts repository.ChangedFilesOptions) (repository.ChangedFilesContext, error) {
			return repository.ChangedFilesContext{}, errors.New("git unavailable")
		},
	}
	service := &Service{
		repositoryService: repoService,
		events:            make(chan RuntimeEvent, 8),
	}
	state := newRepositoryTestState(t.TempDir(), "review 我的改动")

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
	}
	if repoContext != (agentcontext.RepositoryContext{}) {
		t.Fatalf("expected empty repository context on failure, got %+v", repoContext)
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
		if payload.Stage != "changed_files" || payload.Mode != "" || payload.Reason == "" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		return
	}
	t.Fatalf("expected repository unavailable event payload")
}

func TestBuildRepositoryContextEmitsUnavailableEventForRetrievalFailure(t *testing.T) {
	t.Parallel()

	repoService := &stubRepositoryFactService{
		retrieveFn: func(ctx context.Context, workdir string, query repository.RetrievalQuery) ([]repository.RetrievalHit, error) {
			return nil, errors.New("read failed")
		},
	}
	service := &Service{
		repositoryService: repoService,
		events:            make(chan RuntimeEvent, 8),
	}
	state := newRepositoryTestState(t.TempDir(), "找 `permission_requested` 在哪里处理")

	repoContext, err := service.buildRepositoryContext(context.Background(), &state, state.session.Workdir)
	if err != nil {
		t.Fatalf("buildRepositoryContext() error = %v", err)
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
		if payload.Stage != "retrieval" || payload.Mode != "text" || payload.Reason == "" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		return
	}
	t.Fatalf("expected repository unavailable event payload")
}
