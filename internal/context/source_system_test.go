package context

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCollectSystemStateHandlesGitUnavailable(t *testing.T) {
	t.Parallel()

	state, err := collectSystemState(context.Background(), testMetadata("/workspace"), func(ctx context.Context, workdir string, args ...string) (string, error) {
		return "", errors.New("git unavailable")
	})
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

func TestCollectSystemStateIncludesGitSummaryFromSingleCall(t *testing.T) {
	t.Parallel()

	callCount := 0
	runner := func(ctx context.Context, workdir string, args ...string) (string, error) {
		callCount++
		if strings.Join(args, " ") != "status --short --branch" {
			return "", errors.New("unexpected git command")
		}
		return "## feature/context...origin/feature/context [ahead 2, behind 1]\n M internal/context/builder.go\n", nil
	}

	state, err := collectSystemState(context.Background(), testMetadata("/workspace"), runner)
	if err != nil {
		t.Fatalf("collectSystemState() error = %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected a single git call, got %d", callCount)
	}
	if !state.Git.Available {
		t.Fatalf("expected git to be available")
	}
	if state.Git.Branch != "feature/context" {
		t.Fatalf("expected branch to be trimmed, got %q", state.Git.Branch)
	}
	if !state.Git.Dirty {
		t.Fatalf("expected dirty git state")
	}
	if state.Git.Ahead != 2 || state.Git.Behind != 1 {
		t.Fatalf("expected ahead=2 behind=1, got %+v", state.Git)
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
	if strings.Contains(section, "internal/context/builder.go") {
		t.Fatalf("did not expect full git status output in system section, got %q", section)
	}
}

func TestCollectSystemStateReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := collectSystemState(ctx, testMetadata("/workspace"), func(ctx context.Context, workdir string, args ...string) (string, error) {
		return "", ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestSystemStateSourceSectionsReturnsRunnerContextError(t *testing.T) {
	t.Parallel()

	source := &systemStateSource{
		gitRunner: func(ctx context.Context, workdir string, args ...string) (string, error) {
			return "", context.DeadlineExceeded
		},
	}

	_, err := source.Sections(context.Background(), BuildInput{
		Metadata: testMetadata("/workspace"),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestCollectSystemStateSkipsGitSummaryWhenRunnerUnavailableOrWorkdirBlank(t *testing.T) {
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
	if state.Git.Available {
		t.Fatalf("expected git to stay unavailable without runner")
	}
	if state.Workdir != "/workspace" {
		t.Fatalf("expected trimmed workdir, got %q", state.Workdir)
	}

	state, err = collectSystemState(context.Background(), Metadata{
		Workdir:  " ",
		Shell:    " bash ",
		Provider: " local ",
		Model:    " mini ",
	}, func(ctx context.Context, workdir string, args ...string) (string, error) {
		t.Fatalf("runner should not be called for blank workdir")
		return "", nil
	})
	if err != nil {
		t.Fatalf("collectSystemState() blank workdir error = %v", err)
	}
	if state.Git.Available {
		t.Fatalf("expected git to stay unavailable for blank workdir")
	}
}

func TestParseGitStatusSummaryHandlesCleanDetachedAndBranchlessOutput(t *testing.T) {
	t.Parallel()

	cleanState := parseGitStatusSummary("## main...origin/main\n")
	if !cleanState.Available || cleanState.Branch != "main" || cleanState.Dirty {
		t.Fatalf("unexpected clean state: %+v", cleanState)
	}

	dirtyWithoutBranch := parseGitStatusSummary(" M internal/context/builder.go\n")
	if !dirtyWithoutBranch.Available || dirtyWithoutBranch.Branch != "" || !dirtyWithoutBranch.Dirty {
		t.Fatalf("unexpected dirty state without branch header: %+v", dirtyWithoutBranch)
	}

	branch, ahead, behind := parseGitBranchLine("No commits yet on feature/bootstrap")
	if branch != "feature/bootstrap" {
		t.Fatalf("expected unborn branch name, got %q", branch)
	}
	if ahead != 0 || behind != 0 {
		t.Fatalf("expected unborn branch counters to be zero, got ahead=%d behind=%d", ahead, behind)
	}
	branch, ahead, behind = parseGitBranchLine("HEAD detached at abc123")
	if branch != "detached" {
		t.Fatalf("expected detached HEAD marker, got %q", branch)
	}
	if ahead != 0 || behind != 0 {
		t.Fatalf("expected detached counters to be zero, got ahead=%d behind=%d", ahead, behind)
	}

	branch, ahead, behind = parseGitBranchLine("main...origin/main [ahead 2, behind 3]")
	if branch != "main" || ahead != 2 || behind != 3 {
		t.Fatalf("expected ahead/behind parsed, got branch=%q ahead=%d behind=%d", branch, ahead, behind)
	}
}
