package subagent

import (
	"context"
	"strings"
)

// defaultEngine 提供一个可运行的默认单步完成引擎，便于工厂与测试快速装配。
type defaultEngine struct{}

// RunStep 执行默认单步逻辑并返回结构化结果。
func (defaultEngine) RunStep(ctx context.Context, input StepInput) (StepOutput, error) {
	if err := ctx.Err(); err != nil {
		return StepOutput{}, err
	}

	summary := strings.TrimSpace(input.Task.ExpectedOutput)
	if summary == "" {
		summary = strings.TrimSpace(input.Task.Goal)
	}

	return StepOutput{
		Delta: "default engine completed",
		Done:  true,
		Output: Output{
			Summary:     summary,
			Findings:    []string{"default-engine: no extra findings"},
			Patches:     []string{"default-engine: no code changes"},
			Risks:       []string{"default-engine: output generated without external verification"},
			NextActions: []string{"replace default engine with role-specific implementation if deeper execution is required"},
			Artifacts:   []string{"default-engine"},
		},
	}, nil
}
