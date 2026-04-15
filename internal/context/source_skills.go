package context

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"neo-code/internal/skills"
)

const (
	maxSkillReferences = 3
	maxSkillToolHints  = 3
	maxSkillExamples   = 2
)

// skillPromptSource 负责将当前轮次激活的 skills 渲染为统一的 prompt section。
type skillPromptSource struct{}

// Sections 根据 BuildInput.ActiveSkills 生成结构化的 Skills prompt section。
func (skillPromptSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rendered := renderActiveSkillsSection(input.ActiveSkills)
	if strings.TrimSpace(rendered) == "" {
		return nil, nil
	}
	return []promptSection{NewPromptSection("Skills", rendered)}, nil
}

// renderActiveSkillsSection 负责去重、排序并渲染激活 skills 的结构化提示文本。
func renderActiveSkillsSection(activeSkills []skills.Skill) string {
	normalized := normalizeActiveSkills(activeSkills)
	if len(normalized) == 0 {
		return ""
	}

	parts := make([]string, 0, len(normalized))
	for _, skill := range normalized {
		rendered := renderOneSkill(skill)
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n")
}

// renderOneSkill 将单个 skill 渲染为固定结构，避免 provider 侧看到不稳定格式。
func renderOneSkill(skill skills.Skill) string {
	lines := []string{
		fmt.Sprintf("- skill: %s (%s)", strings.TrimSpace(skill.Descriptor.Name), strings.TrimSpace(skill.Descriptor.ID)),
	}

	instruction := strings.TrimSpace(skill.Content.Instruction)
	if instruction != "" {
		lines = append(lines, "  instruction: "+instruction)
	}

	toolHints := truncateSkillStrings(skill.Content.ToolHints, maxSkillToolHints)
	if len(toolHints) > 0 {
		lines = append(lines, "  tool_hints: "+strings.Join(toolHints, " | "))
	}

	references := truncateSkillReferences(skill.Content.References, maxSkillReferences)
	if len(references) > 0 {
		lines = append(lines, "  references: "+strings.Join(references, " | "))
	}

	examples := truncateSkillStrings(skill.Content.Examples, maxSkillExamples)
	if len(examples) > 0 {
		lines = append(lines, "  examples: "+strings.Join(examples, " | "))
	}

	return strings.Join(lines, "\n")
}

// normalizeActiveSkills 对激活 skills 按规范化 ID 去重并稳定排序。
func normalizeActiveSkills(activeSkills []skills.Skill) []skills.Skill {
	if len(activeSkills) == 0 {
		return nil
	}

	byID := make(map[string]skills.Skill, len(activeSkills))
	keys := make([]string, 0, len(activeSkills))
	for _, skill := range activeSkills {
		key := normalizeSkillID(skill.Descriptor.ID)
		if key == "" {
			continue
		}
		if _, ok := byID[key]; ok {
			continue
		}
		byID[key] = skill
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}

	sort.Strings(keys)
	normalized := make([]skills.Skill, 0, len(keys))
	for _, key := range keys {
		normalized = append(normalized, byID[key])
	}
	return normalized
}

// truncateSkillStrings 对 skill 文本列表做去空、去重并按上限裁剪。
func truncateSkillStrings(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}

	result := make([]string, 0, min(limit, len(values)))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// truncateSkillReferences 优先保留标题与摘要，并按固定上限裁剪引用条目。
func truncateSkillReferences(references []skills.Reference, limit int) []string {
	if len(references) == 0 || limit <= 0 {
		return nil
	}

	result := make([]string, 0, min(limit, len(references)))
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		title := strings.TrimSpace(reference.Title)
		summary := strings.TrimSpace(reference.Summary)
		path := strings.TrimSpace(reference.Path)

		var rendered string
		switch {
		case title != "" && summary != "":
			rendered = title + ": " + summary
		case title != "":
			rendered = title
		case summary != "":
			rendered = summary
		default:
			rendered = path
		}
		rendered = strings.TrimSpace(rendered)
		if rendered == "" {
			continue
		}
		if _, ok := seen[rendered]; ok {
			continue
		}
		seen[rendered] = struct{}{}
		result = append(result, rendered)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// normalizeSkillID 将 skill id 规范化为去重与排序使用的稳定键。
func normalizeSkillID(skillID string) string {
	normalized := strings.ToLower(strings.TrimSpace(skillID))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return strings.Trim(normalized, "-")
}

// min 返回两个整数中的较小值，供固定上限裁剪逻辑复用。
func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
