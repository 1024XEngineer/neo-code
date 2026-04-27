package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// MultiSourceLoader 按给定顺序加载多个 skills 源，并支持“前者覆盖后者”的合并策略。
type MultiSourceLoader struct {
	loaders []Loader
}

// NewMultiSourceLoader 创建多源 loader，传入顺序即优先级顺序（前高后低）。
func NewMultiSourceLoader(loaders ...Loader) *MultiSourceLoader {
	copied := make([]Loader, 0, len(loaders))
	for _, item := range loaders {
		if item == nil {
			continue
		}
		copied = append(copied, item)
	}
	return &MultiSourceLoader{loaders: copied}
}

// Load 逐源加载并合并快照；缺失 root 会被忽略，其他错误直接返回。
func (l *MultiSourceLoader) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if l == nil || len(l.loaders) == 0 {
		return Snapshot{}, fmt.Errorf("%w: empty sources", ErrSkillRootNotFound)
	}

	merged := Snapshot{
		Skills: make([]Skill, 0),
		Issues: make([]LoadIssue, 0),
	}
	seenFromHigher := make(map[string]struct{})
	loadedAny := false
	loadedAnyFatalIssue := false

	for _, loader := range l.loaders {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		sourceRoot := ""
		sourceLayer := ""
		if localLoader, ok := loader.(*LocalLoader); ok {
			sourceRoot = strings.TrimSpace(localLoader.root)
			sourceLayer = strings.TrimSpace(string(localLoader.layer))
		}
		snapshot, err := loader.Load(ctx)
		if err != nil {
			if errors.Is(err, ErrSkillRootNotFound) {
				continue
			}
			loadedAnyFatalIssue = true
			merged.Issues = append(merged.Issues, LoadIssue{
				Code:    IssueRefreshFailed,
				Path:    sourceRoot,
				Message: formatMultiSourceFailureMessage(sourceLayer),
				Err:     err,
			})
			continue
		}
		loadedAny = true
		merged.Issues = append(merged.Issues, snapshot.Issues...)

		// 同一来源内的重复 ID 仍交由 registry 做冲突判定，这里只处理跨来源覆盖。
		baseSeen := make(map[string]struct{}, len(seenFromHigher))
		for key := range seenFromHigher {
			baseSeen[key] = struct{}{}
		}
		for _, skill := range snapshot.Skills {
			key := normalizeSkillID(skill.Descriptor.ID)
			if key != "" {
				if _, exists := baseSeen[key]; exists {
					continue
				}
				seenFromHigher[key] = struct{}{}
			}
			merged.Skills = append(merged.Skills, skill)
		}
	}

	if !loadedAny && !loadedAnyFatalIssue {
		return Snapshot{}, fmt.Errorf("%w: all sources unavailable", ErrSkillRootNotFound)
	}
	return merged, nil
}

// formatMultiSourceFailureMessage 生成多源加载失败时的稳定 issue 信息。
func formatMultiSourceFailureMessage(sourceLayer string) string {
	layer := strings.TrimSpace(sourceLayer)
	if layer == "" {
		return "skill source refresh failed"
	}
	return "skill source refresh failed (" + layer + ")"
}
