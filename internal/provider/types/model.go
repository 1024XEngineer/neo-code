package types

import "strings"

// ModelCapabilityState 表示模型能力提示的三态枚举。
type ModelCapabilityState string

const (
	ModelCapabilityStateUnknown     ModelCapabilityState = "unknown"
	ModelCapabilityStateUnsupported ModelCapabilityState = "unsupported"
	ModelCapabilityStateSupported   ModelCapabilityState = "supported"
)

// ModelReasoningMode 表示模型推理能力的模式提示。
type ModelReasoningMode string

const (
	ModelReasoningModeUnknown      ModelReasoningMode = "unknown"
	ModelReasoningModeNone         ModelReasoningMode = "none"
	ModelReasoningModeNative       ModelReasoningMode = "native"
	ModelReasoningModeConfigurable ModelReasoningMode = "configurable"
)

// ModelCapabilityHints 描述 discovery/catalog 链路共享的模型能力提示。
type ModelCapabilityHints struct {
	ToolCalling   ModelCapabilityState `json:"tool_calling,omitempty"`
	ImageInput    ModelCapabilityState `json:"image_input,omitempty"`
	ReasoningMode ModelReasoningMode   `json:"reasoning_mode,omitempty"`
}

// ModelDescriptor 表示 discovery/catalog 链路共享的模型元数据描述符。
type ModelDescriptor struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description,omitempty"`
	ContextWindow   int                  `json:"context_window,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	CapabilityHints ModelCapabilityHints `json:"capability_hints,omitempty"`
}

// DescriptorFromRawModel 将原始 provider 模型对象标准化为 ModelDescriptor。
func DescriptorFromRawModel(raw map[string]any) (ModelDescriptor, bool) {
	id := firstNonEmptyString(
		stringValue(raw["id"]),
		stringValue(raw["model"]),
		stringValue(raw["name"]),
	)
	if id == "" {
		return ModelDescriptor{}, false
	}

	descriptor := ModelDescriptor{
		ID:              id,
		Name:            firstNonEmptyString(stringValue(raw["name"]), stringValue(raw["display_name"]), id),
		Description:     stringValue(raw["description"]),
		ContextWindow:   firstPositiveInt(raw["context_window"], raw["contextLength"], raw["input_token_limit"], raw["max_context_tokens"]),
		MaxOutputTokens: firstPositiveInt(raw["max_output_tokens"], raw["output_token_limit"], raw["max_tokens"]),
		CapabilityHints: modelCapabilityHintsFromValue(raw["capabilities"]),
	}
	return normalizeModelDescriptor(descriptor), true
}

// MergeModelDescriptors 合并多个 ModelDescriptor 来源，按 ID 去重并回填缺失字段。
func MergeModelDescriptors(sources ...[]ModelDescriptor) []ModelDescriptor {
	if len(sources) == 0 {
		return nil
	}

	merged := make([]ModelDescriptor, 0)
	indexByID := make(map[string]int)

	for _, source := range sources {
		for _, candidate := range source {
			normalized := normalizeModelDescriptor(candidate)
			key := normalizeKey(normalized.ID)
			if key == "" {
				continue
			}

			if index, exists := indexByID[key]; exists {
				merged[index] = mergeModelDescriptor(merged[index], normalized)
				continue
			}

			indexByID[key] = len(merged)
			merged = append(merged, normalized)
		}
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}

// DescriptorsFromIDs 从模型 ID 列表构建最小化的 ModelDescriptor 列表。
func DescriptorsFromIDs(modelIDs []string) []ModelDescriptor {
	if len(modelIDs) == 0 {
		return nil
	}

	descriptors := make([]ModelDescriptor, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		id := strings.TrimSpace(modelID)
		if id == "" {
			continue
		}
		descriptors = append(descriptors, ModelDescriptor{
			ID:   id,
			Name: id,
		})
	}
	if len(descriptors) == 0 {
		return nil
	}
	return descriptors
}

// CloneModelDescriptors 返回模型描述列表的深拷贝，避免不同快照共享底层切片。
func CloneModelDescriptors(source []ModelDescriptor) []ModelDescriptor {
	if len(source) == 0 {
		return nil
	}

	cloned := make([]ModelDescriptor, 0, len(source))
	for _, descriptor := range source {
		cloned = append(cloned, normalizeModelDescriptor(descriptor))
	}
	return cloned
}

// EqualModelDescriptors 判断两个模型描述列表是否表示同一份规范化快照。
func EqualModelDescriptors(left []ModelDescriptor, right []ModelDescriptor) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if normalizeModelDescriptor(left[index]) != normalizeModelDescriptor(right[index]) {
			return false
		}
	}
	return true
}

// normalizeModelDescriptor 统一清理模型描述中的字符串和能力提示字段。
func normalizeModelDescriptor(descriptor ModelDescriptor) ModelDescriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Name = strings.TrimSpace(descriptor.Name)
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	if descriptor.Name == "" {
		descriptor.Name = descriptor.ID
	}
	descriptor.CapabilityHints = normalizeModelCapabilityHints(descriptor.CapabilityHints)
	return descriptor
}

// mergeModelDescriptor 以 primary 为准合并模型描述，仅用 secondary 回填缺失字段。
func mergeModelDescriptor(primary ModelDescriptor, secondary ModelDescriptor) ModelDescriptor {
	if strings.TrimSpace(primary.Name) == "" {
		primary.Name = secondary.Name
	}
	if strings.TrimSpace(primary.Description) == "" {
		primary.Description = secondary.Description
	}
	if primary.ContextWindow <= 0 {
		primary.ContextWindow = secondary.ContextWindow
	}
	if primary.MaxOutputTokens <= 0 {
		primary.MaxOutputTokens = secondary.MaxOutputTokens
	}
	primary.CapabilityHints = mergeModelCapabilityHints(primary.CapabilityHints, secondary.CapabilityHints)
	return normalizeModelDescriptor(primary)
}

// mergeModelCapabilityHints 以 primary 为准合并模型能力提示，仅用 secondary 回填缺失值。
func mergeModelCapabilityHints(primary ModelCapabilityHints, secondary ModelCapabilityHints) ModelCapabilityHints {
	if primary.ToolCalling == "" {
		primary.ToolCalling = secondary.ToolCalling
	}
	if primary.ImageInput == "" {
		primary.ImageInput = secondary.ImageInput
	}
	if primary.ReasoningMode == "" {
		primary.ReasoningMode = secondary.ReasoningMode
	}
	return normalizeModelCapabilityHints(primary)
}

// normalizeModelCapabilityHints 规范化模型能力提示中的枚举字符串。
func normalizeModelCapabilityHints(hints ModelCapabilityHints) ModelCapabilityHints {
	hints.ToolCalling = normalizeModelCapabilityState(string(hints.ToolCalling))
	hints.ImageInput = normalizeModelCapabilityState(string(hints.ImageInput))
	hints.ReasoningMode = normalizeModelReasoningMode(string(hints.ReasoningMode))
	return hints
}

// modelCapabilityHintsFromValue 将原始 capabilities 值收敛为统一的三态能力提示。
func modelCapabilityHintsFromValue(value any) ModelCapabilityHints {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return ModelCapabilityHints{}
	}

	hints := ModelCapabilityHints{}
	for key, item := range raw {
		boolValue, ok := item.(bool)
		if !ok {
			continue
		}

		switch normalizeKey(key) {
		case "tool_calling", "tool_call":
			hints.ToolCalling = modelCapabilityStateFromBool(boolValue)
		case "image_input":
			hints.ImageInput = modelCapabilityStateFromBool(boolValue)
		}
	}
	return normalizeModelCapabilityHints(hints)
}

// modelCapabilityStateFromBool 将旧式 bool 能力值映射为三态能力提示。
func modelCapabilityStateFromBool(value bool) ModelCapabilityState {
	if value {
		return ModelCapabilityStateSupported
	}
	return ModelCapabilityStateUnsupported
}

// normalizeModelCapabilityState 将能力状态字符串收敛为受支持的三态枚举。
func normalizeModelCapabilityState(value string) ModelCapabilityState {
	switch ModelCapabilityState(normalizeKey(value)) {
	case ModelCapabilityStateSupported:
		return ModelCapabilityStateSupported
	case ModelCapabilityStateUnsupported:
		return ModelCapabilityStateUnsupported
	case ModelCapabilityStateUnknown:
		return ModelCapabilityStateUnknown
	default:
		return ""
	}
}

// normalizeModelReasoningMode 将 reasoning_mode 字符串规范化为受支持的枚举值。
func normalizeModelReasoningMode(value string) ModelReasoningMode {
	switch ModelReasoningMode(normalizeKey(value)) {
	case ModelReasoningModeNone:
		return ModelReasoningModeNone
	case ModelReasoningModeNative:
		return ModelReasoningModeNative
	case ModelReasoningModeConfigurable:
		return ModelReasoningModeConfigurable
	case ModelReasoningModeUnknown:
		return ModelReasoningModeUnknown
	default:
		return ""
	}
}

// normalizeKey 统一执行大小写折叠与空白清理，保证跨层比较稳定。
func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// stringValue 从通用值中提取字符串并做空白裁剪。
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

// firstNonEmptyString 返回首个非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// firstPositiveInt 返回首个大于零的整数值。
func firstPositiveInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			if typed > 0 {
				return typed
			}
		case int32:
			if typed > 0 {
				return int(typed)
			}
		case int64:
			if typed > 0 {
				return int(typed)
			}
		case float32:
			if typed > 0 {
				return int(typed)
			}
		case float64:
			if typed > 0 {
				return int(typed)
			}
		}
	}
	return 0
}
