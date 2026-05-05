package types

import (
	"strconv"
	"strings"
)

// ModelCapabilityState 表示模型能力提示的三态枚举。
type ModelCapabilityState string

const (
	ModelCapabilityStateUnknown     ModelCapabilityState = "unknown"
	ModelCapabilityStateUnsupported ModelCapabilityState = "unsupported"
	ModelCapabilityStateSupported   ModelCapabilityState = "supported"
)

// ModelCapabilityHints 描述 discovery/catalog 链路共享的模型能力提示。
type ModelCapabilityHints struct {
	ToolCalling           ModelCapabilityState `json:"tool_calling,omitempty"`
	ImageInput            ModelCapabilityState `json:"image_input,omitempty"`
	Thinking              ModelCapabilityState `json:"thinking,omitempty"`
	ThinkingEfforts       []string             `json:"thinking_efforts,omitempty"`
	ThinkingDefaultEffort string               `json:"thinking_default_effort,omitempty"`
	ThinkingForceEnabled  bool                 `json:"thinking_force_enabled,omitempty"`
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
	candidates := descriptorRawCandidates(raw)

	idKeys := []string{"id", "model_id", "modelId", "model_name", "modelName", "name"}
	nameKeys := []string{"display_name", "displayName", "displayname", "name", "model_name", "modelName"}
	descriptionKeys := []string{"description", "desc", "model_description", "modelDescription"}
	contextWindowKeys := []string{"context_window", "contextWindow", "contextLength", "input_token_limit", "inputTokenLimit", "max_context_tokens"}
	maxOutputKeys := []string{"max_output_tokens", "maxOutputTokens", "output_token_limit", "outputTokenLimit", "max_tokens", "maxTokens"}
	capabilityKeys := []string{"capabilities", "capability", "features"}

	id := firstNonEmptyString(
		stringValueFromCandidates(candidates, idKeys...),
		stringValue(lookupRawValue(raw, "model")),
	)
	if id == "" {
		return ModelDescriptor{}, false
	}

	descriptor := ModelDescriptor{
		ID: id,
		Name: firstNonEmptyString(
			stringValueFromCandidates(candidates, nameKeys...),
			id,
		),
		Description:     stringValueFromCandidates(candidates, descriptionKeys...),
		ContextWindow:   firstPositiveIntFromCandidates(candidates, contextWindowKeys...),
		MaxOutputTokens: firstPositiveIntFromCandidates(candidates, maxOutputKeys...),
		CapabilityHints: modelCapabilityHintsFromValue(lookupRawValueFromCandidates(candidates, capabilityKeys...)),
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
	if primary.Thinking == "" {
		primary.Thinking = secondary.Thinking
	}
	if len(primary.ThinkingEfforts) == 0 {
		primary.ThinkingEfforts = secondary.ThinkingEfforts
	}
	if primary.ThinkingDefaultEffort == "" {
		primary.ThinkingDefaultEffort = secondary.ThinkingDefaultEffort
	}
	if !primary.ThinkingForceEnabled {
		primary.ThinkingForceEnabled = secondary.ThinkingForceEnabled
	}
	return normalizeModelCapabilityHints(primary)
}

// normalizeModelCapabilityHints 规范化模型能力提示中的枚举字符串。
func normalizeModelCapabilityHints(hints ModelCapabilityHints) ModelCapabilityHints {
	hints.ToolCalling = normalizeModelCapabilityState(string(hints.ToolCalling))
	hints.ImageInput = normalizeModelCapabilityState(string(hints.ImageInput))
	hints.Thinking = normalizeModelCapabilityState(string(hints.Thinking))
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
		switch normalizeKey(key) {
		case "tool_calling", "tool_call":
			if boolValue, ok := item.(bool); ok {
				hints.ToolCalling = modelCapabilityStateFromBool(boolValue)
			}
		case "image_input":
			if boolValue, ok := item.(bool); ok {
				hints.ImageInput = modelCapabilityStateFromBool(boolValue)
			}
		case "thinking":
			if boolValue, ok := item.(bool); ok {
				hints.Thinking = modelCapabilityStateFromBool(boolValue)
			}
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
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

// lookupRawValue 按候选键读取字段，兼容大小写和下划线/连字符差异。
func lookupRawValue(raw map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value
		}
	}

	for _, key := range keys {
		target := normalizeFieldAliasKey(key)
		for currentKey, currentValue := range raw {
			if normalizeFieldAliasKey(currentKey) == target {
				return currentValue
			}
		}
	}
	return nil
}

// normalizeFieldAliasKey 统一字段别名比较规则：忽略大小写、下划线、连字符和空白。
func normalizeFieldAliasKey(key string) string {
	trimmed := strings.TrimSpace(strings.ToLower(key))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(trimmed)
}

// descriptorRawCandidates 生成 descriptor 字段读取候选对象，优先原始对象，再回退嵌套模型对象。
func descriptorRawCandidates(raw map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, 5)
	if raw != nil {
		candidates = append(candidates, raw)
	}

	for _, key := range []string{"model", "model_info", "modelInfo", "spec", "data"} {
		nested, ok := lookupRawValue(raw, key).(map[string]any)
		if !ok || len(nested) == 0 {
			continue
		}
		candidates = append(candidates, nested)
	}
	return candidates
}

// lookupRawValueFromCandidates 按候选对象顺序读取字段，支持第三方把模型元数据包装在嵌套对象中。
func lookupRawValueFromCandidates(candidates []map[string]any, keys ...string) any {
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if value := lookupRawValue(candidate, keys...); value != nil {
			return value
		}
	}
	return nil
}

// stringValueFromCandidates 按候选对象顺序提取首个非空字符串，遇到 map/array 等非字符串值会自动跳过。
func stringValueFromCandidates(candidates []map[string]any, keys ...string) string {
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		for _, key := range keys {
			if value := stringValue(lookupRawValue(candidate, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

// firstPositiveIntFromCandidates 按候选对象顺序提取首个正整数，兼容字符串与数字类型。
func firstPositiveIntFromCandidates(candidates []map[string]any, keys ...string) int {
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		values := make([]any, 0, len(keys))
		for _, key := range keys {
			values = append(values, lookupRawValue(candidate, key))
		}
		if result := firstPositiveInt(values...); result > 0 {
			return result
		}
	}
	return 0
}
