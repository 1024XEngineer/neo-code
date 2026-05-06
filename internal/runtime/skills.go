package runtime

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
)

var errSkillsRegistryUnavailable = errors.New("runtime: skills registry unavailable")

// SessionSkillState 描述一个会话中 skill 的解析结果与当前状态。
type SessionSkillState struct {
	SkillID    string
	Missing    bool
	Descriptor *skills.Descriptor
}

// AvailableSkillState 描述当前可见 skill 的元信息及其在会话中的激活状态。
type AvailableSkillState struct {
	Descriptor skills.Descriptor
	Active     bool
}

// ActivateSessionSkill 在 session 级激活一个已注册的 skill。
func (s *Service) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("runtime: session id is empty")
	}
	if s.skillsRegistry == nil {
		return errSkillsRegistryUnavailable
	}

	descriptor, _, err := s.skillsRegistry.Get(ctx, skillID)
	if err != nil {
		if errors.Is(err, skills.ErrSkillNotFound) {
			if refreshErr := s.skillsRegistry.Refresh(ctx); refreshErr != nil {
				return refreshErr
			}
			descriptor, _, err = s.skillsRegistry.Get(ctx, skillID)
		}
		if err != nil {
			return err
		}
	}

	session, changed, err := s.mutateSessionSkills(ctx, sessionID, func(current *agentsession.Session) bool {
		return current.ActivateSkill(descriptor.ID)
	})
	if err != nil {
		return err
	}
	if changed {
		_ = s.emit(ctx, EventSkillActivated, "", session.ID, SessionSkillEventPayload{SkillID: normalizeRuntimeSkillID(descriptor.ID)})
	}
	return nil
}

// DeactivateSessionSkill 在 session 级停用一个 skill，未知 skill 也保持幂等成功。
func (s *Service) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("runtime: session id is empty")
	}

	session, changed, err := s.mutateSessionSkills(ctx, sessionID, func(current *agentsession.Session) bool {
		return current.DeactivateSkill(skillID)
	})
	if err != nil {
		return err
	}
	if changed {
		_ = s.emit(ctx, EventSkillDeactivated, "", session.ID, SessionSkillEventPayload{SkillID: normalizeRuntimeSkillID(skillID)})
	}
	return nil
}

// ListSessionSkills 返回当前 session 激活 skills 的解析视图。
func (s *Service) ListSessionSkills(ctx context.Context, sessionID string) ([]SessionSkillState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("runtime: session id is empty")
	}

	session, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	ids := session.ActiveSkillIDs()
	if len(ids) == 0 {
		return nil, nil
	}

	states := make([]SessionSkillState, 0, len(ids))
	for _, skillID := range ids {
		state := SessionSkillState{SkillID: skillID}
		if s.skillsRegistry == nil {
			state.Missing = true
			states = append(states, state)
			continue
		}

		descriptor, _, err := s.skillsRegistry.Get(ctx, skillID)
		if err != nil {
			if errors.Is(err, skills.ErrSkillNotFound) {
				state.Missing = true
				states = append(states, state)
				continue
			}
			return nil, err
		}
		descriptorCopy := descriptor
		state.Descriptor = &descriptorCopy
		states = append(states, state)
	}
	return states, nil
}

// ListAvailableSkills 返回当前 registry 中对会话可见的技能列表，并标记激活状态。
func (s *Service) ListAvailableSkills(ctx context.Context, sessionID string) ([]AvailableSkillState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.skillsRegistry == nil {
		return nil, errSkillsRegistryUnavailable
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	workspace := ""
	activeSet := map[string]struct{}{}
	if normalizedSessionID != "" {
		session, err := s.sessionStore.LoadSession(ctx, normalizedSessionID)
		if err != nil {
			return nil, err
		}
		activeSet = skillSetFromIDs(session.ActiveSkillIDs())
		if s.configManager != nil {
			workspace = agentsession.EffectiveWorkdir(session.Workdir, s.configManager.Get().Workdir)
		} else {
			workspace = strings.TrimSpace(session.Workdir)
		}
	} else if s.configManager != nil {
		workspace = strings.TrimSpace(s.configManager.Get().Workdir)
	}

	descriptors, err := s.skillsRegistry.List(ctx, skills.ListInput{Workspace: workspace})
	if err != nil {
		return nil, err
	}
	if len(descriptors) == 0 {
		return nil, nil
	}

	states := make([]AvailableSkillState, 0, len(descriptors))
	for _, descriptor := range descriptors {
		key := normalizeRuntimeSkillID(descriptor.ID)
		_, active := activeSet[key]
		states = append(states, AvailableSkillState{
			Descriptor: descriptor,
			Active:     active,
		})
	}
	sort.Slice(states, func(i, j int) bool {
		return normalizeRuntimeSkillID(states[i].Descriptor.ID) < normalizeRuntimeSkillID(states[j].Descriptor.ID)
	})
	return states, nil
}

// resolveActiveSkills 解析当前 session 激活的 skills，并对缺失项做事件降级。
func (s *Service) resolveActiveSkills(ctx context.Context, state *runState) ([]skills.Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	activeSkillIDs := state.session.ActiveSkillIDs()
	if len(activeSkillIDs) == 0 {
		return nil, nil
	}
	if s.skillsRegistry == nil {
		for _, skillID := range activeSkillIDs {
			s.emitSkillMissingOnce(ctx, state, skillID)
		}
		return nil, nil
	}

	resolved := make([]skills.Skill, 0, len(activeSkillIDs))
	for _, skillID := range activeSkillIDs {
		descriptor, content, err := s.skillsRegistry.Get(ctx, skillID)
		if err != nil {
			if errors.Is(err, skills.ErrSkillNotFound) {
				s.emitSkillMissingOnce(ctx, state, skillID)
				continue
			}
			return nil, err
		}
		resolved = append(resolved, skills.Skill{
			Descriptor: descriptor,
			Content:    content,
		})
	}
	return resolved, nil
}

// prioritizeToolSpecsBySkillHints 按激活 skill 的 tool_hints 调整工具顺序，仅影响提示优先级。
func prioritizeToolSpecsBySkillHints(
	specs []providertypes.ToolSpec,
	activeSkills []skills.Skill,
) []providertypes.ToolSpec {
	if len(specs) == 0 {
		return nil
	}
	hints := collectSkillToolHints(activeSkills)
	if len(hints) == 0 {
		return append([]providertypes.ToolSpec(nil), specs...)
	}

	rank := make(map[string]int, len(hints))
	for idx, hint := range hints {
		rank[hint] = idx
	}
	prioritized := append([]providertypes.ToolSpec(nil), specs...)
	sort.SliceStable(prioritized, func(i, j int) bool {
		leftRank, leftHit := rank[normalizeRuntimeSkillID(prioritized[i].Name)]
		rightRank, rightHit := rank[normalizeRuntimeSkillID(prioritized[j].Name)]
		switch {
		case leftHit && rightHit:
			return leftRank < rightRank
		case leftHit:
			return true
		case rightHit:
			return false
		default:
			// 未命中的工具保持原有相对顺序，避免 hint 影响无关工具排序。
			return false
		}
	})
	return prioritized
}

// emitSkillMissingOnce 在同一次 run 内只上报一次指定 skill 的缺失事件，避免重复噪音。
func (s *Service) emitSkillMissingOnce(ctx context.Context, state *runState, skillID string) {
	if state == nil {
		s.emitRunScoped(ctx, EventSkillMissing, state, SessionSkillEventPayload{SkillID: skillID})
		return
	}
	if !state.markSkillMissingReported(skillID) {
		return
	}
	s.emitRunScoped(ctx, EventSkillMissing, state, SessionSkillEventPayload{SkillID: skillID})
}

// collectSkillToolHints 收集并规范化激活 skills 中的 tool_hints，用于工具排序提示。
func collectSkillToolHints(activeSkills []skills.Skill) []string {
	if len(activeSkills) == 0 {
		return nil
	}
	out := make([]string, 0, len(activeSkills))
	seen := make(map[string]struct{}, len(activeSkills))
	for _, skill := range activeSkills {
		for _, hint := range skill.Content.ToolHints {
			normalized := normalizeRuntimeSkillID(hint)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	return out
}

// skillSetFromIDs 将技能 ID 列表转换为规范化集合，便于快速判断激活状态。
func skillSetFromIDs(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return map[string]struct{}{}
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		normalized := normalizeRuntimeSkillID(id)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

// mutateSessionSkills 串行修改 session 的激活 skills，并在发生变化时立即持久化。
func (s *Service) mutateSessionSkills(
	ctx context.Context,
	sessionID string,
	mutate func(current *agentsession.Session) bool,
) (agentsession.Session, bool, error) {
	if mutate == nil {
		return agentsession.Session{}, false, errors.New("runtime: mutate function is nil")
	}

	sessionMu, releaseLockRef := s.acquireSessionLock(sessionID)
	sessionMu.Lock()
	defer func() {
		sessionMu.Unlock()
		releaseLockRef()
	}()

	session, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err != nil {
		return agentsession.Session{}, false, err
	}
	if !mutate(&session) {
		return session, false, nil
	}

	session.UpdatedAt = time.Now()
	if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session)); err != nil {
		return agentsession.Session{}, false, err
	}
	return session, true, nil
}

// normalizeRuntimeSkillID 统一 runtime 层事件与持久化使用的 skill id 规范化方式。
func normalizeRuntimeSkillID(skillID string) string {
	normalized := strings.ToLower(strings.TrimSpace(skillID))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return strings.Trim(normalized, "-")
}
