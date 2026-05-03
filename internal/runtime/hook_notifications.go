package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	runtimehooks "neo-code/internal/runtime/hooks"
)

const (
	// MaxNotificationsPerRun 限制单次运行允许缓存的异步通知总数。
	MaxNotificationsPerRun = 64
	// MaxNotificationChars 限制单条异步通知的最大字符数，避免提示膨胀。
	MaxNotificationChars = 320
	// MaxInjectedNotificationsPerTurn 限制单轮注入到 provider request 的通知条数。
	MaxInjectedNotificationsPerTurn = 3
	// NotificationTTL 定义异步通知在队列中的生存时间。
	NotificationTTL = 5 * time.Minute
)

type queuedHookNotification struct {
	Payload   HookNotificationPayload
	CreatedAt time.Time
}

type hookAsyncResultSink struct {
	service *Service
}

func newHookAsyncResultSink(service *Service) *hookAsyncResultSink {
	return &hookAsyncResultSink{service: service}
}

// HandleAsyncHookResult 接收 async_rewake 结果并尝试写入当前运行的内存通知队列。
func (s *hookAsyncResultSink) HandleAsyncHookResult(
	ctx context.Context,
	spec runtimehooks.HookSpec,
	input runtimehooks.HookContext,
	result runtimehooks.HookResult,
) {
	if s == nil || s.service == nil {
		return
	}
	if spec.Mode != runtimehooks.HookModeAsyncRewake {
		return
	}
	if !shouldQueueAsyncRewake(result) {
		return
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return
	}
	token := readHookRunToken(input.Metadata)
	payload := HookNotificationPayload{
		HookID:    strings.TrimSpace(spec.ID),
		Source:    strings.TrimSpace(string(spec.Source)),
		Point:     strings.TrimSpace(string(spec.Point)),
		Status:    strings.TrimSpace(string(result.Status)),
		Reason:    strings.TrimSpace(result.Metadata.RewakeReason),
		Summary:   strings.TrimSpace(result.Metadata.RewakeSummary),
		Message:   strings.TrimSpace(result.Message),
		DedupeKey: strings.TrimSpace(buildRuntimeHookNotificationDedupeKey(spec, result)),
	}
	if payload.Summary == "" {
		payload.Summary = payload.Message
	}
	if payload.Reason == "" {
		payload.Reason = strings.TrimSpace(result.Error)
	}
	s.service.enqueueHookNotification(ctx, runID, token, payload)
}

func shouldQueueAsyncRewake(result runtimehooks.HookResult) bool {
	if result.Status == runtimehooks.HookResultFailed || result.Status == runtimehooks.HookResultBlock {
		return true
	}
	return result.Metadata.Rewake
}

func (s *Service) enqueueHookNotification(ctx context.Context, runID string, token uint64, payload HookNotificationPayload) {
	if s == nil {
		return
	}
	state := s.resolveActiveRunState(runID, token)
	if state == nil {
		return
	}
	now := time.Now()
	state.mu.Lock()
	defer state.mu.Unlock()
	purgeExpiredHookNotificationsLocked(state, now)
	dedupeKey := strings.TrimSpace(payload.DedupeKey)
	if dedupeKey == "" {
		dedupeKey = strings.TrimSpace(fmt.Sprintf(
			"%s|%s|%s|%s|%s|%s",
			payload.Source,
			payload.HookID,
			payload.Point,
			payload.Status,
			payload.Reason,
			payload.Summary,
		))
	}
	payload.DedupeKey = strings.ToLower(dedupeKey)
	if seenAt, exists := state.hookNotificationSeen[payload.DedupeKey]; exists && now.Sub(seenAt) < NotificationTTL {
		return
	}
	state.hookNotificationSeen[payload.DedupeKey] = now
	payload.Reason = truncateNotificationText(strings.TrimSpace(payload.Reason), MaxNotificationChars)
	payload.Summary = truncateNotificationText(strings.TrimSpace(payload.Summary), MaxNotificationChars)
	payload.Message = truncateNotificationText(strings.TrimSpace(payload.Message), MaxNotificationChars)
	if len(state.hookNotifications) >= MaxNotificationsPerRun {
		state.hookNotificationOmitted++
		return
	}
	state.hookNotifications = append(state.hookNotifications, queuedHookNotification{
		Payload:   payload,
		CreatedAt: now,
	})
	s.emitRunScopedOptional(EventHookNotification, state, payload)
}

func (s *Service) resolveActiveRunState(runID string, token uint64) *runState {
	if s == nil {
		return nil
	}
	normalizedRunID := strings.TrimSpace(runID)
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if token != 0 {
		if _, exists := s.activeRunCancels[token]; !exists {
			return nil
		}
		state := s.activeRunStates[token]
		if state == nil {
			return nil
		}
		if normalizedRunID != "" {
			if mapped, exists := s.activeRunByID[normalizedRunID]; !exists || mapped != token {
				return nil
			}
		}
		return state
	}
	if normalizedRunID == "" {
		return nil
	}
	mapped, exists := s.activeRunByID[normalizedRunID]
	if !exists {
		return nil
	}
	if _, exists := s.activeRunCancels[mapped]; !exists {
		return nil
	}
	return s.activeRunStates[mapped]
}

func readHookRunToken(metadata map[string]any) uint64 {
	if len(metadata) == 0 {
		return 0
	}
	raw, exists := metadata["runtime_run_token"]
	if !exists || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case uint64:
		return typed
	case int:
		if typed > 0 {
			return uint64(typed)
		}
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func purgeExpiredHookNotificationsLocked(state *runState, now time.Time) {
	if state == nil {
		return
	}
	if len(state.hookNotifications) > 0 {
		filtered := state.hookNotifications[:0]
		for _, entry := range state.hookNotifications {
			if now.Sub(entry.CreatedAt) > NotificationTTL {
				continue
			}
			filtered = append(filtered, entry)
		}
		state.hookNotifications = filtered
	}
	for key, seenAt := range state.hookNotificationSeen {
		if now.Sub(seenAt) > NotificationTTL {
			delete(state.hookNotificationSeen, key)
		}
	}
}

func truncateNotificationText(text string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxChars {
		return string(runes)
	}
	return string(runes[:maxChars])
}

func buildRuntimeHookNotificationDedupeKey(spec runtimehooks.HookSpec, result runtimehooks.HookResult) string {
	message := strings.TrimSpace(result.Message)
	if len([]rune(message)) > 128 {
		message = string([]rune(message)[:128])
	}
	return strings.ToLower(strings.Join([]string{
		strings.TrimSpace(string(spec.Source)),
		strings.TrimSpace(spec.ID),
		strings.TrimSpace(string(spec.Point)),
		strings.TrimSpace(string(result.Status)),
		strings.TrimSpace(result.Metadata.RewakeReason),
		strings.TrimSpace(result.Metadata.RewakeSummary),
		message,
	}, "|"))
}

func (s *Service) drainHookNotificationsForTurn(state *runState) string {
	if s == nil || state == nil {
		return ""
	}
	now := time.Now()
	state.mu.Lock()
	purgeExpiredHookNotificationsLocked(state, now)
	if len(state.hookNotifications) == 0 && state.hookNotificationOmitted == 0 {
		state.mu.Unlock()
		return ""
	}
	entries := append([]queuedHookNotification(nil), state.hookNotifications...)
	omitted := state.hookNotificationOmitted
	state.hookNotifications = nil
	state.hookNotificationOmitted = 0
	state.mu.Unlock()

	lines := make([]string, 0, MaxInjectedNotificationsPerTurn+1)
	injected := 0
	for _, entry := range entries {
		if injected >= MaxInjectedNotificationsPerTurn {
			omitted++
			continue
		}
		payload := entry.Payload
		summary := firstNonBlank(
			strings.TrimSpace(payload.Summary),
			strings.TrimSpace(payload.Message),
			strings.TrimSpace(payload.Reason),
			"notification received",
		)
		lines = append(lines, fmt.Sprintf(
			"- hook=%s point=%s status=%s summary=%s",
			firstNonBlank(strings.TrimSpace(payload.HookID), "unknown"),
			firstNonBlank(strings.TrimSpace(payload.Point), "unknown"),
			firstNonBlank(strings.TrimSpace(payload.Status), "unknown"),
			summary,
		))
		injected++
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("- %d more notifications omitted", omitted))
	}
	if len(lines) == 0 {
		return ""
	}
	return "[runtime_async_notifications]\n" +
		"These are ephemeral runtime notifications from async hooks. Use them as guidance only.\n" +
		strings.Join(lines, "\n")
}

// mergeEphemeralHookNotificationIntoSystemPrompt 将异步通知提示临时拼接到本轮系统提示词中。
// 该函数只影响当前 provider 请求，不写入会话历史或持久化存储。
func mergeEphemeralHookNotificationIntoSystemPrompt(basePrompt string, hint string) string {
	trimmedHint := strings.TrimSpace(hint)
	if trimmedHint == "" {
		return strings.TrimSpace(basePrompt)
	}
	trimmedBase := strings.TrimSpace(basePrompt)
	if trimmedBase == "" {
		return trimmedHint
	}
	return trimmedBase + "\n\n" + trimmedHint
}
