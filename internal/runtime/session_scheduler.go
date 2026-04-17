package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	if strings.TrimSpace(requestedWorkdir) == "" && strings.TrimSpace(session.Workdir) != "" {
		return session, nil
	}

	resolved, err := resolveWorkdirForSession(defaultWorkdir, session.Workdir, requestedWorkdir)
	if err != nil {
		return agentsession.Session{}, err
	}
	if session.Workdir == resolved {
		return session, nil
	}

	session.Workdir = resolved
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session)); err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

// startRun 记录当前激活的运行取消句柄，并分配一个新的运行令牌。
func (s *Service) startRun(cancel context.CancelFunc) uint64 {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.activeRunCancels == nil {
		s.activeRunCancels = make(map[uint64]context.CancelFunc)
	}

	s.nextRunToken++
	token := s.nextRunToken
	s.activeRunToken = token
	s.activeRunCancels[token] = cancel
	return token
}

// finishRun 在运行结束时释放指定运行的取消句柄，并回退到最新活跃运行。
func (s *Service) finishRun(token uint64) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	delete(s.activeRunCancels, token)
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

// acquireSessionLock 获取指定会话锁并返回释放引用的函数。
// 调用方在完成会话级串行操作后，必须调用 release 以允许锁条目回收。
func (s *Service) acquireSessionLock(sessionID string) (*sync.Mutex, func()) {
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
