package runtime

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/repository"
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
}

func TestRepositoryContextHelpers(t *testing.T) {
	t.Parallel()

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
