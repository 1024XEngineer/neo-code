package runtime

import "strings"

// setPendingUserQuestion 在 runState 中记录当前待回答 ask_user 问题，用于快照恢复。
func (s *Service) setPendingUserQuestion(state *runState, payload UserQuestionRequestedPayload) {
	if s == nil || state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.pendingUserQuestion = clonePendingUserQuestion(&payload)
}

// clearPendingUserQuestionIfMatches 在问题被回答/跳过/超时后清理待回答快照。
func (s *Service) clearPendingUserQuestionIfMatches(state *runState, requestID string) {
	if s == nil || state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.pendingUserQuestion == nil {
		return
	}
	if strings.TrimSpace(requestID) == "" ||
		strings.EqualFold(strings.TrimSpace(state.pendingUserQuestion.RequestID), strings.TrimSpace(requestID)) {
		state.pendingUserQuestion = nil
	}
}

// parseAskUserRequestedPayload 把 ask_user 提问事件负载归一化为 runtime 强类型结构。
func parseAskUserRequestedPayload(payload any) (UserQuestionRequestedPayload, bool) {
	switch typed := payload.(type) {
	case UserQuestionRequestedPayload:
		return typed, true
	case *UserQuestionRequestedPayload:
		if typed == nil {
			return UserQuestionRequestedPayload{}, false
		}
		return *typed, true
	case map[string]any:
		result := UserQuestionRequestedPayload{
			RequestID:   trimAnyString(typed["request_id"]),
			QuestionID:  trimAnyString(typed["question_id"]),
			Title:       trimAnyString(typed["title"]),
			Description: trimAnyString(typed["description"]),
			Kind:        trimAnyString(typed["kind"]),
			Required:    toAnyBool(typed["required"]),
			AllowSkip:   toAnyBool(typed["allow_skip"]),
			MaxChoices:  toAnyInt(typed["max_choices"]),
			TimeoutSec:  toAnyInt(typed["timeout_sec"]),
		}
		if rawOptions, ok := typed["options"].([]any); ok {
			result.Options = append([]any(nil), rawOptions...)
		}
		if strings.TrimSpace(result.RequestID) == "" {
			return UserQuestionRequestedPayload{}, false
		}
		return result, true
	default:
		return UserQuestionRequestedPayload{}, false
	}
}

// parseAskUserResolvedPayload 把 ask_user 回答类事件负载归一化为 runtime 强类型结构。
func parseAskUserResolvedPayload(payload any) (UserQuestionResolvedPayload, bool) {
	switch typed := payload.(type) {
	case UserQuestionResolvedPayload:
		return typed, true
	case *UserQuestionResolvedPayload:
		if typed == nil {
			return UserQuestionResolvedPayload{}, false
		}
		return *typed, true
	case map[string]any:
		result := UserQuestionResolvedPayload{
			RequestID:  trimAnyString(typed["request_id"]),
			QuestionID: trimAnyString(typed["question_id"]),
			Status:     trimAnyString(typed["status"]),
			Message:    trimAnyString(typed["message"]),
		}
		if rawValues, ok := typed["values"].([]any); ok {
			values := make([]string, 0, len(rawValues))
			for _, value := range rawValues {
				trimmed := trimAnyString(value)
				if trimmed != "" {
					values = append(values, trimmed)
				}
			}
			result.Values = values
		}
		return result, true
	default:
		return UserQuestionResolvedPayload{}, false
	}
}

// trimAnyString 将任意输入转换为去首尾空白的字符串。
func trimAnyString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

// toAnyBool 将松散 JSON 布尔字段转换为 bool。
func toAnyBool(value any) bool {
	typed, _ := value.(bool)
	return typed
}

// toAnyInt 将松散 JSON 数值字段转换为 int。
func toAnyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
