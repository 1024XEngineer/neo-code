package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	"neo-code/internal/provider/openaicompat/responses"
	providertypes "neo-code/internal/provider/types"
)

// generateSDKChatCompletions 走 SDK chat/completions 发送请求
func (p *Provider) generateSDKChatCompletions(
	ctx context.Context,
	payload chatcompletions.Request,
	events chan<- providertypes.StreamEvent,
) error {
	if shouldUseCompatibleChatCompletionsEndpoint(p.cfg) {
		return p.generateChatCompletionsWithCompatibleStream(ctx, payload, events)
	}

	client, err := p.newSDKClient()
	if err != nil {
		return err
	}
	params := convertToChatCompletionParams(payload)

	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	payloadStarted, err := emitSDKChatCompletionStream(ctx, stream, events)
	if err != nil {
		if mapped, ok := mapOpenAIError(err); ok {
			return mapped
		}
		if !shouldFallbackToCompatibleChatStream(err, payloadStarted) {
			return err
		}
		return p.generateChatCompletionsWithCompatibleStream(ctx, payload, events)
	}
	return nil
}

// emitSDKChatCompletionStream 转发 SDK typed stream 事件，并额外记录是否已真正发出有效 payload。
func emitSDKChatCompletionStream(
	ctx context.Context,
	stream any,
	events chan<- providertypes.StreamEvent,
) (bool, error) {
	proxyEvents := make(chan providertypes.StreamEvent, 16)
	forwardErrCh := make(chan error, 1)
	var payloadStarted atomic.Bool

	go func() {
		forwardErrCh <- forwardSDKStreamEvents(ctx, proxyEvents, events, &payloadStarted)
	}()

	err := chatcompletions.EmitFromSDKStream(ctx, stream, proxyEvents)
	close(proxyEvents)
	forwardErr := <-forwardErrCh
	if err == nil {
		err = forwardErr
	}
	return payloadStarted.Load(), err
}

// forwardSDKStreamEvents 负责把 SDK 事件转发给上层，同时只按有效 payload 规则标记流已开始。
func forwardSDKStreamEvents(
	ctx context.Context,
	source <-chan providertypes.StreamEvent,
	target chan<- providertypes.StreamEvent,
	payloadStarted *atomic.Bool,
) error {
	for event := range source {
		if provider.IsEffectiveGeneratePayloadEvent(event) {
			payloadStarted.Store(true)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case target <- event:
		}
	}
	return nil
}

// convertToChatCompletionParams 将内部 chat/completions 请求映射为 OpenAI SDK 参数对象。
func convertToChatCompletionParams(req chatcompletions.Request) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(req.Model),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, convertToSDKMessage(msg))
	}
	params.Messages = messages

	if len(req.Tools) > 0 {
		tools := make([]openai.ChatCompletionToolUnionParam, 0, len(req.Tools))
		for _, spec := range req.Tools {
			tools = append(tools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
				Name:        spec.Function.Name,
				Description: openai.String(spec.Function.Description),
				Parameters:  openai.FunctionParameters(spec.Function.Parameters),
			}))
		}
		params.Tools = tools
		if req.ToolChoice != "" {
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoAuto)),
			}
		}
	}

	return params
}

// convertToSDKMessage 将内部消息转换为 SDK ChatCompletion 消息参数，并保留 tool 调用语义。
func convertToSDKMessage(msg chatcompletions.Message) openai.ChatCompletionMessageParamUnion {
	contentText := normalizeMessageTextContent(msg.Content)

	switch msg.Role {
	case "system":
		return openai.SystemMessage(contentText)
	case "user":
		if contentParts, ok := toSDKUserContentParts(msg.Content); ok {
			return openai.UserMessage(contentParts)
		}
		return openai.UserMessage(contentText)
	case "assistant":
		var assistant openai.ChatCompletionAssistantMessageParam
		if contentText != "" {
			assistantMessage := openai.AssistantMessage(contentText)
			if assistantMessage.OfAssistant != nil {
				assistant = *assistantMessage.OfAssistant
			}
		}
		if toolCalls := toSDKAssistantToolCalls(msg.ToolCalls); len(toolCalls) > 0 {
			assistant.ToolCalls = toolCalls
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
	case "tool":
		return openai.ToolMessage(contentText, strings.TrimSpace(msg.ToolCallID))
	default:
		return openai.UserMessage(contentText)
	}
}

// normalizeMessageTextContent 将任意消息内容归一化为文本，保证角色降级时语义可读。
func normalizeMessageTextContent(content any) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []chatcompletions.MessageContentPart:
		var textBuilder strings.Builder
		for _, part := range value {
			if part.Type == "text" {
				textBuilder.WriteString(part.Text)
			}
		}
		if textBuilder.Len() > 0 {
			return textBuilder.String()
		}
		return fmt.Sprintf("%v", value)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// toSDKUserContentParts 将多模态消息转换为 SDK user content parts，无法转换时返回 false。
func toSDKUserContentParts(content any) ([]openai.ChatCompletionContentPartUnionParam, bool) {
	value, ok := content.([]chatcompletions.MessageContentPart)
	if !ok {
		return nil, false
	}

	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(value))
	for _, part := range value {
		switch part.Type {
		case "text":
			parts = append(parts, openai.TextContentPart(part.Text))
		case "image_url":
			if part.ImageURL == nil || strings.TrimSpace(part.ImageURL.URL) == "" {
				continue
			}
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: part.ImageURL.URL,
			}))
		}
	}
	return parts, len(parts) > 0
}

// toSDKAssistantToolCalls 将内部 assistant tool_calls 映射到 SDK 所需结构。
func toSDKAssistantToolCalls(calls []chatcompletions.ToolCall) []openai.ChatCompletionMessageToolCallUnionParam {
	if len(calls) == 0 {
		return nil
	}

	toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(calls))
	for _, call := range calls {
		toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: strings.TrimSpace(call.ID),
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      strings.TrimSpace(call.Function.Name),
					Arguments: call.Function.Arguments,
				},
			},
		})
	}
	return toolCalls
}

// shouldFallbackToCompatibleChatStream 判断是否需要从 SDK typed stream 降级到兼容流解析。
func shouldFallbackToCompatibleChatStream(err error, payloadStarted bool) bool {
	if err == nil {
		return false
	}
	if payloadStarted {
		return false
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	message := strings.ToLower(strings.TrimSpace(safeErrorMessage(err)))
	if !strings.Contains(message, "sdk stream error") {
		return false
	}
	return strings.Contains(message, "after top-level value") ||
		strings.Contains(message, "invalid character") ||
		strings.Contains(message, "cannot unmarshal") ||
		strings.Contains(message, "unexpected end of json input")
}

// mapOpenAIError 将 SDK 错误映射为统一的 ProviderError，便于 runtime 做分级处理。
func mapOpenAIError(err error) (error, bool) {
	var sdkErr *openai.Error
	if !errors.As(err, &sdkErr) || sdkErr == nil || sdkErr.StatusCode <= 0 {
		return nil, false
	}

	message := strings.TrimSpace(sdkErr.Message)
	if message == "" {
		message = strings.TrimSpace(safeErrorMessage(err))
	}
	return provider.NewProviderErrorFromStatus(sdkErr.StatusCode, message), true
}

// safeErrorMessage 安全获取错误文本，避免第三方错误类型在 Error() 中触发 panic。
func safeErrorMessage(err error) (message string) {
	if err == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			message = ""
		}
	}()
	return err.Error()
}

// generateChatCompletionsWithCompatibleStream 在弱 SSE 网关下回退到兼容解析逻辑，保证请求可继续。
func (p *Provider) generateChatCompletionsWithCompatibleStream(
	ctx context.Context,
	payload chatcompletions.Request,
	events chan<- providertypes.StreamEvent,
) error {
	endpoint, err := provider.ResolveChatEndpointURL(
		p.cfg.BaseURL,
		resolveChatEndpointPathByMode(p.cfg.ChatEndpointPath, provider.ChatAPIModeChatCompletions),
	)
	if err != nil {
		return fmt.Errorf("%sinvalid chat endpoint configuration: %w", errorPrefix, err)
	}

	client, err := p.newSDKClient()
	if err != nil {
		return err
	}
	var resp *http.Response
	err = client.Post(
		ctx,
		strings.TrimSpace(endpoint),
		payload,
		nil,
		option.WithResponseInto(&resp),
		option.WithHeader("Accept", "text/event-stream"),
	)
	if err != nil {
		return wrapSDKRequestError(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return ParseError(resp)
	}

	return chatcompletions.ConsumeStream(ctx, resp.Body, events)
}

// shouldUseCompatibleChatCompletionsEndpoint 判断 chat/completions 是否需要绕过 SDK 默认端点拼接。
func shouldUseCompatibleChatCompletionsEndpoint(cfg provider.RuntimeConfig) bool {
	resolvedEndpoint, err := provider.ResolveChatEndpointURL(
		cfg.BaseURL,
		resolveChatEndpointPathByMode(cfg.ChatEndpointPath, provider.ChatAPIModeChatCompletions),
	)
	if err != nil {
		return false
	}

	defaultEndpoint, err := provider.ResolveChatEndpointURL(cfg.BaseURL, chatEndpointPathCompletions)
	if err != nil {
		return false
	}

	return strings.TrimSpace(resolvedEndpoint) != strings.TrimSpace(defaultEndpoint)
}

// generateSDKResponses 走 SDK responses 发送请求，复用本地流事件映射。
func (p *Provider) generateSDKResponses(
	ctx context.Context,
	payload responses.Request,
	events chan<- providertypes.StreamEvent,
) error {
	endpoint, err := resolveChatEndpoint(p.cfg)
	if err != nil {
		return err
	}

	client, err := p.newSDKClient()
	if err != nil {
		return err
	}
	var resp *http.Response
	err = client.Post(
		ctx,
		strings.TrimSpace(endpoint),
		payload,
		nil,
		option.WithResponseInto(&resp),
		option.WithHeader("Accept", "text/event-stream"),
	)
	if err != nil {
		return wrapSDKRequestError(err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return ParseError(resp)
	}

	return responses.EmitFromStream(ctx, resp.Body, events)
}

// wrapSDKRequestError 将 SDK 请求错误映射为统一 ProviderError；无法映射时保留原始错误链。
func wrapSDKRequestError(err error, action string) error {
	if err == nil {
		return nil
	}
	if mapped, ok := mapOpenAIError(err); ok {
		return mapped
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	message := strings.TrimSpace(safeErrorMessage(err))
	if message == "" {
		message = "unknown transport error"
	}
	if isSDKTransportTimeout(err) {
		return provider.NewTimeoutProviderError(
			fmt.Sprintf("%s%s timeout: %s", errorPrefix, strings.TrimSpace(action), message),
		)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return provider.NewNetworkProviderError(
			fmt.Sprintf("%s%s: %s", errorPrefix, strings.TrimSpace(action), message),
		)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return provider.NewNetworkProviderError(
			fmt.Sprintf("%s%s: %s", errorPrefix, strings.TrimSpace(action), message),
		)
	}
	return fmt.Errorf("%s%s: %w", errorPrefix, strings.TrimSpace(action), err)
}

// isSDKTransportTimeout 统一识别 OpenAI-compatible 发送阶段的超时错误。
func isSDKTransportTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (p *Provider) newSDKClient() (openai.Client, error) {
	apiKey, err := p.cfg.ResolveAPIKeyValue()
	if err != nil {
		return openai.Client{}, err
	}
	return openai.NewClient(
		option.WithHTTPClient(p.generateClient),
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
		option.WithBaseURL(strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/")),
	), nil
}

func resolveChatEndpoint(cfg provider.RuntimeConfig) (string, error) {
	chatEndpointPath := resolveChatEndpointPathByMode(cfg.ChatEndpointPath, cfg.ChatAPIMode)
	endpoint, err := provider.ResolveChatEndpointURL(cfg.BaseURL, chatEndpointPath)
	if err != nil {
		return "", fmt.Errorf("%sinvalid chat endpoint configuration: %w", errorPrefix, err)
	}
	return endpoint, nil
}

// resolveChatEndpointPathByMode 在 chat endpoint 为空时，根据 chat_api_mode 自动回填默认端点路径。
func resolveChatEndpointPathByMode(rawPath string, chatAPIMode string) string {
	if strings.TrimSpace(rawPath) != "" {
		return rawPath
	}

	mode, err := provider.NormalizeProviderChatAPIMode(chatAPIMode)
	if err != nil || mode == "" {
		mode = provider.DefaultProviderChatAPIMode()
	}
	if mode == provider.ChatAPIModeResponses {
		return chatEndpointPathResponses
	}
	return chatEndpointPathCompletions
}
