package components

import "unicode/utf8"

// fallback 在主值为空时回退到默认值。
func fallback(primary string, fallbackValue string) string {
	if primary != "" {
		return primary
	}
	return fallbackValue
}

// trimMiddle 在长度超限时保留首尾内容并使用省略号连接。
func trimMiddle(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if limit <= 3 {
		return string(runes[:limit])
	}
	head := (limit - 3) / 2
	tail := limit - 3 - head
	return string(runes[:head]) + "..." + string(runes[len(runes)-tail:])
}

// trimRunes 按 rune 边界安全截断文本。
func trimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 4 {
		return text
	}
	return string(runes[:limit-3]) + "..."
}

// clamp 将数值限制在给定区间内。
func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
