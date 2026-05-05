package feishuadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// BuildSessionID 基于飞书 chat_id 生成稳定会话标识。
func BuildSessionID(chatID string) string {
	return "feishu_" + stableHash(chatID)
}

// BuildRunID 基于飞书 message_id 生成稳定运行标识。
func BuildRunID(messageID string) string {
	return "feishu_" + stableHash(messageID)
}

// stableHash 对外部输入做稳定散列，避免直接暴露原始平台标识。
func stableHash(raw string) string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}
