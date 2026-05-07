package runtime

import (
	"context"
	"errors"
	"testing"

	agentcontext "neo-code/internal/context"
	"neo-code/internal/repository"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type stubRepositoryFactService struct {
	inspectFn       func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error)
	retrieveFn      func(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error)
	inspectCalls    int
	retrieveCalls   int
	lastInspectOpts repository.InspectOptions
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

func TestBuildRepositoryContextReturnsSummaryOnly(t *testing.T) {
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
	if repoContext.ChangedFiles != nil || repoContext.Retrieval != nil {
		t.Fatalf("expected empty repository context, got %+v", repoContext)
	}
	if repoService.inspectCalls != 1 {
		t.Fatalf("expected a single inspect call, got %d", repoService.inspectCalls)
	}
	if repoService.lastInspectOpts.ChangedFilesLimit != 0 {
		t.Fatalf("expected no changed-files limit, got %+v", repoService.lastInspectOpts)
	}
}

func TestBuildRepositoryContextSkipsWithoutGitRepo(t *testing.T) {
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

func TestPrepareTurnBudgetSnapshotPassesRepositorySummaryToBuilder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	builder := &stubContextBuilder{}
	repoService := &stubRepositoryFactService{
		inspectFn: func(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error) {
			return repository.InspectResult{
				Summary: repository.Summary{InGitRepo: true, Branch: "main", Dirty: true},
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
	if builder.lastInput.Repository.ChangedFiles != nil {
		t.Fatalf("expected builder to receive no changed files context")
	}
	if builder.lastInput.RepositorySummary == nil || builder.lastInput.RepositorySummary.Branch != "main" {
		t.Fatalf("expected builder to receive repository summary, got %+v", builder.lastInput.RepositorySummary)
	}
}
