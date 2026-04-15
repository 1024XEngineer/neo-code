package subagent

import "strings"

var supportedOutputSections = map[string]struct{}{
	"summary":      {},
	"findings":     {},
	"patches":      {},
	"risks":        {},
	"next_actions": {},
	"artifacts":    {},
}

// validateOutputContract 校验输出结构是否满足角色策略要求。
func validateOutputContract(policy RolePolicy, output Output) error {
	out := output.normalize()
	requiredSections, err := normalizeRequiredSections(policy.RequiredSections)
	if err != nil {
		return err
	}
	for _, key := range requiredSections {
		if !hasOutputSectionContent(out, key) {
			return errorsf("output section %q is required", key)
		}
	}
	return nil
}

// normalizeRequiredSections 归一化并校验 required section 名称集合。
func normalizeRequiredSections(sections []string) ([]string, error) {
	items := dedupeAndTrim(sections)
	keys := make([]string, 0, len(items))
	for _, section := range items {
		key := strings.ToLower(strings.TrimSpace(section))
		if _, ok := supportedOutputSections[key]; !ok {
			return nil, errorsf("unsupported required output section %q", section)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// hasOutputSectionContent 判断指定 section 在输出中是否包含有效内容。
func hasOutputSectionContent(output Output, section string) bool {
	switch section {
	case "summary":
		return strings.TrimSpace(output.Summary) != ""
	case "findings":
		return len(output.Findings) > 0
	case "patches":
		return len(output.Patches) > 0
	case "risks":
		return len(output.Risks) > 0
	case "next_actions":
		return len(output.NextActions) > 0
	case "artifacts":
		return len(output.Artifacts) > 0
	default:
		return false
	}
}
