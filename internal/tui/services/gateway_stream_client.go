package services

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

const runtimeEventPayloadVersion = 4

// GatewayStreamClient 负责消费 gateway.event 并恢复为 TUI 事件。
type GatewayStreamClient struct {
	source <-chan gatewayRPCNotification

	closeOnce sync.Once
	closeCh   chan struct{}
	done      chan struct{}
	events    chan RuntimeEvent
}

// NewGatewayStreamClient 创建并启动网关事件流消费者。
func NewGatewayStreamClient(source <-chan gatewayRPCNotification) *GatewayStreamClient {
	client := &GatewayStreamClient{
		source:  source,
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		events:  make(chan RuntimeEvent, 128),
	}
	go client.run()
	return client
}

// Events 返回恢复后的事件流。
func (c *GatewayStreamClient) Events() <-chan RuntimeEvent {
	return c.events
}

// Close 停止事件消费并释放资源。
func (c *GatewayStreamClient) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		<-c.done
	})
	return nil
}

// run 持续读取网关通知并向上游输出事件。
func (c *GatewayStreamClient) run() {
	defer close(c.done)
	defer close(c.events)

	for {
		select {
		case <-c.closeCh:
			return
		case notification, ok := <-c.source:
			if !ok {
				return
			}
			if !strings.EqualFold(strings.TrimSpace(notification.Method), protocol.MethodGatewayEvent) {
				continue
			}

			event, err := decodeRuntimeEventFromGatewayNotification(notification)
			if err != nil {
				errMessage := fmt.Sprintf("gateway stream decode error: %v", err)
				select {
				case <-c.closeCh:
					return
				case c.events <- RuntimeEvent{
					Type:      EventError,
					Timestamp: time.Now().UTC(),
					Payload:   errMessage,
				}:
				}
				if isRuntimePayloadVersionMismatch(errMessage) {
					return
				}
				continue
			}

			select {
			case <-c.closeCh:
				return
			case c.events <- event:
			}
		}
	}
}

// isRuntimePayloadVersionMismatch 判断错误是否由 runtime 事件版本不匹配触发，用于快速停止消费避免噪声洪泛。
func isRuntimePayloadVersionMismatch(errMessage string) bool {
	normalized := strings.ToLower(strings.TrimSpace(errMessage))
	return strings.Contains(normalized, "payload_version") &&
		strings.Contains(normalized, "unsupported")
}

// decodeRuntimeEventFromGatewayNotification 将 gateway.event 通知还原为事件。
func decodeRuntimeEventFromGatewayNotification(notification gatewayRPCNotification) (RuntimeEvent, error) {
	var frame gateway.MessageFrame
	if len(notification.Params) == 0 {
		return RuntimeEvent{}, fmt.Errorf("gateway.event params is empty")
	}
	if err := json.Unmarshal(notification.Params, &frame); err != nil {
		return RuntimeEvent{}, fmt.Errorf("decode gateway.event frame: %w", err)
	}

	envelope, ok := extractRuntimeEnvelope(frame.Payload)
	if !ok {
		return RuntimeEvent{}, fmt.Errorf("missing runtime event envelope")
	}

	eventType := EventType(strings.TrimSpace(streamReadMapString(envelope, "runtime_event_type")))
	if eventType == "" {
		return RuntimeEvent{}, fmt.Errorf("missing runtime_event_type")
	}

	event := RuntimeEvent{
		Type:           eventType,
		RunID:          strings.TrimSpace(frame.RunID),
		SessionID:      strings.TrimSpace(frame.SessionID),
		Turn:           streamReadMapInt(envelope, "turn"),
		Phase:          streamReadMapString(envelope, "phase"),
		Timestamp:      streamReadMapTime(envelope, "timestamp"),
		PayloadVersion: streamReadMapInt(envelope, "payload_version"),
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.PayloadVersion != runtimeEventPayloadVersion {
		return RuntimeEvent{}, fmt.Errorf(
			"unsupported runtime payload_version: got %d want %d",
			event.PayloadVersion,
			runtimeEventPayloadVersion,
		)
	}

	rawPayload, _ := streamReadMapValue(envelope, "payload")
	restoredPayload, err := restoreRuntimePayload(event.Type, rawPayload)
	if err != nil {
		return RuntimeEvent{}, err
	}
	event.Payload = restoredPayload
	return event, nil
}

// extractRuntimeEnvelope 从网关 payload 中提取事件包裹层。
func extractRuntimeEnvelope(payload any) (map[string]any, bool) {
	switch typed := payload.(type) {
	case map[string]any:
		if _, exists := streamReadMapValue(typed, "runtime_event_type"); exists {
			return typed, true
		}
		if nested, exists := streamReadMapValue(typed, "payload"); exists {
			if nestedMap, ok := nested.(map[string]any); ok {
				if _, hasEventType := streamReadMapValue(nestedMap, "runtime_event_type"); hasEventType {
					return nestedMap, true
				}
			}
		}
	case nil:
		return nil, false
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}

	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil, false
	}
	if _, exists := streamReadMapValue(asMap, "runtime_event_type"); exists {
		return asMap, true
	}
	if nested, exists := streamReadMapValue(asMap, "payload"); exists {
		if nestedMap, ok := nested.(map[string]any); ok {
			if _, hasEventType := streamReadMapValue(nestedMap, "runtime_event_type"); hasEventType {
				return nestedMap, true
			}
		}
	}
	return nil, false
}

// restoreRuntimePayload 按事件类型将 payload 恢复为 TUI 可消费的强类型结构。
func restoreRuntimePayload(eventType EventType, payload any) (any, error) {
	switch eventType {
	case EventUserMessage, EventAgentDone:
		return decodeRuntimePayload[providertypes.Message](payload)
	case EventToolStart:
		return decodeRuntimePayload[providertypes.ToolCall](payload)
	case EventToolResult:
		return decodeRuntimePayload[tools.ToolResult](payload)
	case EventPermissionRequested:
		return decodeRuntimePayload[PermissionRequestPayload](payload)
	case EventPermissionResolved:
		return decodeRuntimePayload[PermissionResolvedPayload](payload)
	case EventCompactApplied:
		return decodeRuntimePayload[CompactResult](payload)
	case EventCompactError:
		return decodeRuntimePayload[CompactErrorPayload](payload)
	case EventTokenUsage:
		return decodeRuntimePayload[TokenUsagePayload](payload)
	case EventPhaseChanged:
		return decodeRuntimePayload[PhaseChangedPayload](payload)
	case EventStopReasonDecided:
		return decodeStopReasonPayload(payload)
	case EventVerificationStarted:
		return decodeRuntimePayload[VerificationStartedPayload](payload)
	case EventVerificationStageFinished:
		return decodeRuntimePayload[VerificationStageFinishedPayload](payload)
	case EventVerificationFinished:
		return decodeRuntimePayload[VerificationFinishedPayload](payload)
	case EventVerificationCompleted:
		return decodeRuntimePayload[VerificationCompletedPayload](payload)
	case EventVerificationFailed:
		return decodeRuntimePayload[VerificationFailedPayload](payload)
	case EventAcceptanceDecided:
		return decodeRuntimePayload[AcceptanceDecidedPayload](payload)
	case EventInputNormalized:
		return decodeRuntimePayload[InputNormalizedPayload](payload)
	case EventAssetSaved:
		return decodeRuntimePayload[AssetSavedPayload](payload)
	case EventAssetSaveFailed:
		return decodeRuntimePayload[AssetSaveFailedPayload](payload)
	case EventHookStarted, EventHookFinished, EventHookFailed:
		return decodeRuntimePayload[HookEventPayload](payload)
	case EventHookBlocked:
		return decodeRuntimePayload[HookBlockedPayload](payload)
	case EventHookNotification:
		return decodeRuntimePayload[HookNotificationPayload](payload)
	case EventRepoHooksDiscovered, EventRepoHooksLoaded, EventRepoHooksSkippedUntrusted:
		return decodeRuntimePayload[RepoHooksLifecyclePayload](payload)
	case EventRepoHooksTrustStoreInvalid:
		return decodeRuntimePayload[RepoHooksTrustStoreInvalidPayload](payload)
	case EventTodoUpdated, EventTodoConflict:
		return decodeRuntimePayload[TodoEventPayload](payload)
	case EventTodoSnapshotUpdated:
		return decodeRuntimePayload[TodoEventPayload](payload)
	case EventSubAgentStarted, EventSubAgentProgress, EventSubAgentRetried, EventSubAgentBlocked, EventSubAgentCompleted, EventSubAgentFailed, EventSubAgentCanceled, EventSubAgentFinished:
		return decodeRuntimePayload[SubAgentEventPayload](payload)
	case EventSubAgentToolCallStarted, EventSubAgentToolCallResult, EventSubAgentToolCallDenied:
		return decodeRuntimePayload[SubAgentToolCallEventPayload](payload)
	case EventRuntimeSnapshotUpdated:
		return decodeRuntimePayload[RuntimeSnapshotUpdatedPayload](payload)
	case EventFactsUpdated:
		return decodeRuntimePayload[FactsUpdatedPayload](payload)
	case EventDecisionMade:
		return decodeRuntimePayload[DecisionMadePayload](payload)
	case EventSubAgentSnapshotUpdated:
		return decodeRuntimePayload[SubAgentSnapshotUpdatedPayload](payload)
	case EventType(RuntimeEventRunContext):
		return decodeRuntimePayload[RuntimeRunContextPayload](payload)
	case EventType(RuntimeEventToolStatus):
		return decodeRuntimePayload[RuntimeToolStatusPayload](payload)
	case EventType(RuntimeEventUsage):
		return decodeRuntimePayload[RuntimeUsagePayload](payload)
	case EventAgentChunk, EventToolChunk, EventError, EventToolCallThinking:
		return decodeStringPayload(payload), nil
	default:
		return payload, nil
	}
}

// decodeStopReasonPayload 约束 stop reason 枚举类型，避免字符串漂移。
func decodeStopReasonPayload(payload any) (StopReasonDecidedPayload, error) {
	decoded, err := decodeRuntimePayload[StopReasonDecidedPayload](payload)
	if err != nil {
		return StopReasonDecidedPayload{}, err
	}
	decoded.Reason = normalizeStopReasonCompatibility(decoded.Reason)
	return decoded, nil
}

// normalizeStopReasonCompatibility 统一兼容 runtime 新旧 stop reason 枚举值。
func normalizeStopReasonCompatibility(reason StopReason) StopReason {
	normalized := strings.ToLower(strings.TrimSpace(string(reason)))
	switch normalized {
	case "stop_completed":
		return StopReasonAccepted
	case "stop_user_interrupt":
		return StopReasonUserInterrupt
	case "stop_fatal_error":
		return StopReasonFatalError
	case "stop_max_turns_reached":
		return StopReasonMaxTurnExceeded
	case "stop_budget_exceeded":
		return StopReasonBudgetExceeded
	default:
		return StopReason(normalized)
	}
}

// decodeStringPayload 兼容字符串类事件 payload 解码。
func decodeStringPayload(payload any) string {
	switch typed := payload.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

// decodeRuntimePayload 使用 JSON 兜底做泛型反序列化，确保 map/struct 输入都可处理。
func decodeRuntimePayload[T any](payload any) (T, error) {
	var zero T
	switch typed := payload.(type) {
	case T:
		return typed, nil
	case *T:
		if typed == nil {
			return zero, fmt.Errorf("payload is nil")
		}
		return *typed, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("encode payload: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return zero, fmt.Errorf("payload is empty")
	}

	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return zero, fmt.Errorf("decode payload: %w", err)
	}
	return decoded, nil
}

// streamReadMapValue 提供对 snake/camel/大小写的兼容键读取。
func streamReadMapValue(m map[string]any, key string) (any, bool) {
	if len(m) == 0 {
		return nil, false
	}

	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return nil, false
	}

	if value, ok := m[trimmedKey]; ok {
		return value, true
	}
	if value, ok := m[strings.ToLower(trimmedKey)]; ok {
		return value, true
	}
	if value, ok := m[toSnakeCase(trimmedKey)]; ok {
		return value, true
	}
	if value, ok := m[toLowerCamelCase(trimmedKey)]; ok {
		return value, true
	}

	target := normalizeMapLookupKey(trimmedKey)
	for existingKey, value := range m {
		if normalizeMapLookupKey(existingKey) == target {
			return value, true
		}
	}
	return nil, false
}

// streamReadMapString 从动态 map 中读取字符串字段。
func streamReadMapString(m map[string]any, key string) string {
	value, ok := streamReadMapValue(m, key)
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

// streamReadMapInt 从动态 map 中读取整数，兼容 number/string。
func streamReadMapInt(m map[string]any, key string) int {
	value, ok := streamReadMapValue(m, key)
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return 0
		}
		return int(number)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		return number
	default:
		return 0
	}
}

// streamReadMapTime 从动态 map 中读取时间字段，支持 RFC3339Nano 字符串。
func streamReadMapTime(m map[string]any, key string) time.Time {
	value, ok := streamReadMapValue(m, key)
	if !ok || value == nil {
		return time.Time{}
	}
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return time.Time{}
		}
		parsed, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return time.Time{}
		}
		return parsed
	default:
		return time.Time{}
	}
}

// normalizeMapLookupKey 将键名归一化用于宽松匹配。
func normalizeMapLookupKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(key)))
}

// toSnakeCase 将字符串转为 snake_case，用于键名兼容读取。
func toSnakeCase(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	for index, r := range trimmed {
		if r >= 'A' && r <= 'Z' {
			if index > 0 {
				builder.WriteByte('_')
			}
			builder.WriteRune(r + ('a' - 'A'))
			continue
		}
		builder.WriteRune(r)
	}
	return strings.ToLower(builder.String())
}

// toLowerCamelCase 将首字母转小写，用于 lowerCamel 键名兼容。
func toLowerCamelCase(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) == 0 {
		return ""
	}
	if runes[0] >= 'A' && runes[0] <= 'Z' {
		runes[0] = runes[0] + ('a' - 'A')
	}
	return string(runes)
}
