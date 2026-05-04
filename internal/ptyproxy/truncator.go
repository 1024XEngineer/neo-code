package ptyproxy

import "unicode/utf8"

const truncationMarker = "\n...[truncated]...\n"

// TruncateUTF8HeadTail 对文本执行 UTF-8 安全的首尾截断，避免长日志撑爆 token。
func TruncateUTF8HeadTail(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if maxBytes <= len(truncationMarker)+2 {
		return utf8SafePrefix(text, maxBytes)
	}

	budget := maxBytes - len(truncationMarker)
	headBudget := budget / 2
	tailBudget := budget - headBudget
	head := utf8SafePrefix(text, headBudget)
	tail := utf8SafeSuffix(text, tailBudget)
	return head + truncationMarker + tail
}

// utf8SafePrefix 按字节预算截取合法 UTF-8 前缀。
func utf8SafePrefix(text string, bytes int) string {
	if bytes <= 0 {
		return ""
	}
	raw := []byte(text)
	if len(raw) <= bytes {
		return text
	}
	candidate := raw[:bytes]
	for len(candidate) > 0 && !utf8.Valid(candidate) {
		candidate = candidate[:len(candidate)-1]
	}
	return string(candidate)
}

// utf8SafeSuffix 按字节预算截取合法 UTF-8 后缀。
func utf8SafeSuffix(text string, bytes int) string {
	if bytes <= 0 {
		return ""
	}
	raw := []byte(text)
	if len(raw) <= bytes {
		return text
	}
	start := len(raw) - bytes
	candidate := raw[start:]
	for len(candidate) > 0 && !utf8.Valid(candidate) {
		candidate = candidate[1:]
	}
	return string(candidate)
}
