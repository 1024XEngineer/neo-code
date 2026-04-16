package tools

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

const (
	// DefaultOutputLimitBytes is the default max size of tool output content.
	DefaultOutputLimitBytes = 64 * 1024
	truncatedSuffix         = "\n...[truncated]"

	maxProjectedToolMetadataKeys     = 6
	maxProjectedToolMetadataValueLen = 160
)

var projectedToolMetadataAllowlist = map[string]struct{}{
	"bytes":              {},
	"count":              {},
	"emitted_bytes":      {},
	"filtered_count":     {},
	"http_status":        {},
	"matched_count":      {},
	"matched_files":      {},
	"matched_lines":      {},
	"mcp_server_id":      {},
	"mcp_tool_name":      {},
	"path":               {},
	"relative_path":      {},
	"replacement_length": {},
	"returned_count":     {},
	"root":               {},
	"search_length":      {},
	"status_code":        {},
	"tool_name":          {},
	"truncated":          {},
	"workdir":            {},
}

// ApplyOutputLimit truncates tool output content and adds a truncated metadata flag.
func ApplyOutputLimit(result ToolResult, limit int) ToolResult {
	if limit <= 0 {
		return result
	}
	if len(result.Content) <= limit {
		return result
	}

	result.Content = result.Content[:limit] + truncatedSuffix
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if existing, ok := result.Metadata["truncated"].(bool); !ok || !existing {
		result.Metadata["truncated"] = true
	}
	return result
}

// SanitizeToolMetadata 过滤并裁剪写入会话的工具 metadata，避免把内部或大字段永久带入对话状态。
func SanitizeToolMetadata(toolName string, metadata map[string]any) map[string]string {
	sanitized := make(map[string]string)
	if name := strings.TrimSpace(toolName); name != "" {
		sanitized["tool_name"] = name
	}

	if len(metadata) == 0 {
		if len(sanitized) == 0 {
			return nil
		}
		return sanitized
	}

	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		key = strings.TrimSpace(key)
		if _, ok := projectedToolMetadataAllowlist[key]; !ok || key == "tool_name" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if len(sanitized) >= maxProjectedToolMetadataKeys {
			break
		}
		value, ok := sanitizeToolMetadataValue(metadata[key])
		if !ok {
			continue
		}
		sanitized[key] = value
	}

	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

// FormatToolMessageForModel 将持久化的 tool 消息投影为仅供模型消费的结构化文本。
func FormatToolMessageForModel(message providertypes.Message) string {
	lines := []string{"tool result"}

	if toolName := strings.TrimSpace(message.ToolMetadata["tool_name"]); toolName != "" {
		lines = append(lines, "tool: "+toolName)
	}

	status := "ok"
	if message.IsError {
		status = "error"
	}
	lines = append(lines, "status: "+status)

	if toolCallID := strings.TrimSpace(message.ToolCallID); toolCallID != "" {
		lines = append(lines, "tool_call_id: "+toolCallID)
	}

	lines = append(lines, fmt.Sprintf("truncated: %t", toolMessageTruncated(message.ToolMetadata)))
	lines = append(lines, formatToolMetadataLines(message.ToolMetadata)...)

	content := renderToolMessageParts(message.Parts)
	if strings.TrimSpace(content) != "" {
		lines = append(lines, "", "content:", content)
	}

	return strings.Join(lines, "\n")
}

// renderToolMessageParts 将工具消息分片渲染为模型可消费的文本，图片仅保留安全占位。
func renderToolMessageParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderDisplayParts(parts)
}

// FormatError builds a consistent error payload for tool failures.
func FormatError(toolName string, reason string, details string) string {
	toolName = strings.TrimSpace(toolName)
	reason = strings.TrimSpace(reason)
	details = strings.TrimSpace(details)

	lines := []string{"tool error"}
	if toolName != "" {
		lines = append(lines, "tool: "+toolName)
	}
	if reason != "" {
		lines = append(lines, "reason: "+reason)
	}
	if details != "" {
		lines = append(lines, "details: "+details)
	}

	return strings.Join(lines, "\n")
}

// NormalizeErrorReason strips the tool name prefix from an error message, if present.
func NormalizeErrorReason(toolName string, err error) string {
	if err == nil {
		return ""
	}
	reason := strings.TrimSpace(err.Error())
	if toolName == "" {
		return reason
	}

	prefix := strings.ToLower(strings.TrimSpace(toolName)) + ":"
	lower := strings.ToLower(reason)
	if strings.HasPrefix(lower, prefix) {
		return strings.TrimSpace(reason[len(prefix):])
	}
	return reason
}

// NewErrorResult returns a standardized error ToolResult.
func NewErrorResult(toolName string, reason string, details string, metadata map[string]any) ToolResult {
	return ToolResult{
		Name:     toolName,
		Content:  FormatError(toolName, reason, details),
		IsError:  true,
		Metadata: metadata,
	}
}

// toolMessageTruncated 从持久化的轻量 metadata 中提取截断标记，缺省时返回 false。
func toolMessageTruncated(metadata map[string]string) bool {
	if metadata == nil {
		return false
	}
	truncated, _ := strconv.ParseBool(strings.TrimSpace(metadata["truncated"]))
	return truncated
}

// formatToolMetadataLines 以稳定顺序输出供模型消费的 metadata 行，并跳过已提升为顶层字段的键。
func formatToolMetadataLines(metadata map[string]string) []string {
	if len(metadata) == 0 {
		return nil
	}

	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		key = strings.TrimSpace(key)
		if key == "" || key == "tool_name" || key == "truncated" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, "meta."+key+": "+sanitizeToolMetadataString(metadata[key]))
	}
	return lines
}

// sanitizeToolMetadataValue 将原始 metadata 值收敛为短小的单行文本，不接受复杂结构。
func sanitizeToolMetadataValue(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		text := sanitizeToolMetadataString(v)
		return text, text != ""
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case int8, int16, int32, int64:
		return fmt.Sprint(v), true
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(v), true
	case float32, float64:
		return fmt.Sprint(v), true
	default:
		return "", false
	}
}

// sanitizeToolMetadataString 将 metadata 字符串裁剪为单行短文本，避免提示词膨胀。
func sanitizeToolMetadataString(value string) string {
	text := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n").Replace(strings.TrimSpace(value))
	if text == "" {
		return ""
	}
	if len(text) > maxProjectedToolMetadataValueLen {
		return text[:maxProjectedToolMetadataValueLen] + "..."
	}
	return text
}
