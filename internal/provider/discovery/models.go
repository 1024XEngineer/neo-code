package discovery

import (
	"fmt"
	"strings"

	"neo-code/internal/provider"
)

// ExtractRawModels 兼容不同供应商的模型列表响应结构，统一提取为模型对象切片。
func ExtractRawModels(payload any, profile string) ([]map[string]any, error) {
	if payload == nil {
		return nil, nil
	}

	if models, isArrayPayload, err := decodeRootArrayPayload(payload); isArrayPayload || err != nil {
		return models, err
	}

	objectPayload, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unsupported models payload type %T", payload)
	}

	normalizedProfile := normalizeResponseProfile(profile)
	keys := modelListKeys(normalizedProfile)
	models, found, err := extractRawModelsFromObject(objectPayload, keys)
	if err != nil {
		return nil, err
	}
	if found {
		return models, nil
	}

	// 严格 profile 未命中时，回退到 generic 键集合，降低第三方 OpenAI-compatible 变体接入成本。
	if normalizedProfile != provider.DiscoveryResponseProfileGeneric {
		fallbackKeys := modelListKeys(provider.DiscoveryResponseProfileGeneric)
		models, found, err = extractRawModelsFromObject(objectPayload, fallbackKeys)
		if err != nil {
			return nil, err
		}
		if found {
			return models, nil
		}
	}

	if looksLikeSingleModelObject(objectPayload) {
		return []map[string]any{objectPayload}, nil
	}

	// 对未知容器键做受控深度遍历兜底，仅在“看起来像模型集合”时返回结果，避免误识别无关数组。
	if models, found, err := extractRawModelsByTraversal(objectPayload, "", 0); err != nil {
		return nil, err
	} else if found {
		return models, nil
	}

	return nil, fmt.Errorf("models payload does not contain supported list keys %q", strings.Join(keys, ", "))
}

// extractRawModelsFromObject 在对象中按候选键提取模型列表，支持 data.models 一类嵌套容器。
func extractRawModelsFromObject(objectPayload map[string]any, keys []string) ([]map[string]any, bool, error) {
	for _, key := range keys {
		value, exists := lookupObjectValue(objectPayload, key)
		if !exists {
			continue
		}

		if nestedObject, ok := value.(map[string]any); ok {
			if looksLikeSingleModelObject(nestedObject) {
				return []map[string]any{nestedObject}, true, nil
			}
			nestedModels, nestedFound, nestedErr := extractRawModelsFromObject(nestedObject, keys)
			if nestedErr != nil {
				return nil, true, nestedErr
			}
			if nestedFound {
				return nestedModels, true, nil
			}
			continue
		}

		models, err := decodeModelEntries(value)
		if err != nil {
			return nil, true, fmt.Errorf("unsupported %q value type: %w", key, err)
		}
		return models, true, nil
	}
	return nil, false, nil
}

// lookupObjectValue 优先按原键读取，再按大小写与分隔符无关的规则兜底读取。
func lookupObjectValue(objectPayload map[string]any, key string) (any, bool) {
	if value, ok := objectPayload[key]; ok {
		return value, true
	}
	target := normalizeDiscoveryKey(key)
	for currentKey, value := range objectPayload {
		if normalizeDiscoveryKey(currentKey) == target {
			return value, true
		}
	}
	return nil, false
}

// normalizeDiscoveryKey 统一字段名比较规则：忽略大小写、下划线、连字符和空白。
func normalizeDiscoveryKey(key string) string {
	trimmed := strings.TrimSpace(strings.ToLower(key))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(trimmed)
}

// decodeRootArrayPayload 专门处理顶层数组 payload，避免把 map 根对象误判成单模型。
func decodeRootArrayPayload(raw any) ([]map[string]any, bool, error) {
	switch raw.(type) {
	case nil:
		return nil, true, nil
	case []any:
		models, err := decodeModelEntries(raw)
		return models, true, err
	default:
		return nil, false, nil
	}
}

// decodeModelEntries 处理列表、单对象和空值三种模型承载形式。
func decodeModelEntries(raw any) ([]map[string]any, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		models := make([]map[string]any, 0, len(value))
		for _, item := range value {
			switch typed := item.(type) {
			case map[string]any:
				models = append(models, typed)
			case string:
				modelID := strings.TrimSpace(typed)
				if modelID == "" {
					continue
				}
				models = append(models, map[string]any{
					"id":   modelID,
					"name": modelID,
				})
			}
		}
		return models, nil
	case map[string]any:
		return []map[string]any{value}, nil
	default:
		return nil, fmt.Errorf("unsupported models data type %T", raw)
	}
}

// modelListKeys 根据 profile 返回优先尝试的模型列表字段顺序。
func modelListKeys(profile string) []string {
	switch normalizeResponseProfile(profile) {
	case provider.DiscoveryResponseProfileGemini:
		return []string{"models", "data"}
	case provider.DiscoveryResponseProfileGeneric:
		return []string{"data", "models", "items", "results", "model_list", "modelList", "list"}
	default:
		return []string{"data", "models"}
	}
}

// extractRawModelsByTraversal 在未知容器键场景下做深度遍历兜底，防止第三方字段命名偏差导致 discovery 全量失效。
func extractRawModelsByTraversal(raw any, currentKey string, depth int) ([]map[string]any, bool, error) {
	if depth > 8 {
		return nil, false, nil
	}

	switch typed := raw.(type) {
	case map[string]any:
		if looksLikeSingleModelObject(typed) {
			return []map[string]any{typed}, true, nil
		}
		for key, value := range typed {
			models, found, err := extractRawModelsByTraversal(value, key, depth+1)
			if err != nil {
				return nil, true, err
			}
			if found {
				return models, true, nil
			}
		}
		return nil, false, nil
	case []any:
		models, err := decodeModelEntries(typed)
		if err != nil {
			return nil, true, err
		}
		if len(models) > 0 && looksLikeModelCollection(models) {
			// 对纯字符串数组做额外约束，避免把 errors/messages 误识别为模型列表。
			if !arrayContainsOnlyStrings(typed) || currentKey == "" || isLikelyModelListKey(currentKey) {
				return models, true, nil
			}
		}
		for _, item := range typed {
			models, found, nestedErr := extractRawModelsByTraversal(item, currentKey, depth+1)
			if nestedErr != nil {
				return nil, true, nestedErr
			}
			if found {
				return models, true, nil
			}
		}
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// looksLikeModelCollection 判断提取出的列表是否具有模型集合特征，降低兜底路径误命中概率。
func looksLikeModelCollection(models []map[string]any) bool {
	if len(models) == 0 {
		return false
	}

	validCount := 0
	for _, model := range models {
		if looksLikeSingleModelObject(model) {
			validCount++
		}
	}
	return validCount > 0 && validCount*2 >= len(models)
}

// arrayContainsOnlyStrings 判断数组是否全部由字符串元素构成。
func arrayContainsOnlyStrings(items []any) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

// isLikelyModelListKey 判断当前键名是否疑似模型列表容器键。
func isLikelyModelListKey(key string) bool {
	normalized := normalizeDiscoveryKey(key)
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "model") {
		return true
	}
	switch normalized {
	case "data", "items", "results", "list":
		return true
	default:
		return false
	}
}

// normalizeResponseProfile 统一 profile 写法并为未知值回退到 openai 解析序。
func normalizeResponseProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case provider.DiscoveryResponseProfileGemini:
		return provider.DiscoveryResponseProfileGemini
	case provider.DiscoveryResponseProfileGeneric:
		return provider.DiscoveryResponseProfileGeneric
	default:
		return provider.DiscoveryResponseProfileOpenAI
	}
}

// looksLikeSingleModelObject 判断 map 是否像一个模型对象，用于根对象兼容兜底。
func looksLikeSingleModelObject(raw map[string]any) bool {
	if raw == nil {
		return false
	}
	if id, ok := lookupObjectValue(raw, "id"); ok && strings.TrimSpace(fmt.Sprint(id)) != "" {
		return true
	}
	if model, ok := lookupObjectValue(raw, "model"); ok && strings.TrimSpace(fmt.Sprint(model)) != "" {
		return true
	}
	if modelID, ok := lookupObjectValue(raw, "model_id"); ok && strings.TrimSpace(fmt.Sprint(modelID)) != "" {
		return true
	}
	if modelID, ok := lookupObjectValue(raw, "modelId"); ok && strings.TrimSpace(fmt.Sprint(modelID)) != "" {
		return true
	}
	return false
}
