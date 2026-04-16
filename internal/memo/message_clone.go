package memo

import providertypes "neo-code/internal/provider/types"

// cloneProviderMessage 深拷贝消息结构，避免后台流程读取到运行时后续修改。
func cloneProviderMessage(message providertypes.Message) providertypes.Message {
	cloned := message
	cloned.Parts = providertypes.CloneParts(message.Parts)
	if len(message.ToolCalls) > 0 {
		cloned.ToolCalls = append([]providertypes.ToolCall(nil), message.ToolCalls...)
	}
	if len(message.ToolMetadata) > 0 {
		cloned.ToolMetadata = make(map[string]string, len(message.ToolMetadata))
		for key, value := range message.ToolMetadata {
			cloned.ToolMetadata[key] = value
		}
	}
	return cloned
}
