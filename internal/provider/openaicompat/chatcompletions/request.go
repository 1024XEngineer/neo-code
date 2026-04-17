package chatcompletions

import (
	"context"
	"encoding/base64"
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

const maxSessionAssetReadBytes = providertypes.MaxSessionAssetBytes
const maxSessionAssetsTotalBytes = providertypes.MaxSessionAssetsTotalBytes

// BuildRequest 将 provider.GenerateRequest 转换为 Chat Completions 请求结构。
// 模型优先取 req.Model，其次使用配置中的默认模型。
func BuildRequest(ctx context.Context, cfg provider.RuntimeConfig, req providertypes.GenerateRequest) (Request, error) {
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

	var usedSessionAssetBytes int64
	for _, message := range req.Messages {
		remainingSessionAssetBytes := maxSessionAssetsTotalBytes - usedSessionAssetBytes
		msg, consumedBytes, err := toOpenAIMessageWithBudget(
			ctx,
			message,
			req.SessionAssetReader,
			remainingSessionAssetBytes,
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
					Parameters:  normalizeToolSchemaForOpenAI(spec.Schema),
				},
			}
			payload.Tools = append(payload.Tools, def)
		}
	}

	return payload, nil
}

// normalizeToolSchemaForOpenAI 归一化工具参数 schema，避免修改调用方原始结构并尽量保持语义。
// 仅在缺失 schema 或明显非法（非 object 顶层）时做最小兼容降级，不再删除顶层组合约束关键字。
func normalizeToolSchemaForOpenAI(schema map[string]any) map[string]any {
	normalized := cloneSchemaTopLevel(schema)
	if len(normalized) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	typeName, _ := normalized["type"].(string)
	if strings.TrimSpace(strings.ToLower(typeName)) != "object" {
		normalized["type"] = "object"
		normalized["x-neocode-schema-downgraded"] = true
	}

	if _, ok := normalized["properties"].(map[string]any); !ok {
		normalized["properties"] = map[string]any{}
		if strings.TrimSpace(strings.ToLower(typeName)) != "object" {
			normalized["x-neocode-schema-downgraded"] = true
		}
	}
	return normalized
}

// cloneSchemaTopLevel 复制 schema 顶层 map，避免归一化阶段修改调用方原始结构。
func cloneSchemaTopLevel(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(schema))
	for key, value := range schema {
		cloned[key] = value
	}
	return cloned
}

// ToOpenAIMessage 将通用 Message 转换为 OpenAI 协议消息格式。
func ToOpenAIMessage(ctx context.Context, message providertypes.Message, assetReader providertypes.SessionAssetReader) (Message, error) {
	msg, _, err := toOpenAIMessageWithBudget(ctx, message, assetReader, maxSessionAssetsTotalBytes)
	return msg, err
}

// toOpenAIMessageWithBudget 将通用 Message 转换为 OpenAI 协议消息格式，并记录 session_asset 消耗字节数。
func toOpenAIMessageWithBudget(
	ctx context.Context,
	message providertypes.Message,
	assetReader providertypes.SessionAssetReader,
	remainingAssetBudget int64,
) (Message, int64, error) {
	if remainingAssetBudget < 0 {
		remainingAssetBudget = 0
	}
	if err := providertypes.ValidateParts(message.Parts); err != nil {
		return Message{}, 0, fmt.Errorf("%sinvalid message parts: %w", shared.ErrorPrefix, err)
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
					imageURL, readBytes, err := resolveSessionAssetDataURL(
						ctx,
						assetReader,
						part.Image.Asset,
						remainingAssetBudget-usedAssetBytes,
					)
					if err != nil {
						return Message{}, 0, err
					}
					usedAssetBytes += readBytes
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
) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if remainingBudget <= 0 {
		return "", 0, fmt.Errorf(
			"session_asset total exceeds %d bytes",
			maxSessionAssetsTotalBytes,
		)
	}
	reader, mimeType, err := assetReader.Open(ctx, asset.ID)
	if err != nil {
		return "", 0, fmt.Errorf("open session_asset %q: %w", asset.ID, err)
	}
	defer func() { _ = reader.Close() }()

	readLimit := maxSessionAssetReadBytes
	if remainingBudget < readLimit {
		readLimit = remainingBudget
	}

	data, err := io.ReadAll(io.LimitReader(reader, readLimit+1))
	if err != nil {
		return "", 0, fmt.Errorf("read session_asset %q: %w", asset.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if int64(len(data)) > readLimit {
		if readLimit < maxSessionAssetReadBytes {
			return "", 0, fmt.Errorf(
				"session_asset total exceeds %d bytes",
				maxSessionAssetsTotalBytes,
			)
		}
		return "", 0, fmt.Errorf("session_asset %q exceeds %d bytes", asset.ID, maxSessionAssetReadBytes)
	}
	if len(data) == 0 {
		return "", 0, fmt.Errorf("session_asset %q is empty", asset.ID)
	}

	resolvedMime := strings.TrimSpace(mimeType)
	if resolvedMime == "" {
		resolvedMime = strings.TrimSpace(asset.MimeType)
	}
	normalizedMime := strings.ToLower(resolvedMime)
	if normalizedMime == "" {
		return "", 0, fmt.Errorf("session_asset %q missing mime type", asset.ID)
	}
	if !strings.HasPrefix(normalizedMime, "image/") {
		return "", 0, fmt.Errorf("session_asset %q has unsupported mime type %q", asset.ID, resolvedMime)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", normalizedMime, encoded), int64(len(data)), nil
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
