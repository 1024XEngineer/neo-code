package gemini

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

// BuildRequest 将通用 GenerateRequest 直接转换为 Gemini SDK 入参，避免中间协议结构。
func BuildRequest(
	ctx context.Context,
	cfg provider.RuntimeConfig,
	req providertypes.GenerateRequest,
) (string, []*genai.Content, *genai.GenerateContentConfig, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return "", nil, nil, errors.New(errorPrefix + "model is empty")
	}

	contents := make([]*genai.Content, 0, len(req.Messages))
	assetPolicy := session.NormalizeAssetPolicy(cfg.SessionAssetPolicy)
	requestBudget := provider.NormalizeRequestAssetBudget(cfg.RequestAssetBudget, assetPolicy.MaxSessionAssetBytes)
	var usedSessionAssetBytes int64
	for _, message := range req.Messages {
		remainingSessionAssetBytes := requestBudget.MaxSessionAssetsTotalBytes - usedSessionAssetBytes
		content, consumedBytes, err := toGeminiContentWithBudget(
			ctx,
			message,
			req.SessionAssetReader,
			remainingSessionAssetBytes,
			assetPolicy.MaxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return "", nil, nil, err
		}
		usedSessionAssetBytes += consumedBytes
		if content == nil || len(content.Parts) == 0 {
			continue
		}
		contents = append(contents, content)
	}

	config := &genai.GenerateContentConfig{}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		config.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: req.SystemPrompt}}}
	}
	if len(req.Tools) > 0 {
		tools := make([]*genai.Tool, 0, len(req.Tools))
		functionDecls := make([]*genai.FunctionDeclaration, 0, len(req.Tools))
		for _, spec := range req.Tools {
			functionDecls = append(functionDecls, &genai.FunctionDeclaration{
				Name:                 strings.TrimSpace(spec.Name),
				Description:          strings.TrimSpace(spec.Description),
				ParametersJsonSchema: provider.NormalizeToolSchemaObject(spec.Schema),
			})
		}
		tools = append(tools, &genai.Tool{FunctionDeclarations: functionDecls})
		config.Tools = tools
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto},
		}
	}

	return model, contents, config, nil
}

// toGeminiContentWithBudget 将通用消息转换为 Gemini Content，并记录 session_asset 消耗字节数。
func toGeminiContentWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (*genai.Content, int64, error) {
	if err := providertypes.ValidateParts(message.Parts); err != nil {
		return nil, 0, fmt.Errorf("%sinvalid message parts: %w", errorPrefix, err)
	}
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}
	var usedAssetBytes int64

	switch strings.TrimSpace(message.Role) {
	case providertypes.RoleSystem:
		return nil, usedAssetBytes, nil
	case providertypes.RoleUser:
		parts, consumedBytes, err := toGeminiPartsWithBudget(
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
		usedAssetBytes += consumedBytes
		return &genai.Content{Role: "user", Parts: parts}, usedAssetBytes, nil
	case providertypes.RoleAssistant:
		parts, consumedBytes, err := toGeminiAssistantPartsWithBudget(
			ctx,
			message,
			assetReader,
			remainingAssetBudget,
			maxSessionAssetBytes,
			requestBudget,
		)
		if err != nil {
			return nil, 0, err
		}
		usedAssetBytes += consumedBytes
		return &genai.Content{Role: "model", Parts: parts}, usedAssetBytes, nil
	case providertypes.RoleTool:
		part, err := toGeminiToolResultPart(message)
		if err != nil {
			return nil, 0, err
		}
		return &genai.Content{Role: "user", Parts: []*genai.Part{part}}, usedAssetBytes, nil
	default:
		return nil, 0, fmt.Errorf("%sunsupported message role %q", errorPrefix, message.Role)
	}
}

// toGeminiAssistantPartsWithBudget 将助手消息转换为 Gemini SDK Part，并记录 session_asset 消耗。
func toGeminiAssistantPartsWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) ([]*genai.Part, int64, error) {
	result, consumedBytes, err := toGeminiPartsWithBudget(
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
		args, err := provider.DecodeToolArgumentsToObject(call.Arguments)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, &genai.Part{
			FunctionCall: &genai.FunctionCall{
				ID:   strings.TrimSpace(call.ID),
				Name: name,
				Args: args,
			},
		})
	}
	return result, consumedBytes, nil
}

// toGeminiPartsWithBudget 将文本/图片片段映射到 Gemini SDK Part，并执行 session_asset 预算校验。
func toGeminiPartsWithBudget(
	ctx context.Context,
	parts []providertypes.ContentPart,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) ([]*genai.Part, int64, error) {
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}
	result := make([]*genai.Part, 0, len(parts))
	var usedAssetBytes int64
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if part.Text != "" {
				result = append(result, &genai.Part{Text: part.Text})
			}
		case providertypes.ContentPartImage:
			switch {
			case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceRemote:
				result = append(result, &genai.Part{
					FileData: &genai.FileData{
						FileURI: part.Image.URL,
					},
				})
			case part.Image != nil && part.Image.SourceType == providertypes.ImageSourceSessionAsset:
				if part.Image.Asset == nil || strings.TrimSpace(part.Image.Asset.ID) == "" {
					return nil, 0, errors.New("session_asset image missing asset id")
				}
				if assetReader == nil {
					return nil, 0, errors.New("session_asset reader is not configured")
				}
				inlineData, readBytes, err := resolveSessionAssetInlineData(
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
				result = append(result, &genai.Part{InlineData: inlineData})
			default:
				return nil, 0, errors.New("unsupported source type for image part")
			}
		}
	}
	return result, usedAssetBytes, nil
}

// toGeminiToolResultPart 将工具结果消息映射为 Gemini functionResponse 分片。
func toGeminiToolResultPart(message providertypes.Message) (*genai.Part, error) {
	toolName := strings.TrimSpace(message.ToolMetadata["tool_name"])
	if toolName == "" {
		toolName = "tool"
	}

	response := map[string]any{}
	text := provider.RenderMessageText(message.Parts)
	if text != "" {
		response["content"] = text
	}
	if toolCallID := strings.TrimSpace(message.ToolCallID); toolCallID != "" {
		response["tool_call_id"] = toolCallID
	}
	if len(message.ToolMetadata) > 0 {
		metadata := make(map[string]any, len(message.ToolMetadata))
		for key, value := range message.ToolMetadata {
			metadata[key] = value
		}
		response["metadata"] = metadata
	}
	if len(response) == 0 {
		response["content"] = ""
	}

	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{Name: toolName, Response: response},
	}, nil
}

// decodeToolArgumentsToObject 将工具参数 JSON 解码为对象，失败时回退 raw 字符串包装。

// renderMessageText 折叠消息中的文本片段，供工具结果透传使用。

// normalizeToolSchemaForGemini 归一化工具参数 schema，确保顶层为 object。

// cloneSchemaTopLevel 复制 schema 顶层 map，避免归一化阶段污染调用方输入。

// resolveSessionAssetInlineData 读取会话附件并转换为 Gemini 可发送的 inlineData。
func resolveSessionAssetInlineData(
	ctx context.Context,
	assetReader providertypes.SessionAssetReader,
	asset *providertypes.AssetRef,
	remainingBudget int64,
	maxSessionAssetBytes int64,
	requestBudget provider.RequestAssetBudget,
) (*genai.Blob, int64, error) {
	normalizedMime, data, readBytes, err := provider.ReadSessionAssetImage(
		ctx,
		assetReader,
		asset,
		remainingBudget,
		maxSessionAssetBytes,
		requestBudget,
	)
	if err != nil {
		return nil, 0, err
	}
	return &genai.Blob{MIMEType: normalizedMime, Data: data}, readBytes, nil
}
