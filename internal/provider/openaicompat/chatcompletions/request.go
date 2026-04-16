package chatcompletions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/shared"
	providertypes "neo-code/internal/provider/types"
)

// BuildRequest 将 provider.GenerateRequest 转换为 Chat Completions 请求结构。
// 模型优先取 req.Model，其次使用配置中的默认模型。
func BuildRequest(cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (Request, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return Request{}, errors.New(shared.ErrorPrefix + "model is empty")
	}

	payload := Request{
		Model:    model,
		Stream:   true,
		Messages: make([]Message, 0, len(req.Messages)+1),
	}

	if strings.TrimSpace(req.SystemPrompt) != "" {
		payload.Messages = append(payload.Messages, Message{
			Role:    providertypes.RoleSystem,
			Content: req.SystemPrompt,
		})
	}

	for _, message := range req.Messages {
		msg, err := ToOpenAIMessage(message)
		if err != nil {
			return Request{}, err
		}
		payload.Messages = append(payload.Messages, msg)
	}

	if len(req.Tools) > 0 {
		payload.ToolChoice = "auto"
		payload.Tools = make([]ToolDefinition, 0, len(req.Tools))
		for _, spec := range req.Tools {
			def := ToolDefinition{
				Type: "function",
				Function: FunctionDefinition{
					Name:        spec.Name,
					Description: spec.Description,
					Parameters:  spec.Schema,
				},
			}
			payload.Tools = append(payload.Tools, def)
		}
	}

	return payload, nil
}

// ToOpenAIMessage 将通用 Message 转换为 OpenAI 协议消息格式。
func ToOpenAIMessage(message providertypes.Message) (Message, error) {
	if err := providertypes.ValidateParts(message.Parts); err != nil {
		return Message{}, fmt.Errorf("%sinvalid message parts: %w", shared.ErrorPrefix, err)
	}

	out := Message{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
	}

	var hasImage bool
	for _, part := range message.Parts {
		if part.Kind == providertypes.ContentPartImage {
			hasImage = true
			break
		}
	}

	if !hasImage {
		var textBuilder strings.Builder
		for _, part := range message.Parts {
			if part.Kind == providertypes.ContentPartText {
				textBuilder.WriteString(part.Text)
			}
		}
		if text := textBuilder.String(); text != "" {
			out.Content = text
		}
	} else {
		var contentParts []MessageContentPart
		for _, part := range message.Parts {
			switch part.Kind {
			case providertypes.ContentPartText:
				contentParts = append(contentParts, MessageContentPart{
					Type: "text",
					Text: part.Text,
				})
			case providertypes.ContentPartImage:
				switch {
				case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceRemote:
					contentParts = append(contentParts, MessageContentPart{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: part.Image.URL,
						},
					})
				case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceSessionAsset:
					return Message{}, errors.New("session_asset image is not supported in this phase")
				default:
					return Message{}, errors.New("unsupported image part payload")
				}
			}
		}
		if len(contentParts) > 0 {
			out.Content = contentParts
		}
	}

	if len(message.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   call.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
	}

	return out, nil
}

// ParseError 解析 HTTP 错误响应并包装为 ProviderError。
func ParseError(resp *http.Response) error {
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return provider.NewProviderErrorFromStatus(resp.StatusCode,
			fmt.Sprintf("%sread error response: %v", shared.ErrorPrefix, readErr))
	}

	var parsed ErrorResponse
	if err := json.Unmarshal(data, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return provider.NewProviderErrorFromStatus(resp.StatusCode, parsed.Error.Message)
	}

	bodyText := strings.TrimSpace(string(data))
	if bodyText == "" {
		return provider.NewProviderErrorFromStatus(resp.StatusCode, resp.Status)
	}

	return provider.NewProviderErrorFromStatus(resp.StatusCode, bodyText)
}
