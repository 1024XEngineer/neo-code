package deepseek

import (
	"context"
	"encoding/json"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

// BuildRequest 将 GenerateRequest 转换为 chatcompletions.Request。
func BuildRequest(ctx context.Context, cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (chatcompletions.Request, error) {
	return chatcompletions.BuildRequest(ctx, cfg, req)
}

// InjectThinkingParams 将基础请求 JSON 注入 DeepSeek 特定的 thinking 控制参数。
func InjectThinkingParams(body []byte, tc providertypes.ThinkingConfig) ([]byte, error) {
	if tc.Enabled {
		return injectEnabledThinking(body, tc)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["thinking"] = map[string]any{"type": "disabled"}
	result, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func injectEnabledThinking(body []byte, tc providertypes.ThinkingConfig) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["thinking"] = map[string]any{"type": "enabled"}
	if tc.Effort != "" {
		raw["reasoning_effort"] = tc.Effort
	}
	return json.Marshal(raw)
}

// ExtractContinuity 将 reasoning_content 存入消息的 ThinkingMetadata 中以备续轮使用。
func ExtractContinuity(msg *providertypes.Message, reasoningContent string) {
	if msg == nil || reasoningContent == "" {
		return
	}
	meta, err := json.Marshal(map[string]string{
		"reasoning_content": reasoningContent,
	})
	if err != nil {
		return
	}
	msg.ThinkingMetadata = json.RawMessage(meta)
}
