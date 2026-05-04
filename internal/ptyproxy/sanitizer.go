package ptyproxy

import (
	"regexp"
	"strings"
	"unicode"
)

const defaultDiagnosisPayloadMaxBytes = 8 * 1024

var (
	ansiSequencePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|][^\x07]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

	awsAccessKeyPattern = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	jwtPattern          = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\b`)
	bearerTokenPattern  = regexp.MustCompile(`(?i)\bbearer\s+([a-z0-9\-._~+/]+=*)`)
	apiKeyPattern       = regexp.MustCompile(`(?i)\b(api[_-]?key|token|password|secret)\b\s*[:=]\s*([^\s,;]+)`)
	privateKeyPattern   = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

// SanitizeDiagnosisText 对诊断载荷执行降噪、截断和脱敏，必须只在 RPC 发起前一刻调用。
// 性能约束：禁止把该函数放入实时终端 I/O 热路径，避免高频正则导致 CPU 抖动。
func SanitizeDiagnosisText(raw string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultDiagnosisPayloadMaxBytes
	}
	trimmed := normalizeWhitespace(foldCarriageReturns(stripANSI(raw)))
	trimmed = TruncateUTF8HeadTail(trimmed, maxBytes)
	return redactSensitive(trimmed)
}

// stripANSI 去除 ANSI 控制序列，避免颜色编码污染模型输入。
func stripANSI(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return ansiSequencePattern.ReplaceAllString(raw, "")
}

// foldCarriageReturns 折叠 \r 覆盖输出，仅保留每行最终可见内容。
func foldCarriageReturns(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	for index, line := range lines {
		if strings.Contains(line, "\r") {
			line = strings.TrimRight(line, "\r")
			parts := strings.Split(line, "\r")
			if len(parts) > 0 {
				lines[index] = parts[len(parts)-1]
			} else {
				lines[index] = ""
			}
		}
	}
	return strings.Join(lines, "\n")
}

// normalizeWhitespace 清理不可打印字符并折叠多余空白。
func normalizeWhitespace(raw string) string {
	if raw == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(raw))
	lastSpace := false
	for _, char := range raw {
		if char == '\n' {
			builder.WriteRune(char)
			lastSpace = false
			continue
		}
		if unicode.IsSpace(char) {
			if lastSpace {
				continue
			}
			builder.WriteRune(' ')
			lastSpace = true
			continue
		}
		if !unicode.IsPrint(char) {
			continue
		}
		builder.WriteRune(char)
		lastSpace = false
	}
	return strings.TrimSpace(builder.String())
}

// redactSensitive 对常见高危凭证做强制打码。
func redactSensitive(raw string) string {
	if raw == "" {
		return ""
	}
	redacted := awsAccessKeyPattern.ReplaceAllString(raw, "AKIA[REDACTED]")
	redacted = jwtPattern.ReplaceAllString(redacted, "[JWT_REDACTED]")
	redacted = bearerTokenPattern.ReplaceAllString(redacted, "Bearer [REDACTED]")
	redacted = apiKeyPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
	redacted = privateKeyPattern.ReplaceAllString(redacted, "-----BEGIN PRIVATE KEY-----[REDACTED]-----END PRIVATE KEY-----")
	return redacted
}
