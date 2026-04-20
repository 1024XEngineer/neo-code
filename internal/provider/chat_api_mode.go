package provider

import (
	"fmt"
)

const (
	ChatAPIModeChatCompletions = "chat_completions"
	ChatAPIModeResponses       = "responses"
)

// NormalizeProviderChatAPIMode 统一归一化 openaicompat 的聊天协议模式，并校验取值是否合法。
func NormalizeProviderChatAPIMode(mode string) (string, error) {
	normalized := NormalizeKey(mode)
	switch normalized {
	case "":
		return "", nil
	case ChatAPIModeChatCompletions, ChatAPIModeResponses:
		return normalized, nil
	default:
		return "", fmt.Errorf("provider chat_api_mode %q is unsupported", mode)
	}
}

// DefaultProviderChatAPIMode 返回 openaicompat 在未显式指定模式时的默认协议模式。
func DefaultProviderChatAPIMode() string {
	return ChatAPIModeChatCompletions
}
