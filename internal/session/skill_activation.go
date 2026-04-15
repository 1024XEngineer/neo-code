package session

import (
	"sort"
	"strings"
)

// SkillActivation 表示一个会话级激活的 skill 引用，仅持久化规范化后的 SkillID。
type SkillActivation struct {
	SkillID string `json:"skill_id"`
}

// ActivateSkill 将 skill 记录到当前会话，并返回本次调用是否新增了激活项。
func (s *Session) ActivateSkill(skillID string) bool {
	if s == nil {
		return false
	}

	normalized := normalizeSkillID(skillID)
	if normalized == "" {
		return false
	}
	for _, item := range s.ActivatedSkills {
		if item.SkillID == normalized {
			return false
		}
	}
	s.ActivatedSkills = append(s.ActivatedSkills, SkillActivation{SkillID: normalized})
	s.ActivatedSkills = normalizeSkillActivations(s.ActivatedSkills)
	return true
}

// DeactivateSkill 从当前会话移除一个 skill，并返回本次调用是否真的移除了记录。
func (s *Session) DeactivateSkill(skillID string) bool {
	if s == nil || len(s.ActivatedSkills) == 0 {
		return false
	}

	normalized := normalizeSkillID(skillID)
	if normalized == "" {
		return false
	}

	filtered := make([]SkillActivation, 0, len(s.ActivatedSkills))
	removed := false
	for _, item := range s.ActivatedSkills {
		if item.SkillID == normalized {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		return false
	}
	s.ActivatedSkills = normalizeSkillActivations(filtered)
	return true
}

// ActiveSkillIDs 返回当前会话中已激活 skill 的稳定、去重后的 ID 列表。
func (s Session) ActiveSkillIDs() []string {
	if len(s.ActivatedSkills) == 0 {
		return nil
	}

	normalized := normalizeSkillActivations(s.ActivatedSkills)
	ids := make([]string, 0, len(normalized))
	for _, item := range normalized {
		ids = append(ids, item.SkillID)
	}
	return ids
}

// Clone 返回 skill 激活记录的副本，避免调用方共享底层切片。
func (a SkillActivation) Clone() SkillActivation {
	return SkillActivation{SkillID: a.SkillID}
}

// normalizeSkillActivations 统一收敛 skill 激活列表的空白、重复项与排序。
func normalizeSkillActivations(items []SkillActivation) []SkillActivation {
	if len(items) == 0 {
		return nil
	}

	deduped := make([]SkillActivation, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := normalizeSkillID(item.SkillID)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		deduped = append(deduped, SkillActivation{SkillID: normalized})
	}
	if len(deduped) == 0 {
		return nil
	}

	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].SkillID < deduped[j].SkillID
	})
	return deduped
}

// cloneSkillActivations 深拷贝 skill 激活列表，供运行时与持久化快照复用。
func cloneSkillActivations(items []SkillActivation) []SkillActivation {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]SkillActivation, len(items))
	for idx, item := range items {
		cloned[idx] = item.Clone()
	}
	return cloned
}

// normalizeSkillID 将外部输入的 skill id 规范化为稳定的会话持久化键。
func normalizeSkillID(skillID string) string {
	normalized := strings.ToLower(strings.TrimSpace(skillID))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return strings.Trim(normalized, "-")
}
