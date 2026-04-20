package responses

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "openaicompat provider: "

// BuildRequest 将通用 GenerateRequest 转换为 Responses 请求结构。
func BuildRequest(ctx context.Context, cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (Request, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return Request{}, errors.New(errorPrefix + "model is empty")
	}

	payload := Request{
		Model:  model,
		Stream: true,
		Input:  make([]InputItem, 0, len(req.Messages)+1),
	}

	if strings.TrimSpace(req.SystemPrompt) != "" {
		payload.Instructions = req.SystemPrompt
	}

	assetLimits := providertypes.NormalizeSessionAssetLimits(cfg.SessionAssetLimits)
	var usedSessionAssetBytes int64
	for _, message := range req.Messages {
		remainingSessionAssetBytes := assetLimits.MaxSessionAssetsTotalBytes - usedSessionAssetBytes
		items, consumedBytes, err := toResponsesInputItems(
			ctx,
			message,
			req.SessionAssetReader,
			remainingSessionAssetBytes,
			assetLimits,
		)
		if err != nil {
			return Request{}, err
		}
		usedSessionAssetBytes += consumedBytes
		payload.Input = append(payload.Input, items...)
	}

	if len(req.Tools) > 0 {
		payload.ToolChoice = "auto"
		payload.Tools = make([]ToolDefinition, 0, len(req.Tools))
		for _, spec := range req.Tools {
			payload.Tools = append(payload.Tools, ToolDefinition{
				Type:        "function",
				Name:        strings.TrimSpace(spec.Name),
				Description: strings.TrimSpace(spec.Description),
				Parameters:  provider.NormalizeToolSchemaObject(spec.Schema),
			})
		}
	}

	return payload, nil
}

// toResponsesInputItems 将通用 Message 映射为 Responses 输入项。
func toResponsesInputItems(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	assetLimits providertypes.SessionAssetLimits,
) ([]InputItem, int64, error) {
	openaiMessage, consumedBytes, err := chatcompletions.ToOpenAIMessageWithBudget(
		ctx,
		message,
		assetReader,
		remainingAssetBudget,
		assetLimits,
	)
	if err != nil {
		return nil, 0, err
	}

	switch strings.TrimSpace(openaiMessage.Role) {
	case providertypes.RoleSystem:
		return nil, consumedBytes, nil
	case providertypes.RoleUser, providertypes.RoleAssistant:
		items := make([]InputItem, 0, 1+len(openaiMessage.ToolCalls))
		contentParts, err := toResponsesContentParts(openaiMessage.Content)
		if err != nil {
			return nil, 0, err
		}
		if len(contentParts) > 0 {
			items = append(items, InputItem{
				Role:    openaiMessage.Role,
				Content: contentParts,
			})
		}
		if strings.TrimSpace(openaiMessage.Role) == providertypes.RoleAssistant {
			for _, toolCall := range openaiMessage.ToolCalls {
				if strings.TrimSpace(toolCall.Function.Name) == "" {
					continue
				}
				items = append(items, InputItem{
					Type:      "function_call",
					CallID:    strings.TrimSpace(toolCall.ID),
					Name:      strings.TrimSpace(toolCall.Function.Name),
					Arguments: toolCall.Function.Arguments,
				})
			}
		}
		return items, consumedBytes, nil
	case providertypes.RoleTool:
		callID := strings.TrimSpace(openaiMessage.ToolCallID)
		if callID == "" {
			return nil, 0, errors.New(errorPrefix + "tool result message requires tool_call_id")
		}
		output, err := renderToolOutput(openaiMessage.Content)
		if err != nil {
			return nil, 0, err
		}
		return []InputItem{{
			Type:   "function_call_output",
			CallID: callID,
			Output: output,
		}}, consumedBytes, nil
	default:
		return nil, 0, fmt.Errorf("%sunsupported message role %q", errorPrefix, openaiMessage.Role)
	}
}

// toResponsesContentParts 将 chatcompletions 内容结构转为 responses 输入内容结构。
func toResponsesContentParts(content any) ([]InputContentPart, error) {
	switch typed := content.(type) {
	case nil:
		return nil, nil
	case string:
		if typed == "" {
			return nil, nil
		}
		return []InputContentPart{{Type: "input_text", Text: typed}}, nil
	case []chatcompletions.MessageContentPart:
		parts := make([]InputContentPart, 0, len(typed))
		for _, part := range typed {
			switch strings.TrimSpace(part.Type) {
			case "text":
				if part.Text == "" {
					continue
				}
				parts = append(parts, InputContentPart{Type: "input_text", Text: part.Text})
			case "image_url":
				if part.ImageURL == nil || strings.TrimSpace(part.ImageURL.URL) == "" {
					continue
				}
				parts = append(parts, InputContentPart{Type: "input_image", ImageURL: part.ImageURL.URL})
			}
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("%sunsupported content type %T for responses input", errorPrefix, content)
	}
}

// renderToolOutput 折叠 tool 消息内容，作为 function_call_output.output。
func renderToolOutput(content any) (string, error) {
	switch typed := content.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case []chatcompletions.MessageContentPart:
		var builder strings.Builder
		for _, part := range typed {
			if strings.TrimSpace(part.Type) == "text" {
				builder.WriteString(part.Text)
			}
		}
		return builder.String(), nil
	default:
		return "", fmt.Errorf("%sunsupported tool output content type %T", errorPrefix, content)
	}
}

// normalizeToolSchemaForResponses 归一化工具参数 schema，确保顶层为 object。

// cloneSchemaTopLevel 复制 schema 顶层 map，避免归一化阶段修改调用方输入。
