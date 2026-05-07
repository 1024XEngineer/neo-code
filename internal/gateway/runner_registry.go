package gateway

import (
	"log"
	"sync"
	"time"
)

// RunnerRecord 表示一个已注册的本地 runner。
type RunnerRecord struct {
	RunnerID        string
	RunnerName      string
	Workdir         string
	Labels          []string
	RegisteredAt    time.Time
	LastSeenAt      time.Time
	SessionBindings map[string]struct{} // session ID 集合
}

// RunnerRegistry 管理 runner 连接的生命周期与会话路由。
type RunnerRegistry struct {
	mu           sync.RWMutex
	runners      map[ConnectionID]*RunnerRecord // connectionID -> record
	sessionIndex map[string]ConnectionID        // sessionID -> connectionID
	logger       *log.Logger
}

// NewRunnerRegistry 创建 runner 注册中心。
func NewRunnerRegistry(logger *log.Logger) *RunnerRegistry {
	if logger == nil {
		logger = log.Default()
	}
	return &RunnerRegistry{
		runners:      make(map[ConnectionID]*RunnerRecord),
		sessionIndex: make(map[string]ConnectionID),
		logger:       logger,
	}
}

// Register 注册一个 runner 连接。
func (r *RunnerRegistry) Register(connectionID ConnectionID, runnerID string, runnerName string, workdir string, labels []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	r.runners[connectionID] = &RunnerRecord{
		RunnerID:        runnerID,
		RunnerName:      runnerName,
		Workdir:         workdir,
		Labels:          labels,
		RegisteredAt:    now,
		LastSeenAt:      now,
		SessionBindings: make(map[string]struct{}),
	}

	if r.logger != nil {
		r.logger.Printf("runner registered: runner_id=%s connection_id=%s", runnerID, connectionID)
	}
}

// Unregister 注销一个 runner 连接。
func (r *RunnerRegistry) Unregister(connectionID ConnectionID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runners[connectionID]
	if record == nil {
		return
	}

	// 清理 session 索引
	for sessionID := range record.SessionBindings {
		delete(r.sessionIndex, sessionID)
	}

	delete(r.runners, connectionID)

	if r.logger != nil {
		r.logger.Printf("runner unregistered: runner_id=%s connection_id=%s", record.RunnerID, connectionID)
	}
}

// BindSession 将会话绑定到指定 runner 连接。
func (r *RunnerRegistry) BindSession(sessionID string, connectionID ConnectionID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runners[connectionID]
	if record == nil {
		return false
	}

	record.SessionBindings[sessionID] = struct{}{}
	r.sessionIndex[sessionID] = connectionID
	return true
}

// UnbindSession 解除会话与 runner 的绑定。
func (r *RunnerRegistry) UnbindSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	connectionID, exists := r.sessionIndex[sessionID]
	if !exists {
		return
	}

	delete(r.sessionIndex, sessionID)

	record := r.runners[connectionID]
	if record != nil {
		delete(record.SessionBindings, sessionID)
	}
}

// LookupBySession 根据会话 ID 查找 runner 连接。
func (r *RunnerRegistry) LookupBySession(sessionID string) (ConnectionID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	connectionID, exists := r.sessionIndex[sessionID]
	return connectionID, exists
}

// IsOnline 判断 runner 是否在线。
func (r *RunnerRegistry) IsOnline(connectionID ConnectionID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.runners[connectionID]
	return exists
}

// Record 返回 runner 记录。
func (r *RunnerRegistry) Record(connectionID ConnectionID) (*RunnerRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record, exists := r.runners[connectionID]
	return record, exists
}

// List 返回所有在线 runner 记录。
func (r *RunnerRegistry) List() []RunnerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]RunnerRecord, 0, len(r.runners))
	for _, record := range r.runners {
		result = append(result, *record)
	}
	return result
}

// Heartbeat 刷新 runner 最后活跃时间。
func (r *RunnerRegistry) Heartbeat(connectionID ConnectionID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.runners[connectionID]
	if record != nil {
		record.LastSeenAt = time.Now()
	}
}

// OnConnectionDropped 在连接断开时清理 runner 记录。
func (r *RunnerRegistry) OnConnectionDropped(connectionID ConnectionID) {
	r.Unregister(connectionID)
}
