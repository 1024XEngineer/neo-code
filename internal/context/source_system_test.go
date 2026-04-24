package context

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCollectSystemStateHandlesGitUnavailable(t *testing.T) {
	t.Parallel()

	state, err := collectSystemState(context.Background(), testMetadata("/workspace"), nil)
	if err != nil {
		t.Fatalf("collectSystemState() error = %v", err)
	}
	if state.Git.Available {
		t.Fatalf("expected git to be unavailable")
	}

	section := renderPromptSection(renderSystemStateSection(state))
	if !strings.Contains(section, "- git: unavailable") {
		t.Fatalf("expected unavailable git section, got %q", section)
	}
}

func TestCollectSystemStateIncludesRepositorySummary(t *testing.T) {
	t.Parallel()

	state, err := collectSystemState(context.Background(), testMetadata("/workspace"), &RepositorySummarySection{
		InGitRepo: true,
		Branch:    "feature/context",
		Dirty:     true,
		Ahead:     2,
		Behind:    1,
	})
	if err != nil {
		t.Fatalf("collectSystemState() error = %v", err)
	}
	if !state.Git.Available {
		t.Fatalf("expected git to be available")
	}
	if state.Git.Branch != "feature/context" {
		t.Fatalf("expected branch to be preserved, got %q", state.Git.Branch)
	}
	if !state.Git.Dirty || state.Git.Ahead != 2 || state.Git.Behind != 1 {
		t.Fatalf("unexpected git state: %+v", state.Git)
	}

	section := renderPromptSection(renderSystemStateSection(state))
	if !strings.Contains(section, "branch=`feature/context`") {
		t.Fatalf("expected branch in system section, got %q", section)
	}
	if !strings.Contains(section, "dirty=`dirty`") {
		t.Fatalf("expected dirty marker in system section, got %q", section)
	}
	if !strings.Contains(section, "ahead=`2`, behind=`1`") {
		t.Fatalf("expected ahead/behind counters in system section, got %q", section)
	}
}

func TestCollectSystemStateReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := collectSystemState(ctx, testMetadata("/workspace"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestSystemStateSourceSectionsReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := &systemStateSource{}
	_, err := source.Sections(ctx, BuildInput{Metadata: testMetadata("/workspace")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestCollectSystemStateTrimsMetadataAndLeavesGitUnavailableWithoutSummary(t *testing.T) {
	t.Parallel()

	state, err := collectSystemState(context.Background(), Metadata{
		Workdir:  " /workspace ",
		Shell:    " powershell ",
		Provider: " openai ",
		Model:    " gpt-test ",
	}, nil)
	if err != nil {
		t.Fatalf("collectSystemState() error = %v", err)
	}
	if state.Workdir != "/workspace" || state.Shell != "powershell" || state.Provider != "openai" || state.Model != "gpt-test" {
		t.Fatalf("unexpected trimmed state: %+v", state)
	}
	if state.Git.Available {
		t.Fatalf("expected git to stay unavailable without summary")
	}
}

func TestToGitStateMapsRepositorySummary(t *testing.T) {
	t.Parallel()

	state := toGitState(&RepositorySummarySection{
		InGitRepo: true,
		Branch:    "main",
		Ahead:     2,
		Behind:    3,
	})
	if !state.Available || state.Branch != "main" || state.Dirty {
		t.Fatalf("unexpected mapped state: %+v", state)
	}
	if state.Ahead != 2 || state.Behind != 3 {
		t.Fatalf("unexpected ahead/behind mapping: %+v", state)
	}

	unavailable := toGitState(nil)
	if unavailable.Available {
		t.Fatalf("expected unavailable state for nil summary, got %+v", unavailable)
	}
}
