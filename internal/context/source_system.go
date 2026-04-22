package context

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gitCommandTimeout 定义 git 命令的最大等待时间，避免网络挂载或损坏仓库阻塞上下文构建。
const gitCommandTimeout = 5 * time.Second

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
		state.Branch, state.Ahead, state.Behind = parseGitBranchLine(strings.TrimPrefix(firstLine, "## "))
		trimmed = trimmed[1:]
	}
	state.Dirty = len(trimmed) > 0
	return state
}

// parseGitBranchLine 从 git branch 摘要行中提取分支名与 ahead/behind 计数。
func parseGitBranchLine(line string) (string, int, int) {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return "", 0, 0
	case strings.HasPrefix(line, "No commits yet on "):
		return strings.TrimSpace(strings.TrimPrefix(line, "No commits yet on ")), 0, 0
	case strings.HasPrefix(line, "HEAD "):
		return "detached", 0, 0
	default:
		ahead, behind := parseGitTrackingCounters(line)
		if index := strings.Index(line, "..."); index >= 0 {
			line = line[:index]
		}
		return strings.TrimSpace(line), ahead, behind
	}
}

// parseGitTrackingCounters 解析 [ahead N, behind M] 片段中的追踪计数。
func parseGitTrackingCounters(line string) (int, int) {
	start := strings.Index(line, "[")
	end := strings.LastIndex(line, "]")
	if start < 0 || end <= start {
		return 0, 0
	}

	segment := strings.TrimSpace(line[start+1 : end])
	if segment == "" {
		return 0, 0
	}

	parts := strings.Split(segment, ",")
	ahead := 0
	behind := 0
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "ahead":
			ahead = value
		case "behind":
			behind = value
		}
	}
	return ahead, behind
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
		lines = append(lines, fmt.Sprintf(
			"- git: branch=`%s`, dirty=`%s`, ahead=`%d`, behind=`%d`",
			promptValue(state.Git.Branch),
			dirty,
			state.Git.Ahead,
			state.Git.Behind,
		))
	} else {
		lines = append(lines, "- git: unavailable")
	}

	return promptSection{
		Title:   "System State",
		Content: strings.Join(lines, "\n"),
	}
}

// runGitCommand 执行 git 命令并在超时后自动取消，避免阻塞上下文构建主链路。
func runGitCommand(ctx context.Context, workdir string, args ...string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	command := exec.CommandContext(timeoutCtx, "git", append([]string{"-C", workdir}, args...)...)
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
