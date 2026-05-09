package runtime

import (
	"fmt"

	providertypes "neo-code/internal/provider/types"
)

// resolveThinkingConfig 根据用户覆盖、全局开关和模型能力构建 ThinkingConfig。
// 返回 nil 表示不传递 thinking 控制参数（unsupported 模型）。
func resolveThinkingConfig(
	caps providertypes.ModelCapabilityHints,
	override *ThinkingOverride,
	globalEnabled bool,
) (*providertypes.ThinkingConfig, error) {
	thinkingState := caps.Thinking
	if thinkingState == "" {
		thinkingState = providertypes.ModelCapabilityStateUnknown
	}

	switch thinkingState {
	case providertypes.ModelCapabilityStateUnsupported:
		return nil, nil
	case providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateUnknown:
		// 继续处理
	default:
		return nil, nil
	}

	enabled := globalEnabled
	explicitOverride := false
	if override != nil && override.Enabled != nil {
		enabled = *override.Enabled
		explicitOverride = true
	}
	// ThinkingForceEnabled 模型强制开启
	if caps.ThinkingForceEnabled {
		enabled = true
	}
	if explicitOverride && override != nil && override.Enabled != nil && !*override.Enabled {
		enabled = false
	}
	if !enabled {
		return &providertypes.ThinkingConfig{Enabled: false}, nil
	}

	effort := caps.ThinkingDefaultEffort
	if override != nil && override.Effort != "" {
		effort = override.Effort
		// 校验 effort 在列表内
		if len(caps.ThinkingEfforts) > 0 && !containsEffort(caps.ThinkingEfforts, effort) {
			return nil, fmt.Errorf("runtime: thinking effort %q not in supported list %v", effort, caps.ThinkingEfforts)
		}
	}
	// 空列表时不为空 effort
	if len(caps.ThinkingEfforts) == 0 && effort != "" {
		effort = ""
	}

	return &providertypes.ThinkingConfig{
		Enabled: enabled,
		Effort:  effort,
	}, nil
}

func containsEffort(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func cloneThinkingOverride(override *ThinkingOverride) *ThinkingOverride {
	if override == nil {
		return nil
	}
	cloned := &ThinkingOverride{
		Effort: override.Effort,
	}
	if override.Enabled != nil {
		enabled := *override.Enabled
		cloned.Enabled = &enabled
	}
	return cloned
}

// modelCapabilityHintsForRequest 从 provider 配置的静态模型列表中查找能力提示。
func modelCapabilityHintsForRequest(model string, models []providertypes.ModelDescriptor) providertypes.ModelCapabilityHints {
	for _, m := range models {
		if m.ID == model {
			return m.CapabilityHints
		}
	}
	return providertypes.ModelCapabilityHints{}
}
