package provider

import (
	"strings"

	"neo-code/internal/config"
)

// DescriptorFromRawModel normalizes a raw provider model object into a ModelDescriptor.
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
		Capabilities:    boolMapValue(raw["capabilities"]),
	}
	return normalizeModelDescriptor(descriptor), true
}

func modelDescriptorsFromIDs(modelIDs []string) []ModelDescriptor {
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

func MergeModelDescriptors(sources ...[]ModelDescriptor) []ModelDescriptor {
	if len(sources) == 0 {
		return nil
	}

	merged := make([]ModelDescriptor, 0)
	indexByID := make(map[string]int)

	for _, source := range sources {
		for _, candidate := range source {
			normalized := normalizeModelDescriptor(candidate)
			key := normalizeModelID(normalized.ID)
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

func normalizeModelDescriptor(descriptor ModelDescriptor) ModelDescriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Name = strings.TrimSpace(descriptor.Name)
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	if descriptor.Name == "" {
		descriptor.Name = descriptor.ID
	}
	descriptor.Capabilities = cloneStringBoolMap(descriptor.Capabilities)
	return descriptor
}

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
	primary.Capabilities = mergeStringBoolMaps(primary.Capabilities, secondary.Capabilities)
	return normalizeModelDescriptor(primary)
}

func mergeStringBoolMaps(primary map[string]bool, secondary map[string]bool) map[string]bool {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}

	merged := cloneStringBoolMap(primary)
	if merged == nil {
		merged = make(map[string]bool, len(secondary))
	}
	for key, value := range secondary {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
}

func boolMapValue(value any) map[string]bool {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}

	normalized := make(map[string]bool, len(raw))
	for key, value := range raw {
		boolValue, ok := value.(bool)
		if !ok {
			continue
		}
		normalized[strings.TrimSpace(key)] = boolValue
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

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

func cloneStringBoolMap(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func normalizeModelID(id string) string {
	return config.NormalizeKey(id)
}
