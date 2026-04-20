package memo

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const (
	autoExtractDebounce = 2 * time.Second
	autoExtractIdleTTL  = 5 * time.Minute
)

// AutoExtractor 负责按会话在后台调度自动提取，并处理防抖、互斥和尾随执行。
type AutoExtractor struct {
	extractor      Extractor
	svc            *Service
	debounce       time.Duration
	idleTTL        time.Duration
	extractTimeout time.Duration
	logf           func(format string, args ...any)

	mu     sync.Mutex
	states map[string]*autoExtractState
}

type autoExtractState struct {
	mu              sync.Mutex
	pending         *autoExtractRequest
	running         bool
	timer           *time.Timer
	idleTimer       *time.Timer
	scheduleSeq     uint64
	idleSeq         uint64
	lastFingerprint uint64
}

type autoExtractRequest struct {
	messages  []providertypes.Message
	dueAt     time.Time
	extractor Extractor
}

// NewAutoExtractor 创建后台自动提取调度器。
func NewAutoExtractor(extractor Extractor, svc *Service, extractTimeout time.Duration) *AutoExtractor {
	if extractTimeout <= 0 {
		extractTimeout = 15 * time.Second
	}
	return &AutoExtractor{
		extractor:      extractor,
		svc:            svc,
		debounce:       autoExtractDebounce,
		idleTTL:        autoExtractIdleTTL,
		extractTimeout: extractTimeout,
		logf:           log.Printf,
		states:         make(map[string]*autoExtractState),
	}
}

// Schedule 按会话维度安排一次后台自动提取。
func (a *AutoExtractor) Schedule(sessionID string, messages []providertypes.Message) {
	a.ScheduleWithExtractor(sessionID, messages, a.extractor)
}

// ScheduleWithExtractor 允许调用方在调度时绑定本次请求专用的提取器快照。
func (a *AutoExtractor) ScheduleWithExtractor(sessionID string, messages []providertypes.Message, extractor Extractor) {
	if a == nil || extractor == nil || a.svc == nil {
		return
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	req := autoExtractRequest{
		messages:  cloneProviderMessages(messages),
		dueAt:     time.Now().Add(a.debounce),
		extractor: extractor,
	}

	state := a.ensureState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()

	stopTimer(state.idleTimer)
	state.idleTimer = nil
	reqCopy := req
	state.pending = &reqCopy
	state.scheduleSeq++
	if state.running {
		return
	}

	a.armDebounceTimerLocked(sessionID, state, state.scheduleSeq, req.dueAt)
}

// ensureState 获取或创建会话级别的调度状态。
func (a *AutoExtractor) ensureState(sessionID string) *autoExtractState {
	a.mu.Lock()
	defer a.mu.Unlock()

	if state, ok := a.states[sessionID]; ok {
		return state
	}

	state := &autoExtractState{}
	a.states[sessionID] = state
	return state
}

// armDebounceTimerLocked 在持有状态锁时重置会话的防抖定时器。
func (a *AutoExtractor) armDebounceTimerLocked(
	sessionID string,
	state *autoExtractState,
	seq uint64,
	dueAt time.Time,
) {
	stopTimer(state.timer)
	wait := time.Until(dueAt)
	if wait < 0 {
		wait = 0
	}
	state.timer = time.AfterFunc(wait, func() {
		a.handleDebounce(sessionID, state, seq)
	})
}

// handleDebounce 在防抖窗口结束后启动一次后台提取，若消息未变化则跳过以避免重复 LLM 调用。
func (a *AutoExtractor) handleDebounce(sessionID string, state *autoExtractState, seq uint64) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.scheduleSeq != seq || state.running || state.pending == nil {
		return
	}
	if wait := time.Until(state.pending.dueAt); wait > 0 {
		a.armDebounceTimerLocked(sessionID, state, seq, state.pending.dueAt)
		return
	}

	req := *state.pending
	state.pending = nil

	// 增量检测：消息未变化时跳过提取
	fp := computeMessageFingerprint(req.messages)
	if fp == state.lastFingerprint {
		a.armIdleTimerLocked(sessionID, state)
		return
	}

	state.running = true
	state.timer = nil

	go func() {
		if a.extractAndStore(req.extractor, req.messages) {
			state.mu.Lock()
			state.lastFingerprint = fp
			state.mu.Unlock()
		}
		a.handleRunDone(sessionID, state)
	}()
}

// handleRunDone 在后台提取结束后决定是否执行尾随提取，或安排空闲回收。
func (a *AutoExtractor) handleRunDone(sessionID string, state *autoExtractState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	state.running = false
	if state.pending != nil {
		a.armDebounceTimerLocked(sessionID, state, state.scheduleSeq, state.pending.dueAt)
		return
	}

	a.armIdleTimerLocked(sessionID, state)
}

// armIdleTimerLocked 在会话空闲时安排状态回收，避免 map 与 goroutine 长期累积。
func (a *AutoExtractor) armIdleTimerLocked(sessionID string, state *autoExtractState) {
	stopTimer(state.idleTimer)
	state.idleSeq++
	seq := state.idleSeq
	state.idleTimer = time.AfterFunc(a.idleTTL, func() {
		a.handleIdle(sessionID, state, seq)
	})
}

// handleIdle 在会话空闲超时后回收状态。
func (a *AutoExtractor) handleIdle(sessionID string, state *autoExtractState, seq uint64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !isIdleStateLocked(state, seq) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// 在删除前再次确认 map 中仍指向当前状态，防止旧回调回收新状态。
	if a.states[sessionID] != state {
		return
	}
	if !isIdleStateLocked(state, seq) {
		return
	}
	state.idleTimer = nil
	delete(a.states, sessionID)
}

// isIdleStateLocked 判断状态在持锁条件下是否仍满足可回收的空闲条件。
func isIdleStateLocked(state *autoExtractState, seq uint64) bool {
	return state.idleSeq == seq && !state.running && state.pending == nil
}

// extractAndStore 执行提取，并在写入前做本地批次去重和持久化级别的原子去重。
// 返回值表示本次提取和写入流程是否成功完成，可用于更新增量指纹。
func (a *AutoExtractor) extractAndStore(extractor Extractor, messages []providertypes.Message) bool {
	ctx, cancel := context.WithTimeout(context.Background(), a.extractTimeout)
	defer cancel()

	entries, err := extractor.Extract(ctx, messages)
	if err != nil {
		if errors.Is(err, ErrExtractionNoJSONArray) || errors.Is(err, ErrExtractionIncompleteJSONArray) {
			a.logError("memo: auto extract skipped (protocol_mismatch): %v", err)
			return true
		}
		a.logError("memo: auto extract failed: %v", err)
		return false
	}
	if len(entries) == 0 {
		return true
	}

	seen := make(map[string]struct{}, len(entries))
	succeeded := true
	for _, entry := range entries {
		entry.Source = SourceAutoExtract
		key := autoExtractDedupKey(entry)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}

		added, err := a.svc.addAutoExtractIfAbsent(ctx, entry)
		if err != nil {
			a.logError("memo: auto extract add failed: %v", err)
			succeeded = false
			continue
		}

		seen[key] = struct{}{}
		if !added {
			continue
		}
	}
	return succeeded
}

// autoExtractDedupKey 生成自动提取条目的精确去重键。
func autoExtractDedupKey(entry Entry) string {
	title := NormalizeTitle(entry.Title)
	content := strings.TrimSpace(entry.Content)
	if !IsValidType(entry.Type) || title == "" || content == "" {
		return ""
	}
	return strings.Join([]string{string(entry.Type), title, content}, "\x1f")
}

// parseTopicSourceAndContent 从 topic 文件中解析 source frontmatter 和正文内容。
func parseTopicSourceAndContent(topic string) (string, string) {
	parts := strings.Split(topic, "---")
	if len(parts) < 3 {
		return "", strings.TrimSpace(topic)
	}

	frontmatter := parts[1]
	body := strings.TrimSpace(strings.Join(parts[2:], "---"))
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "source:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "source:")), body
	}
	return "", body
}

// cloneProviderMessages 深拷贝消息切片，避免后台任务读取到后续修改。
func cloneProviderMessages(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]providertypes.Message, 0, len(messages))
	for _, message := range messages {
		cloned = append(cloned, cloneProviderMessage(message))
	}
	return cloned
}

// stopTimer 停止定时器并在必要时清空通道。
func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// logError 统一收敛后台提取日志，避免把错误暴露给主链路。
func (a *AutoExtractor) logError(format string, args ...any) {
	if a != nil && a.logf != nil {
		a.logf(format, args...)
	}
}

// computeMessageFingerprint 使用 FNV-1a 64bit 哈希计算消息窗口的内容指纹，用于增量提取检测。
func computeMessageFingerprint(messages []providertypes.Message) uint64 {
	h := fnv.New64a()
	payload, err := json.Marshal(messages)
	if err == nil {
		_, _ = h.Write(payload)
		return h.Sum64()
	}

	// 回退路径仅在意外序列化失败时使用，至少保留文本消息的增量检测能力。
	for _, msg := range messages {
		_, _ = h.Write([]byte(msg.Role))
		_, _ = h.Write([]byte{0})
		for _, part := range msg.Parts {
			_, _ = h.Write([]byte(part.Kind))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte(part.Text))
			_, _ = h.Write([]byte{0})
		}
	}
	return h.Sum64()
}
