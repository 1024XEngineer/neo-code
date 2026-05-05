package context

import (
	"context"
	"strings"

	"neo-code/internal/promptasset"
)

// capabilitiesSource 根据当前 PlanStage 动态注入能力声明。
type capabilitiesSource struct{}

// Sections 返回与当前模式匹配的能力与限制声明。
func (capabilitiesSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage := strings.TrimSpace(input.PlanStage)
	content := promptasset.CapabilitiesPrompt(stage)
	if content == "" {
		return nil, nil
	}

	return []promptSection{{
		Title:   "Capabilities & Limitations",
		Content: content,
	}}, nil
}
