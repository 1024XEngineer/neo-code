package context

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCollectSystemStateHandlesGitUnavailable(t *testing.T) {
	t.Parallel()

	state := collectSystemState(context.Background(), Metadata{
		Workdir:  "/workspace",
		Shell:    "bash",
		Provider: "openai",
		Model:    "gpt-5.4",
	}, func(ctx context.Context, workdir string, args ...string) (string, error) {
		return "", errors.New("git unavailable")
	})

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

	state := collectSystemState(context.Background(), Metadata{
		Workdir:  "/workspace",
		Shell:    "bash",
		Provider: "openai",
		Model:    "gpt-5.4",
	}, runner)

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
