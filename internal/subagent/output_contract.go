package subagent

import "strings"

var supportedOutputSections = map[string]struct{}{
	"summary":      {},
	"report":       {},
	"status":       {},
	"logs":         {},
	"findings":     {},
	"patches":      {},
	"risks":        {},
	"next_actions": {},
	"artifacts":    {},
}

// validateOutputContract 校验子代理输出是否满足统一结构化契约。
func validateOutputContract(policy RolePolicy, output Output) error {
	requiredSections, err := normalizeRequiredSections(policy.RequiredSections)
	if err != nil {
		return err
	}
	out := output.normalize()
	for _, section := range requiredSections {
		if !hasOutputSectionContent(out, section) {
			return errorsf("output section %q is required", section)
		}
	}
	return nil
}

// normalizeRequiredSections 规整并校验 required section 名称集合。
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
	case "report":
		return strings.TrimSpace(output.Report) != ""
	case "status":
		return strings.TrimSpace(output.Status) != ""
	case "logs":
		return len(output.Logs) > 0
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
