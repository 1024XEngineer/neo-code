package gateway

import "fmt"

// ErrorCode 表示网关协议层稳定错误码。
type ErrorCode string

const (
	// ErrorCodeInvalidFrame 表示帧结构或帧类型非法。
	ErrorCodeInvalidFrame ErrorCode = "invalid_frame"
	// ErrorCodeInvalidAction 表示动作值非法。
	ErrorCodeInvalidAction ErrorCode = "invalid_action"
	// ErrorCodeInvalidMultimodalPayload 表示多模态输入负载非法。
	ErrorCodeInvalidMultimodalPayload ErrorCode = "invalid_multimodal_payload"
	// ErrorCodeMissingRequiredField 表示缺少必填字段。
	ErrorCodeMissingRequiredField ErrorCode = "missing_required_field"
	// ErrorCodeUnsupportedAction 表示动作暂不支持。
	ErrorCodeUnsupportedAction ErrorCode = "unsupported_action"
	// ErrorCodeInternalError 表示网关内部错误。
	ErrorCodeInternalError ErrorCode = "internal_error"
)

var stableErrorCodes = map[string]struct{}{
	string(ErrorCodeInvalidFrame):             {},
	string(ErrorCodeInvalidAction):            {},
	string(ErrorCodeInvalidMultimodalPayload): {},
	string(ErrorCodeMissingRequiredField):     {},
	string(ErrorCodeUnsupportedAction):        {},
	string(ErrorCodeInternalError):            {},
}

// String 返回错误码的字符串值。
func (c ErrorCode) String() string {
	return string(c)
}

// NewFrameError 创建统一格式的协议错误对象。
func NewFrameError(code ErrorCode, message string) *FrameError {
	return &FrameError{
		Code:    code.String(),
		Message: message,
	}
}

// NewMissingRequiredFieldError 创建缺少必填字段的错误对象。
func NewMissingRequiredFieldError(field string) *FrameError {
	return NewFrameError(ErrorCodeMissingRequiredField, fmt.Sprintf("missing required field: %s", field))
}

// IsStableErrorCode 判断给定字符串是否为网关稳定错误码。
func IsStableErrorCode(code string) bool {
	_, exists := stableErrorCodes[code]
	return exists
}
