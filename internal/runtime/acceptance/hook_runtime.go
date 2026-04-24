package acceptance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

type prioritizedHook struct {
	priority int
	hook     Hook
}

// runConfiguredHooks 按 priority 与超时设置执行阶段 hook，确保验收链路可控。
func runConfiguredHooks(
	ctx context.Context,
	spec config.HookSpec,
	stage string,
	hooks []prioritizedHook,
	input FinalAcceptanceInput,
) error {
	if !spec.Enabled || len(hooks) == 0 {
		return nil
	}
	selected := make([]prioritizedHook, 0, len(hooks))
	for _, item := range hooks {
		if item.hook == nil {
			continue
		}
		// priority 作为最小执行门槛，允许通过配置裁剪低优先级 hook。
		if spec.Priority > 0 && item.priority < spec.Priority {
			continue
		}
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return nil
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].priority == selected[j].priority {
			return selected[i].hook.Name() < selected[j].hook.Name()
		}
		return selected[i].priority < selected[j].priority
	})
	for _, item := range selected {
		if err := runHookWithTimeout(ctx, spec.TimeoutSec, item.hook, input); err != nil {
			return fmt.Errorf("%s hook %q failed: %w", stage, item.hook.Name(), err)
		}
	}
	return nil
}

// isFailOpenPolicy 判断 hook 失败时是否允许继续验收流程。
func isFailOpenPolicy(policy string) bool {
	return strings.EqualFold(strings.TrimSpace(policy), hookFailurePolicyFailOpen)
}

// hookFailureDecision 将 hook 失败统一映射为结构化 failed 决策。
func hookFailureDecision(stage string, err error) AcceptanceDecision {
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		detail = "acceptance hook failed"
	}
	return AcceptanceDecision{
		Status:             AcceptanceFailed,
		StopReason:         controlplane.StopReasonVerificationExecutionError,
		ErrorClass:         verify.ErrorClassUnknown,
		UserVisibleSummary: "验收阶段执行失败，任务失败。",
		InternalSummary:    fmt.Sprintf("%s: %s", stage, detail),
		HasProgress:        false,
	}
}

type verifierSelectionHook struct {
	verifierCount int
}

// Name 返回 hook 标识，便于日志与错误定位。
func (h verifierSelectionHook) Name() string { return "verifier_selection" }

// Run 校验启用 verifier 时至少选择了一个执行器，避免空流水线误判通过。
func (h verifierSelectionHook) Run(_ context.Context, input FinalAcceptanceInput) error {
	if !input.VerificationInput.VerificationConfig.EnabledValue() {
		return nil
	}
	if h.verifierCount > 0 {
		return nil
	}
	return errors.New("verification enabled but no verifier selected")
}

type taskTypeMetadataHook struct{}

// Name 返回 hook 标识，便于日志与错误定位。
func (taskTypeMetadataHook) Name() string { return "task_type_metadata" }

// Run 校验 task_type 元数据不为空字符串，避免策略映射退化为隐式脏值。
func (taskTypeMetadataHook) Run(_ context.Context, input FinalAcceptanceInput) error {
	if len(input.VerificationInput.Metadata) == 0 {
		return nil
	}
	value, exists := input.VerificationInput.Metadata["task_type"]
	if !exists {
		return nil
	}
	if strings.TrimSpace(fmt.Sprintf("%v", value)) != "" {
		return nil
	}
	return errors.New("task_type metadata is empty")
}

type verificationResultHook struct {
	resultCount int
}

// Name 返回 hook 标识，便于日志与错误定位。
func (h verificationResultHook) Name() string { return "verification_results" }

// Run 校验 verifier 编排后存在结果输出，避免空结果链路被误判为通过。
func (h verificationResultHook) Run(_ context.Context, input FinalAcceptanceInput) error {
	if !input.VerificationInput.VerificationConfig.EnabledValue() {
		return nil
	}
	if h.resultCount > 0 {
		return nil
	}
	return errors.New("verification finished without results")
}

type maxTurnConsistencyHook struct{}

// Name 返回 hook 标识，便于日志与错误定位。
func (maxTurnConsistencyHook) Name() string { return "max_turn_consistency" }

// Run 校验 max turn 标志与阈值一致性，避免 stop reason 决策基于不完整事实。
func (maxTurnConsistencyHook) Run(_ context.Context, input FinalAcceptanceInput) error {
	if !input.MaxTurnsReached {
		return nil
	}
	if input.MaxTurnsLimit > 0 {
		return nil
	}
	return errors.New("max_turns_reached=true but max_turns_limit is empty")
}

// beforeVerificationHooks 返回 before_verification 阶段内置 hook 列表。
func beforeVerificationHooks(verifierCount int) []prioritizedHook {
	return []prioritizedHook{
		{priority: 10, hook: verifierSelectionHook{verifierCount: verifierCount}},
		{priority: 20, hook: taskTypeMetadataHook{}},
	}
}

// afterVerificationHooks 返回 after_verification 阶段内置 hook 列表。
func afterVerificationHooks(resultCount int) []prioritizedHook {
	return []prioritizedHook{
		{priority: 20, hook: verificationResultHook{resultCount: resultCount}},
	}
}

// beforeCompletionDecisionHooks 返回 before_completion_decision 阶段内置 hook 列表。
func beforeCompletionDecisionHooks() []prioritizedHook {
	return []prioritizedHook{
		{priority: 30, hook: maxTurnConsistencyHook{}},
	}
}
