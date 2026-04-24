package context

import (
	"context"
	"fmt"
	"strings"
)

// collectSystemState 汇总运行时上下文，并消费 runtime 已准备好的 repository summary 投影。
func collectSystemState(ctx context.Context, metadata Metadata, summary *RepositorySummarySection) (SystemState, error) {
	state := SystemState{
		Workdir:  strings.TrimSpace(metadata.Workdir),
		Shell:    strings.TrimSpace(metadata.Shell),
		Provider: strings.TrimSpace(metadata.Provider),
		Model:    strings.TrimSpace(metadata.Model),
	}

	if err := ctx.Err(); err != nil {
		return state, err
	}
	state.Git = toGitState(summary)
	return state, nil
}

// toGitState 将 runtime 提供的 repository summary 投影映射为最小 git 状态。
func toGitState(summary *RepositorySummarySection) GitState {
	if summary == nil || !summary.InGitRepo {
		return GitState{}
	}
	return GitState{
		Available: true,
		Branch:    strings.TrimSpace(summary.Branch),
		Dirty:     summary.Dirty,
		Ahead:     summary.Ahead,
		Behind:    summary.Behind,
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

func promptValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
