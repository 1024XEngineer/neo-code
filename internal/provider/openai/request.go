package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// buildRequest 将 provider.GenerateRequest 转换为 OpenAI API 请求结构。
// 模型优先取 req.Model，其次使用配置中的默认模型。
func (p *Provider) buildRequest(req providertypes.GenerateRequest) (chatCompletionRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.cfg.DefaultModel)
	}
	if model == "" {
		return chatCompletionRequest{}, errors.New("openai provider: model is empty")
	}

	payload := chatCompletionRequest{
		Model:    model,
		Stream:   true,
		Messages: make([]openAIMessage, 0, len(req.Messages)+1),
	}

	if strings.TrimSpace(req.SystemPrompt) != "" {
		payload.Messages = append(payload.Messages, openAIMessage{
			Role:    providertypes.RoleSystem,
			Content: req.SystemPrompt,
		})
	}

	for _, message := range req.Messages {
		payload.Messages = append(payload.Messages, toOpenAIMessage(message))
	}

	if len(req.Tools) > 0 {
		payload.ToolChoice = "auto"
		payload.Tools = make([]openAIToolDefinition, 0, len(req.Tools))
		for _, spec := range req.Tools {
			payload.Tools = append(payload.Tools, openAIToolDefinition{
				Type: "function",
				Function: openAIFunctionDefinition{
					Name:        spec.Name,
					Description: spec.Description,
					Parameters:  spec.Schema,
				},
			})
		}
	}

	return payload, nil
}

// toOpenAIMessage 将通用 Message 转换为 OpenAI 协议消息格式。
func toOpenAIMessage(message providertypes.Message) openAIMessage {
	out := openAIMessage{
		Role:       message.Role,
		Content:    message.Content,
		ToolCallID: message.ToolCallID,
	}

	if len(message.ToolCalls) > 0 {
		out.ToolCalls = make([]openAIToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, openAIToolCall{
				ID:   call.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
	}

	return out
}

// parseError 解析 HTTP 错误响应并包装为 ProviderError。
func (p *Provider) parseError(resp *http.Response) error {
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return provider.NewProviderErrorFromStatus(resp.StatusCode,
			fmt.Sprintf("openai provider: read error response: %v", readErr))
	}

	var parsed openAIErrorResponse
	if err := json.Unmarshal(data, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return provider.NewProviderErrorFromStatus(resp.StatusCode, parsed.Error.Message)
	}

	bodyText := strings.TrimSpace(string(data))
	if bodyText == "" {
		return provider.NewProviderErrorFromStatus(resp.StatusCode, resp.Status)
	}

	return provider.NewProviderErrorFromStatus(resp.StatusCode, bodyText)
}
