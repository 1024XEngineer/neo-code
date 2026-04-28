package handlers

import (
	"fmt"
	"path/filepath"
	"strings"

	"neo-code/internal/gateway/protocol"
)

const (
	// WakeErrorCodeInvalidAction 表示 wake 动作不在白名单内。
	WakeErrorCodeInvalidAction = "invalid_action"
	// WakeErrorCodeMissingRequiredField 表示 wake 请求缺少必要字段。
	WakeErrorCodeMissingRequiredField = "missing_required_field"
	// WakeErrorCodeUnsafePath 表示输入路径存在越界风险或不符合安全约束。
	WakeErrorCodeUnsafePath = "unsafe_path"
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
	sessionID := strings.TrimSpace(intent.SessionID)
	isResumeOnly := sessionID != ""
	if !protocol.IsSupportedWakeAction(action) {
		return WakeOpenURLResult{}, newWakeError(
			WakeErrorCodeInvalidAction,
			fmt.Sprintf("unsupported wake action: %s", intent.Action),
		)
	}

	switch action {
	case protocol.WakeActionReview:
		if !isResumeOnly {
			path := strings.TrimSpace(intent.Params["path"])
			if path == "" {
				return WakeOpenURLResult{}, newWakeError(
					WakeErrorCodeMissingRequiredField,
					"missing required field: params.path",
				)
			}
			if !isSafeReviewPath(path) {
				return WakeOpenURLResult{}, newWakeError(
					WakeErrorCodeUnsafePath,
					"unsafe review path",
				)
			}
			if strings.TrimSpace(intent.Workdir) == "" {
				return WakeOpenURLResult{}, newWakeError(
					WakeErrorCodeMissingRequiredField,
					"missing required field: workdir or session_id for review",
				)
			}
		}
	case protocol.WakeActionRun:
		if !isResumeOnly {
			prompt := strings.TrimSpace(intent.Params["prompt"])
			if prompt == "" {
				return WakeOpenURLResult{}, newWakeError(
					WakeErrorCodeMissingRequiredField,
					"missing required field: params.prompt",
				)
			}
		}
	}

	return WakeOpenURLResult{
		Message: "wake intent accepted",
		Action:  action,
		Params:  cloneParams(intent.Params),
	}, nil
}

// isSafeReviewPath 校验 review 请求路径，要求必须为相对路径且不允许出现目录回退段。
func isSafeReviewPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	if hasWindowsDriveLetterPrefix(trimmed) {
		return false
	}
	if filepath.VolumeName(trimmed) != "" {
		return false
	}
	if hasBlockedWindowsPathPrefix(trimmed) {
		return false
	}
	if isAbsoluteReviewPath(trimmed) {
		return false
	}
	if containsParentTraversalSegment(trimmed) {
		return false
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == "" {
		return false
	}
	if containsParentTraversalSegment(cleaned) {
		return false
	}
	return true
}

// hasWindowsDriveLetterPrefix 检查是否为 Windows 盘符前缀路径，如 C:foo。
func hasWindowsDriveLetterPrefix(path string) bool {
	trimmed := strings.TrimSpace(path)
	if len(trimmed) < 2 {
		return false
	}
	drive := trimmed[0]
	return ((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) && trimmed[1] == ':'
}

// hasBlockedWindowsPathPrefix 检查是否命中 Windows 设备路径前缀，避免绕过常规路径校验。
func hasBlockedWindowsPathPrefix(path string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "/", "\\")
	return strings.HasPrefix(normalized, `\\?\`) || strings.HasPrefix(normalized, `\\.\`)
}

// isAbsoluteReviewPath 统一识别跨平台绝对路径，避免在 Windows 下漏判 Unix 风格前导斜杠。
func isAbsoluteReviewPath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	normalized := normalizePath(path)
	return strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "\\")
}

// containsParentTraversalSegment 按路径段语义识别目录回退段，避免子串匹配导致误伤。
func containsParentTraversalSegment(path string) bool {
	normalized := normalizePath(path)
	normalized = filepath.ToSlash(normalized)
	segments := strings.Split(normalized, "/")
	for _, segment := range segments {
		if segment == ".." {
			return true
		}
	}
	return false
}

// normalizePath 将路径转换为统一的正斜杠表示，便于后续分段检查。
func normalizePath(path string) string {
	return filepath.ToSlash(strings.ReplaceAll(path, "\\", "/"))
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
