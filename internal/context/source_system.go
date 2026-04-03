package context

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// gitCommandRunner 抽象 git 命令执行，方便测试替身注入。
type gitCommandRunner func(ctx context.Context, workdir string, args ...string) (string, error)

// collectSystemState 聚合运行时环境信息，并尽力探测 git 状态。
// 非上下文取消类 git 错误会被吞掉，以保证主流程可继续。
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

	// 当前分支读取失败时，不中断上下文构建；只有上下文取消才返回错误。
	branch, err := runner(ctx, state.Workdir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		if isContextError(err) {
			return state, err
		}
		return state, nil
	}
	// 通过 porcelain 输出是否为空判断工作区是否脏。
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

// renderSystemStateSection 将系统态转为结构化提示词文本。
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

// runGitCommand 使用 `git -C <workdir>` 在指定目录执行 git 子命令。
func runGitCommand(ctx context.Context, workdir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", workdir}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// promptValue 把空值规范为 unknown，避免向提示词暴露空字符串。
func promptValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

// isContextError 识别可传播的上下文取消/超时错误。
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
