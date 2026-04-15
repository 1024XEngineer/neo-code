package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

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
		return err
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

	session, err := s.sessionStore.Load(ctx, sessionID)
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

// emitSkillMissingOnce 在同一次 run 内只上报一次指定 skill 的缺失事件，避免重复噪音。
func (s *Service) emitSkillMissingOnce(ctx context.Context, state *runState, skillID string) {
	if state == nil {
		_ = s.emitRunScoped(ctx, EventSkillMissing, state, SessionSkillEventPayload{SkillID: skillID})
		return
	}
	if !state.markSkillMissingReported(skillID) {
		return
	}
	_ = s.emitRunScoped(ctx, EventSkillMissing, state, SessionSkillEventPayload{SkillID: skillID})
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

	session, err := s.sessionStore.Load(ctx, sessionID)
	if err != nil {
		return agentsession.Session{}, false, err
	}
	if !mutate(&session) {
		return session, false, nil
	}

	session.UpdatedAt = time.Now()
	if err := s.sessionStore.Save(ctx, &session); err != nil {
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
