package handlers

import (
	"fmt"
	"strings"

	"neo-code/internal/gateway/protocol"
)

const (
	// WakeErrorCodeInvalidAction 表示 wake 动作不在白名单内。
	WakeErrorCodeInvalidAction = "invalid_action"
	// WakeErrorCodeMissingRequiredField 表示 wake 请求缺少必要字段。
	WakeErrorCodeMissingRequiredField = "missing_required_field"
)

// WakeError 表示 wake handler 返回的结构化错误。
type WakeError struct {
	Code    string
	Message string
}

// Error 返回 wake 错误文本。
func (e *WakeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// WakeOpenURLResult 表示 wake.openUrl 最小处理结果。
type WakeOpenURLResult struct {
	Message string            `json:"message"`
	Action  string            `json:"action"`
	Params  map[string]string `json:"params,omitempty"`
}

// WakeOpenURLHandler 负责处理 wake.openUrl 的协议层校验。
type WakeOpenURLHandler struct{}

// NewWakeOpenURLHandler 创建 wake.openUrl 处理器实例。
func NewWakeOpenURLHandler() *WakeOpenURLHandler {
	return &WakeOpenURLHandler{}
}

// Handle 执行 wake.openUrl 的白名单与必填参数校验，并返回 ACK 负载。
func (h *WakeOpenURLHandler) Handle(intent protocol.WakeIntent) (WakeOpenURLResult, *WakeError) {
	_ = h

	action := strings.ToLower(strings.TrimSpace(intent.Action))
	if !protocol.IsSupportedWakeAction(action) {
		return WakeOpenURLResult{}, newWakeError(
			WakeErrorCodeInvalidAction,
			fmt.Sprintf("unsupported wake action: %s", intent.Action),
		)
	}

	switch action {
	case protocol.WakeActionReview:
		path := strings.TrimSpace(intent.Params["path"])
		if path == "" {
			return WakeOpenURLResult{}, newWakeError(
				WakeErrorCodeMissingRequiredField,
				"missing required field: params.path",
			)
		}
	}

	return WakeOpenURLResult{
		Message: "wake intent accepted",
		Action:  action,
		Params:  cloneParams(intent.Params),
	}, nil
}

// cloneParams 复制参数 map，避免调用方修改影响返回值。
func cloneParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(params))
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

// newWakeError 创建 wake handler 错误对象。
func newWakeError(code, message string) *WakeError {
	return &WakeError{
		Code:    code,
		Message: message,
	}
}
