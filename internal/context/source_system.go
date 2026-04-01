package context

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type gitCommandRunner func(ctx context.Context, workdir string, args ...string) (string, error)

func collectSystemState(ctx context.Context, metadata Metadata, runner gitCommandRunner) SystemState {
	state := SystemState{
		Workdir:  strings.TrimSpace(metadata.Workdir),
		Shell:    strings.TrimSpace(metadata.Shell),
		Provider: strings.TrimSpace(metadata.Provider),
		Model:    strings.TrimSpace(metadata.Model),
	}

	if err := ctx.Err(); err != nil {
		return state
	}
	if runner == nil || state.Workdir == "" {
		return state
	}

	branch, err := runner(ctx, state.Workdir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return state
	}
	dirty, err := runner(ctx, state.Workdir, "status", "--porcelain")
	if err != nil {
		return state
	}

	state.Git = GitState{
		Available: true,
		Branch:    strings.TrimSpace(branch),
		Dirty:     strings.TrimSpace(dirty) != "",
	}
	return state
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
