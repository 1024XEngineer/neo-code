package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

const defaultMaxTokens = 4096

// BuildRequest 将通用 GenerateRequest 直接转换为 Anthropic SDK 入参。
func BuildRequest(ctx context.Context, cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (anthropic.MessageNewParams, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return anthropic.MessageNewParams{}, errors.New(errorPrefix + "model is empty")
	}

	params := anthropic.MessageNewParams{
		MaxTokens: int64(defaultMaxTokens),
		Model:     anthropic.Model(model),
		Messages:  make([]anthropic.MessageParam, 0, len(req.Messages)),
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt}}
	}

	assetPolicy := session.NormalizeAssetPolicy(cfg.SessionAssetPolicy)
	requestBudget := provider.NormalizeRequestAssetBudget(cfg.RequestAssetBudget, assetPolicy.MaxSessionAssetBytes)
	var usedSessionAssetBytes int64
	for _, message := range req.Messages {
		remainingSessionAssetBytes := requestBudget.MaxSessionAssetsTotalBytes - usedSessionAssetBytes
		converted, consumedBytes, include, err := toAnthropicMessageWithBudget(
			ctx,
			message,
			req.SessionAssetReader,
			remainingSessionAssetBytes,
			assetPolicy.MaxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}
		usedSessionAssetBytes += consumedBytes
		if !include {
			continue
		}
		params.Messages = append(params.Messages, converted)
	}

	if len(req.Tools) > 0 {
		params.Tools = make([]anthropic.ToolUnionParam, 0, len(req.Tools))
		for _, spec := range req.Tools {
			schema, err := toAnthropicToolSchema(spec.Schema)
			if err != nil {
				return anthropic.MessageNewParams{}, err
			}
			converted := anthropic.ToolUnionParamOfTool(schema, strings.TrimSpace(spec.Name))
			if converted.OfTool != nil && strings.TrimSpace(spec.Description) != "" {
				converted.OfTool.Description = anthropic.String(strings.TrimSpace(spec.Description))
			}
			params.Tools = append(params.Tools, converted)
		}
	}

	return params, nil
}

// toAnthropicMessageWithBudget 将通用消息映射为 Anthropic SDK MessageParam，并记录 session_asset 消耗字节数。
func toAnthropicMessageWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (anthropic.MessageParam, int64, bool, error) {
	if err := providertypes.ValidateParts(message.Parts); err != nil {
		return anthropic.MessageParam{}, 0, false, fmt.Errorf("%sinvalid message parts: %w", errorPrefix, err)
	}
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}
	var usedAssetBytes int64

	switch strings.TrimSpace(message.Role) {
	case providertypes.RoleSystem:
		return anthropic.MessageParam{}, usedAssetBytes, false, nil
	case providertypes.RoleUser:
		blocks, consumedBytes, err := toAnthropicTextBlocksWithBudget(
			ctx,
			message.Parts,
			assetReader,
			remainingAssetBudget,
			maxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return anthropic.MessageParam{}, 0, false, err
		}
		usedAssetBytes += consumedBytes
		if len(blocks) == 0 {
			return anthropic.MessageParam{}, usedAssetBytes, false, nil
		}
		return anthropic.NewUserMessage(blocks...), usedAssetBytes, true, nil
	case providertypes.RoleAssistant:
		blocks, consumedBytes, err := toAnthropicAssistantBlocksWithBudget(
			ctx,
			message,
			assetReader,
			remainingAssetBudget,
			maxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return anthropic.MessageParam{}, 0, false, err
		}
		usedAssetBytes += consumedBytes
		if len(blocks) == 0 {
			return anthropic.MessageParam{}, usedAssetBytes, false, nil
		}
		return anthropic.NewAssistantMessage(blocks...), usedAssetBytes, true, nil
	case providertypes.RoleTool:
		block, err := toAnthropicToolResultBlock(message)
		if err != nil {
			return anthropic.MessageParam{}, 0, false, err
		}
		return anthropic.NewUserMessage(block), usedAssetBytes, true, nil
	default:
		return anthropic.MessageParam{}, 0, false, fmt.Errorf("%sunsupported message role %q", errorPrefix, message.Role)
	}
}

// toAnthropicTextBlocksWithBudget 将文本/图片内容转换为 Anthropic SDK 内容块，并记录 session_asset 消耗。
func toAnthropicTextBlocksWithBudget(
	ctx context.Context,
	parts []providertypes.ContentPart,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) ([]anthropic.ContentBlockParamUnion, int64, error) {
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}

	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	var usedAssetBytes int64
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if part.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(part.Text))
			}
		case providertypes.ContentPartImage:
			switch {
			case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceRemote:
				blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{
					URL: part.Image.URL,
				}))
			case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceSessionAsset:
				if part.Image.Asset == nil || strings.TrimSpace(part.Image.Asset.ID) == "" {
					return nil, 0, errors.New("session_asset image missing asset id")
				}
				if assetReader == nil {
					return nil, 0, errors.New("session_asset reader is not configured")
				}
				mediaType, encodedData, readBytes, err := resolveSessionAssetImageSource(
					ctx,
					assetReader,
					part.Image.Asset,
					remainingAssetBudget-usedAssetBytes,
					maxSessionAssetBytes,
					requestBudget,
				)
				if err != nil {
					return nil, 0, err
				}
				usedAssetBytes += readBytes
				blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, encodedData))
			default:
				return nil, 0, errors.New("unsupported source type for image part")
			}
		}
	}
	return blocks, usedAssetBytes, nil
}

// toAnthropicAssistantBlocksWithBudget 将助手消息转换为文本、图片与 tool_use 块。
func toAnthropicAssistantBlocksWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) ([]anthropic.ContentBlockParamUnion, int64, error) {
	blocks, consumedBytes, err := toAnthropicTextBlocksWithBudget(
		ctx,
		message.Parts,
		assetReader,
		remainingAssetBudget,
		maxSessionAssetBytes,
		requestBudget,
	)
	if err != nil {
		return nil, 0, err
	}
	for _, call := range message.ToolCalls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		input, err := provider.DecodeToolArgumentsToObject(call.Arguments)
		if err != nil {
			return nil, 0, err
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(
			strings.TrimSpace(call.ID),
			input,
			name,
		))
	}
	return blocks, consumedBytes, nil
}

// toAnthropicToolResultBlock 将工具结果消息映射为 tool_result 块。
func toAnthropicToolResultBlock(message providertypes.Message) (anthropic.ContentBlockParamUnion, error) {
	toolUseID := strings.TrimSpace(message.ToolCallID)
	if toolUseID == "" {
		return anthropic.ContentBlockParamUnion{}, errors.New(errorPrefix + "tool result message requires tool_call_id")
	}
	content := provider.RenderMessageText(message.Parts)
	return anthropic.NewToolResultBlock(toolUseID, content, false), nil
}

// decodeToolArgumentsToObject 将工具参数 JSON 解码为对象，失败时回退 raw 字符串包装。

// renderMessageText 折叠消息中的文本片段，供 tool_result 透传使用。

// normalizeToolSchema 归一化工具 schema，确保顶层为 object。

// cloneSchemaTopLevel 复制 schema 顶层 map，避免归一化阶段污染调用方输入。

// toAnthropicToolSchema 将 map 形式 JSON Schema 转为 SDK ToolInputSchemaParam。
func toAnthropicToolSchema(schema map[string]any) (anthropic.ToolInputSchemaParam, error) {
	normalized := provider.NormalizeToolSchemaObject(schema)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return anthropic.ToolInputSchemaParam{}, fmt.Errorf("%sinvalid tool schema: %w", errorPrefix, err)
	}
	var parsed anthropic.ToolInputSchemaParam
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return anthropic.ToolInputSchemaParam{}, fmt.Errorf("%sinvalid tool schema: %w", errorPrefix, err)
	}
	return parsed, nil
}

// resolveSessionAssetImageSource 读取会话附件并转换为 Anthropic 可发送的 base64 image source。
func resolveSessionAssetImageSource(
	ctx context.Context,
	assetReader providertypes.SessionAssetReader,
	asset *providertypes.AssetRef,
	remainingBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (string, string, int64, error) {
	normalizedMime, data, readBytes, err := provider.ReadSessionAssetImage(
		ctx,
		assetReader,
		asset,
		remainingBudget,
		maxSessionAssetBytes,
		requestBudget,
	)
	if err != nil {
		return "", "", 0, err
	}
	return normalizedMime, base64.StdEncoding.EncodeToString(data), readBytes, nil
}
