package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	modeldiscovery "neo-code/internal/provider/discovery"
	"neo-code/internal/provider/transport"
)

type Provider struct {
	cfg    config.ResolvedProviderConfig
	client *http.Client
}

type buildOptions struct {
	transport http.RoundTripper
}

type BuildOption func(*buildOptions)

// WithTransport 注入自定义 HTTP Transport（如 RetryTransport）。
func WithTransport(rt http.RoundTripper) BuildOption {
	return func(o *buildOptions) {
		o.transport = rt
	}
}

const DriverName = "openai"

// defaultRetryTransport 返回内置的带重试的 HTTP Transport。
func defaultRetryTransport() http.RoundTripper {
	return transport.NewRetryTransport(http.DefaultTransport, transport.DefaultRetryConfig())
}

// Driver 返回 OpenAI 协议驱动的定义。
func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return New(cfg, WithTransport(defaultRetryTransport()))
		},
		Discover: func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
			provider, err := New(cfg, WithTransport(defaultRetryTransport()))
			if err != nil {
				return nil, err
			}
			return provider.DiscoverModels(ctx)
		},
	}
}

func New(cfg config.ResolvedProviderConfig, opts ...BuildOption) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("openai provider: %w", err)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openai provider: api key is empty")
	}

	o := &buildOptions{
		transport: http.DefaultTransport,
	}
	for _, apply := range opts {
		apply(o)
	}

	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout:   90 * time.Second,
			Transport: o.transport,
		},
	}, nil
}

func (p *Provider) DiscoverModels(ctx context.Context) ([]config.ModelDescriptor, error) {
	rawModels, err := modeldiscovery.FetchOpenAICompatibleModels(ctx, p.client, p.cfg.BaseURL, p.cfg.APIKey)
	if err != nil {
		return nil, err
	}

	descriptors := make([]config.ModelDescriptor, 0, len(rawModels))
	for _, raw := range rawModels {
		descriptor, ok := config.DescriptorFromRawModel(raw)
		if !ok {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return config.MergeModelDescriptors(descriptors), nil
}

func (p *Provider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	payload, err := p.buildRequest(req)
	if err != nil {
		return provider.ChatResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return provider.ChatResponse{}, fmt.Errorf("openai provider: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return provider.ChatResponse{}, fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return provider.ChatResponse{}, fmt.Errorf("openai provider: send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("openai provider: close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return provider.ChatResponse{}, p.parseError(resp)
	}

	return p.consumeStream(ctx, resp.Body, events)
}

func (p *Provider) buildRequest(req provider.ChatRequest) (chatCompletionRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.cfg.Model)
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
			Role:    provider.RoleSystem,
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

func (p *Provider) consumeStream(ctx context.Context, body io.Reader, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	reader := bufio.NewReader(body)

	var (
		contentBuilder strings.Builder
		finishReason   string
		usage          provider.Usage
		done           bool
	)

	toolCalls := make(map[int]*provider.ToolCall)
	dataLines := make([]string, 0, 4)

	// processChunk 解析单个 SSE data payload，更新累积状态。
	// 返回错误表示应中止流；done 标志通过闭包变量传递。
	processChunk := func(payload string) error {
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("openai provider: decode stream chunk: %w", err)
		}

		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return errors.New(chunk.Error.Message)
		}

		extractStreamUsage(&usage, chunk.Usage)

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				contentBuilder.WriteString(choice.Delta.Content)
				if err := emitTextDelta(ctx, events, choice.Delta.Content); err != nil {
					return err
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				if err := mergeToolCallDelta(ctx, events, toolCalls, delta); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// finishStream 统一的流结束处理：发送 message_done 事件并组装最终响应。
	finishStream := func() (provider.ChatResponse, error) {
		if err := emitMessageDone(ctx, events, finishReason, &usage); err != nil {
			return provider.ChatResponse{}, err
		}
		return finalizeResponse(contentBuilder.String(), toolCalls, finishReason, usage), nil
	}

	flushPendingData := func() error {
		defer func() {
			dataLines = dataLines[:0]
		}()
		return flushDataLines(dataLines, processChunk)
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return provider.ChatResponse{}, fmt.Errorf("openai provider: read stream: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		case trimmed == "":
			if flushErr := flushPendingData(); flushErr != nil {
				return provider.ChatResponse{}, flushErr
			}
			if done {
				return finishStream()
			}
		case strings.HasPrefix(trimmed, ":"):
			// SSE comment/heartbeat; ignore.
		}

		if errors.Is(err, io.EOF) {
			if flushErr := flushPendingData(); flushErr != nil {
				return provider.ChatResponse{}, flushErr
			}
			return finishStream()
		}
	}
}

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

func toOpenAIMessage(message provider.Message) openAIMessage {
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

func emitTextDelta(ctx context.Context, events chan<- provider.StreamEvent, text string) error {
	if text == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.StreamEvent{
		Type: provider.StreamEventTextDelta,
		Text: text,
	})
}

func emitToolCallStart(ctx context.Context, events chan<- provider.StreamEvent, index int, id, name string) error {
	if name == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.StreamEvent{
		Type:          provider.StreamEventToolCallStart,
		ToolCallID:    id,
		ToolName:      name,
		ToolCallIndex: index,
	})
}

// emitToolCallDelta 发送工具调用参数增量事件。
func emitToolCallDelta(ctx context.Context, events chan<- provider.StreamEvent, index int, argumentsDelta string) error {
	if argumentsDelta == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.StreamEvent{
		Type:               provider.StreamEventToolCallDelta,
		ToolCallIndex:      index,
		ToolArgumentsDelta: argumentsDelta,
	})
}

// emitMessageDone 发送消息完成事件。
func emitMessageDone(ctx context.Context, events chan<- provider.StreamEvent, finishReason string, usage *provider.Usage) error {
	if events == nil {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.StreamEvent{
		Type:         provider.StreamEventMessageDone,
		FinishReason: finishReason,
		Usage:        usage,
	})
}

// extractStreamUsage 从 OpenAI usage 响应提取并覆盖累积的 token 统计。
func extractStreamUsage(usage *provider.Usage, raw *openAIUsage) {
	if raw == nil {
		return
	}
	*usage = provider.Usage{
		InputTokens:  raw.PromptTokens,
		OutputTokens: raw.CompletionTokens,
		TotalTokens:  raw.TotalTokens,
	}
}

// mergeToolCallDelta 将单个 tool call delta 累积到 toolCalls map 中。
// 首次发现带名称的 delta 时发送 tool_call_start 事件；
// 每次收到 arguments 增量时发送 tool_call_delta 事件。
func mergeToolCallDelta(ctx context.Context, events chan<- provider.StreamEvent, toolCalls map[int]*provider.ToolCall, delta toolCallDelta) error {
	call, exists := toolCalls[delta.Index]
	if !exists {
		call = &provider.ToolCall{}
		toolCalls[delta.Index] = call
	}

	hadName := strings.TrimSpace(call.Name) != ""

	if id := strings.TrimSpace(delta.ID); id != "" {
		call.ID = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.Name = name
	}

	if !hadName && strings.TrimSpace(call.Name) != "" {
		if err := emitToolCallStart(ctx, events, delta.Index, call.ID, call.Name); err != nil {
			return err
		}
	}

	// 发送参数增量事件（同一 chunk 可能同时携带 name 和 arguments）
	if args := delta.Function.Arguments; args != "" {
		call.Arguments += args
		if err := emitToolCallDelta(ctx, events, delta.Index, args); err != nil {
			return err
		}
	}
	return nil
}

// finalizeResponse 将累积的内容、tool calls 和元数据组装为最终 ChatResponse。
func finalizeResponse(content string, toolCalls map[int]*provider.ToolCall, finishReason string, usage provider.Usage) provider.ChatResponse {
	ordered := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)

	message := provider.Message{
		Role:    provider.RoleAssistant,
		Content: content,
	}

	for _, index := range ordered {
		call := toolCalls[index]
		if call == nil {
			continue
		}
		message.ToolCalls = append(message.ToolCalls, *call)
	}

	if finishReason == "" && len(message.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return provider.ChatResponse{
		Message:      message,
		FinishReason: finishReason,
		Usage:        usage,
	}
}

func emitStreamEvent(ctx context.Context, events chan<- provider.StreamEvent, event provider.StreamEvent) error {
	if events == nil {
		return nil
	}

	select {
	case events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flushDataLines 将缓冲的 data lines 合并为单个 payload 并通过 processChunk 处理。
func flushDataLines(dataLines []string, processChunk func(string) error) error {
	if len(dataLines) == 0 {
		return nil
	}
	return processChunk(strings.Join(dataLines, "\n"))
}

type chatCompletionRequest struct {
	Model      string                 `json:"model"`
	Messages   []openAIMessage        `json:"messages"`
	Tools      []openAIToolDefinition `json:"tools,omitempty"`
	ToolChoice string                 `json:"tool_choice,omitempty"`
	Stream     bool                   `json:"stream"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolDefinition struct {
	Type     string                   `json:"type"`
	Function openAIFunctionDefinition `json:"function"`
}

type openAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chatCompletionChunk struct {
	Choices []struct {
		Index        int        `json:"index"`
		Delta        chunkDelta `json:"delta"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type chunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []toolCallDelta `json:"tool_calls,omitempty"`
}

type toolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}
