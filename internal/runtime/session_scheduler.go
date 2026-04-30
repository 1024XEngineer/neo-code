package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// loadOrCreateSession 负责在运行开始时解析工作目录并加载或创建会话。
func (s *Service) loadOrCreateSession(
	ctx context.Context,
	sessionID string,
	title string,
	defaultWorkdir string,
	requestedWorkdir string,
) (agentsession.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionWorkdir, err := resolveWorkdirForSession(defaultWorkdir, "", requestedWorkdir)
		if err != nil {
			return agentsession.Session{}, err
		}
		session := agentsession.NewWithWorkdir(title, sessionWorkdir)
		establishSessionVerificationProfile(&session)
		session, err = s.sessionStore.CreateSession(ctx, createSessionInputFromSession(session))
		if err != nil {
			return agentsession.Session{}, err
		}
		return session, nil
	}

	session, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err != nil {
		return agentsession.Session{}, err
	}
	profileChanged := establishSessionVerificationProfile(&session)
	restoreAlignChanged := false
	if sessionHasCompactedTranscript(session) {
		restoreAlignChanged = markCurrentPlanRestorePending(&session)
	}
	if strings.TrimSpace(requestedWorkdir) == "" && strings.TrimSpace(session.Workdir) != "" {
		if profileChanged || restoreAlignChanged {
			session.UpdatedAt = time.Now()
			if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session)); err != nil {
				return agentsession.Session{}, err
			}
		}
		return session, nil
	}

	resolved, err := resolveWorkdirForSession(defaultWorkdir, session.Workdir, requestedWorkdir)
	if err != nil {
		return agentsession.Session{}, err
	}
	workdirChanged := session.Workdir != resolved
	if !workdirChanged && !profileChanged && !restoreAlignChanged {
		return session, nil
	}
	if workdirChanged {
		session.Workdir = resolved
	}
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session)); err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

// sessionHasCompactedTranscript 判断会话是否已进入 compact 后的恢复上下文。
func sessionHasCompactedTranscript(session agentsession.Session) bool {
	if len(session.Messages) == 0 {
		return false
	}
	message := session.Messages[0]
	if message.Role != providertypes.RoleAssistant {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts)), "[compact_summary]")
}

// establishSessionVerificationProfile 在创建新会话的边界显式写入验收 profile，避免运行时依赖隐式零值。
func establishSessionVerificationProfile(session *agentsession.Session) bool {
	if session == nil {
		return false
	}
	if session.TaskState.VerificationProfile.Valid() {
		return false
	}
	session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
	return true
}

// startRun 记录当前激活的运行取消句柄，并分配一个新的运行令牌。
func (s *Service) startRun(cancel context.CancelFunc, runID ...string) uint64 {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.activeRunCancels == nil {
		s.activeRunCancels = make(map[uint64]context.CancelFunc)
	}
	if s.activeRunByID == nil {
		s.activeRunByID = make(map[string]uint64)
	}
	if s.activeRunTokenIDs == nil {
		s.activeRunTokenIDs = make(map[uint64]string)
	}

	s.nextRunToken++
	token := s.nextRunToken
	s.activeRunToken = token
	s.activeRunCancels[token] = cancel
	if len(runID) > 0 {
		normalizedRunID := strings.TrimSpace(runID[0])
		if normalizedRunID != "" {
			s.activeRunByID[normalizedRunID] = token
			s.activeRunTokenIDs[token] = normalizedRunID
		}
	}
	return token
}

// finishRun 在运行结束时释放指定运行的取消句柄，并回退到最新活跃运行。
func (s *Service) finishRun(token uint64) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	delete(s.activeRunCancels, token)
	if runID, exists := s.activeRunTokenIDs[token]; exists {
		delete(s.activeRunTokenIDs, token)
		if mappedToken, ok := s.activeRunByID[runID]; ok && mappedToken == token {
			delete(s.activeRunByID, runID)
		}
	}
	if s.activeRunToken != token {
		return
	}

	s.activeRunToken = 0
	for activeToken := range s.activeRunCancels {
		if activeToken > s.activeRunToken {
			s.activeRunToken = activeToken
		}
	}
}

// acquireSessionLock 获取指定会话写锁并返回释放引用的函数。
// 调用方在完成会话级串行操作后，必须调用 release 以允许锁条目回收。
func (s *Service) acquireSessionLock(sessionID string) (*sync.RWMutex, func()) {
	return s.acquireSessionLockEntry(sessionID)
}

// acquireSessionLockEntry 获取指定会话的锁条目并增加引用计数。
func (s *Service) acquireSessionLockEntry(sessionID string) (*sync.RWMutex, func()) {
	s.sessionMu.Lock()
	if s.sessionLocks == nil {
		s.sessionLocks = make(map[string]*sessionLockEntry)
	}

	entry, ok := s.sessionLocks[sessionID]
	if !ok {
		entry = &sessionLockEntry{}
		s.sessionLocks[sessionID] = entry
	}
	entry.refs++
	s.sessionMu.Unlock()

	released := false
	release := func() {
		s.sessionMu.Lock()
		defer s.sessionMu.Unlock()
		if released {
			return
		}
		released = true

		current, exists := s.sessionLocks[sessionID]
		if !exists || current != entry {
			return
		}
		current.refs--
		if current.refs <= 0 {
			delete(s.sessionLocks, sessionID)
		}
	}
	return &entry.mu, release
}

func resolveWorkdirForSession(defaultWorkdir string, currentWorkdir string, requestedWorkdir string) (string, error) {
	base := agentsession.EffectiveWorkdir(currentWorkdir, defaultWorkdir)
	if strings.TrimSpace(requestedWorkdir) == "" {
		return agentsession.ResolveExistingDir(base)
	}

	target := strings.TrimSpace(requestedWorkdir)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return agentsession.ResolveExistingDir(target)
}
