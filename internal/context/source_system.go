package context

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type gitCommandRunner func(ctx context.Context, workdir string, args ...string) (string, error)

// collectSystemState 汇总运行时上下文，并通过一次 git status 调用获取分支与脏状态。
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

	statusOutput, err := runner(ctx, state.Workdir, "status", "--short", "--branch")
	if err != nil {
		if isContextError(err) {
			return state, err
		}
		return state, nil
	}

	state.Git = parseGitStatusSummary(statusOutput)
	return state, nil
}

// parseGitStatusSummary 解析 git status --short --branch 输出中的分支与脏状态。
func parseGitStatusSummary(output string) GitState {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			trimmed = append(trimmed, line)
		}
	}
	if len(trimmed) == 0 {
		return GitState{}
	}

	state := GitState{Available: true}
	firstLine := trimmed[0]
	if strings.HasPrefix(firstLine, "## ") {
		state.Branch = parseGitBranchLine(strings.TrimPrefix(firstLine, "## "))
		trimmed = trimmed[1:]
	}
	state.Dirty = len(trimmed) > 0
	return state
}

// parseGitBranchLine 从 git branch 摘要行中提取用户可读的分支名称。
func parseGitBranchLine(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(line, "No commits yet on "):
		return strings.TrimSpace(strings.TrimPrefix(line, "No commits yet on "))
	case strings.HasPrefix(line, "HEAD "):
		return "detached"
	default:
		if index := strings.Index(line, "..."); index >= 0 {
			line = line[:index]
		}
		return strings.TrimSpace(line)
	}
}

func renderSystemStateSection(state SystemState) promptSection {
	lines := []string{
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

	return promptSection{
		title:   "System State",
		content: strings.Join(lines, "\n"),
	}
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
