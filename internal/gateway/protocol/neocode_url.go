package protocol

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// ParseErrorCodeInvalidURL 表示 URL 文本不合法。
	ParseErrorCodeInvalidURL = "invalid_url"
	// ParseErrorCodeInvalidScheme 表示 URL scheme 非 neocode。
	ParseErrorCodeInvalidScheme = "invalid_scheme"
	// ParseErrorCodeMissingRequiredField 表示缺少必须字段。
	ParseErrorCodeMissingRequiredField = "missing_required_field"
)

const (
	// WakeActionReview 表示 review 唤醒动作。
	WakeActionReview = "review"
)

var supportedWakeActionSet = map[string]struct{}{
	WakeActionReview: {},
}

// WakeIntent 表示从 neocode:// URL 解析得到的标准化唤醒意图。
type WakeIntent struct {
	Action    string            `json:"action"`
	SessionID string            `json:"session_id,omitempty"`
	Workdir   string            `json:"workdir,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
	RawURL    string            `json:"raw_url"`
}

// ParseError 表示 URL 解析阶段可结构化消费的错误。
type ParseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error 返回解析错误的文本描述。
func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ParseNeoCodeURL 将原始 neocode:// URL 解析为标准化唤醒意图。
func ParseNeoCodeURL(raw string) (WakeIntent, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return WakeIntent{}, newParseError(ParseErrorCodeMissingRequiredField, "missing required field: url")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return WakeIntent{}, newParseError(ParseErrorCodeInvalidURL, fmt.Sprintf("invalid url: %v", err))
	}

	if !strings.EqualFold(parsed.Scheme, "neocode") {
		return WakeIntent{}, newParseError(ParseErrorCodeInvalidScheme, fmt.Sprintf("invalid url scheme: %s", parsed.Scheme))
	}

	action := resolveAction(parsed)
	if action == "" {
		return WakeIntent{}, newParseError(ParseErrorCodeMissingRequiredField, "missing required field: action")
	}

	params := flattenQueryValues(parsed.Query())
	sessionID := popQueryParam(params, "session_id", "session")
	workdir := popQueryParam(params, "workdir")
	if len(params) == 0 {
		params = nil
	}

	return WakeIntent{
		Action:    strings.ToLower(action),
		SessionID: sessionID,
		Workdir:   workdir,
		Params:    params,
		RawURL:    parsed.String(),
	}, nil
}

// IsSupportedWakeAction 判断当前动作是否属于网关允许的唤醒动作集合。
func IsSupportedWakeAction(action string) bool {
	_, exists := supportedWakeActionSet[strings.ToLower(strings.TrimSpace(action))]
	return exists
}

// resolveAction 从 URL host 或 path 提取动作名。
func resolveAction(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	if host := strings.TrimSpace(parsed.Hostname()); host != "" {
		return host
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return ""
	}

	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[0])
}

// flattenQueryValues 将 URL Query 标准化为单值 map（同名参数取最后一个值）。
func flattenQueryValues(values url.Values) map[string]string {
	params := make(map[string]string, len(values))
	for key, valueList := range values {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		if len(valueList) == 0 {
			params[normalizedKey] = ""
			continue
		}
		params[normalizedKey] = strings.TrimSpace(valueList[len(valueList)-1])
	}
	return params
}

// popQueryParam 读取并移除 query 参数，支持多个候选键名按顺序回退。
func popQueryParam(params map[string]string, keys ...string) string {
	if len(params) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := params[key]; ok {
			delete(params, key)
			return value
		}
	}
	return ""
}

// newParseError 创建 URL 解析结构化错误。
func newParseError(code, message string) *ParseError {
	return &ParseError{
		Code:    code,
		Message: message,
	}
}
