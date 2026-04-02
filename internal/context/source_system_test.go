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

	section := renderSystemStateSection(state)
	if !strings.Contains(section, "- git: unavailable") {
		t.Fatalf("expected unavailable git section, got %q", section)
	}
}

func TestCollectSystemStateIncludesGitSummary(t *testing.T) {
	t.Parallel()

	runner := func(ctx context.Context, workdir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --abbrev-ref HEAD":
			return "feature/context\n", nil
		case "status --porcelain":
			return " M internal/context/builder.go\n", nil
		default:
			return "", errors.New("unexpected git command")
		}
	}

	state, err := collectSystemState(context.Background(), testMetadata("/workspace"), runner)
	if err != nil {
		t.Fatalf("collectSystemState() error = %v", err)
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

	section := renderSystemStateSection(state)
	if !strings.Contains(section, "branch=`feature/context`") {
		t.Fatalf("expected branch in system section, got %q", section)
	}
	if !strings.Contains(section, "dirty=`dirty`") {
		t.Fatalf("expected dirty marker in system section, got %q", section)
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
