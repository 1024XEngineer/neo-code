package chatcompletions

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

const errorPrefix = "openaicompat provider: "

const maxSessionAssetReadBytes = session.MaxSessionAssetBytes
const maxSessionAssetsTotalBytes = provider.MaxSessionAssetsTotalBytes

// BuildRequest 将 provider.GenerateRequest 转换为 Chat Completions 请求结构。
// 模型优先取 req.Model，其次使用配置中的默认模型。
func BuildRequest(ctx context.Context, cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (Request, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return Request{}, errors.New(errorPrefix + "model is empty")
	}

	payload := Request{
		Model:    model,
		Stream:   true,
		Messages: make([]Message, 0, len(req.Messages)+1),
	}
	assetPolicy := session.NormalizeAssetPolicy(cfg.SessionAssetPolicy)
	requestBudget := provider.NormalizeRequestAssetBudget(cfg.RequestAssetBudget, assetPolicy.MaxSessionAssetBytes)

	if strings.TrimSpace(req.SystemPrompt) != "" {
		payload.Messages = append(payload.Messages, Message{
			Role:    providertypes.RoleSystem,
			Content: req.SystemPrompt,
		})
	}

	var usedSessionAssetBytes int64
	for _, message := range req.Messages {
		remainingSessionAssetBytes := requestBudget.MaxSessionAssetsTotalBytes - usedSessionAssetBytes
		msg, consumedBytes, err := toOpenAIMessageWithBudget(
			ctx,
			message,
			req.SessionAssetReader,
			remainingSessionAssetBytes,
			assetPolicy.MaxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return Request{}, err
		}
		usedSessionAssetBytes += consumedBytes
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
					Parameters:  provider.NormalizeToolSchemaObject(spec.Schema),
				},
			}
			payload.Tools = append(payload.Tools, def)
		}
	}

	return payload, nil
}

// normalizeToolSchemaForOpenAI 归一化工具参数 schema，避免修改调用方原始结构并尽量保持语义。
// 仅在缺失 schema 或明显非法（非 object 顶层）时做最小兼容降级，不再删除顶层组合约束关键字。

// cloneSchemaTopLevel 复制 schema 顶层 map，避免归一化阶段修改调用方原始结构。

// ToOpenAIMessage 将通用 Message 转换为 OpenAI 协议消息格式。
func ToOpenAIMessage(ctx context.Context, message providertypes.Message, assetReader providertypes.SessionAssetReader) (Message, error) {
	msg, _, err := toOpenAIMessageWithBudget(
		ctx,
		message,
		assetReader,
		maxSessionAssetsTotalBytes,
		session.DefaultAssetPolicy().MaxSessionAssetBytes,
		provider.DefaultRequestAssetBudget(),
	)
	return msg, err
}

// ToOpenAIMessageWithBudget 将通用 Message 转换为 OpenAI 协议消息格式，并应用会话附件预算限制。
func ToOpenAIMessageWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (Message, int64, error) {
	return toOpenAIMessageWithBudget(ctx, message, assetReader, remainingAssetBudget, maxSessionAssetBytes, requestBudget)
}

// toOpenAIMessageWithBudget 将通用 Message 转换为 OpenAI 协议消息格式，并记录 session_asset 消耗字节数。
func toOpenAIMessageWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (Message, int64, error) {
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}
	if err := providertypes.ValidateParts(message.Parts); err != nil {
		return Message{}, 0, fmt.Errorf("%sinvalid message parts: %w", errorPrefix, err)
	}

	out := Message{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
	}
	var usedAssetBytes int64

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
					if part.Image.Asset == nil || strings.TrimSpace(part.Image.Asset.ID) == "" {
						return Message{}, 0, errors.New("session_asset image missing asset id")
					}
					if assetReader == nil {
						return Message{}, 0, errors.New("session_asset reader is not configured")
					}
					imageURL, consumedBudgetBytes, err := resolveSessionAssetDataURL(
						ctx,
						assetReader,
						part.Image.Asset,
						remainingAssetBudget-usedAssetBytes,
						maxSessionAssetBytes,
						requestBudget,
					)
					if err != nil {
						return Message{}, 0, err
					}
					usedAssetBytes += consumedBudgetBytes
					contentParts = append(contentParts, MessageContentPart{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: imageURL,
						},
					})
				default:
					return Message{}, 0, errors.New("unsupported source type for image part")
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

	return out, usedAssetBytes, nil
}

// resolveSessionAssetDataURL 读取会话附件并转换为可发送的 data URL，仅在请求阶段临时生成。
func resolveSessionAssetDataURL(
	ctx context.Context,
	assetReader providertypes.SessionAssetReader,
	asset *providertypes.AssetRef,
	remainingBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (string, int64, error) {
	normalizedMime, data, readBytes, err := provider.ReadSessionAssetImage(
		ctx,
		assetReader,
		asset,
		remainingBudget,
		maxSessionAssetBytes,
		requestBudget,
	)
	if err != nil {
		return "", 0, err
	}
	normalizedBudget := provider.NormalizeRequestAssetBudget(requestBudget, maxSessionAssetBytes)
	transportBytes := provider.EstimateDataURLTransportBytes(readBytes, normalizedMime)
	if transportBytes > remainingBudget {
		return "", 0, fmt.Errorf("session_asset total exceeds %d bytes", normalizedBudget.MaxSessionAssetsTotalBytes)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", normalizedMime, encoded), transportBytes, nil
}
