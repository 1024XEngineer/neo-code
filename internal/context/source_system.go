package context

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type gitCommandRunner func(ctx context.Context, workdir string, args ...string) (string, error)

func collectSystemState(ctx context.Context, metadata Metadata, runner gitCommandRunner) (SystemState, error) {
	state := SystemState{
		Workdir:  strings.TrimSpace(metadata.Workdir),
		Shell:    strings.TrimSpace(metadata.Shell),
		Provider: strings.TrimSpace(metadata.Provider),
		Model:    strings.TrimSpace(metadata.Model),
	}

	if err := ctx.Err(); err != nil {
		return state, err
	}
	if runner == nil || state.Workdir == "" {
		return state, nil
	}

	branch, err := runner(ctx, state.Workdir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		if isContextError(err) {
			return state, err
		}
		return state, nil
	}
	dirty, err := runner(ctx, state.Workdir, "status", "--porcelain")
	if err != nil {
		if isContextError(err) {
			return state, err
		}
		return state, nil
	}

	state.Git = GitState{
		Available: true,
		Branch:    strings.TrimSpace(branch),
		Dirty:     strings.TrimSpace(dirty) != "",
	}
	return state, nil
}

func renderSystemStateSection(state SystemState) string {
	lines := []string{
		"## System State",
		"",
		fmt.Sprintf("- workdir: `%s`", promptValue(state.Workdir)),
		fmt.Sprintf("- shell: `%s`", promptValue(state.Shell)),
		fmt.Sprintf("- provider: `%s`", promptValue(state.Provider)),
		fmt.Sprintf("- model: `%s`", promptValue(state.Model)),
	}

	if state.Git.Available {
		dirty := "clean"
		if state.Git.Dirty {
			dirty = "dirty"
		}
		lines = append(lines, fmt.Sprintf("- git: branch=`%s`, dirty=`%s`", promptValue(state.Git.Branch), dirty))
	} else {
		lines = append(lines, "- git: unavailable")
	}

	return strings.Join(lines, "\n")
}

func runGitCommand(ctx context.Context, workdir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", workdir}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func promptValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
