package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
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

type buildOption func(*buildOptions)

// withTransport 注入自定义 HTTP Transport（如 RetryTransport）。
func withTransport(rt http.RoundTripper) buildOption {
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
			return New(cfg, withTransport(defaultRetryTransport()))
		},
		Discover: func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
			p, err := New(cfg, withTransport(defaultRetryTransport()))
			if err != nil {
				return nil, err
			}
			return p.DiscoverModels(ctx)
		},
	}
}

func New(cfg config.ResolvedProviderConfig, opts ...buildOption) (*Provider, error) {
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

// Chat 发起 SSE 流式对话请求，支持透明重连。
//
// 流中途断连时，将已累积的 assistant 消息（文本 + tool call）注入请求上下文，
// 利用 OpenAI 多轮对话语义实现断点续传，对上层调用方透明。
// 最多重连 maxReconnects 次；不可恢复错误直接返回。
func (p *Provider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) error {
	const maxReconnects = 3

	// 保存原始消息列表的副本，避免重连时反复 append 到同一个切片导致上下文污染
	originalMessages := make([]provider.Message, len(req.Messages))
	copy(originalMessages, req.Messages)

	// 跨重连周期持久化的累积状态：已收到的文本和 tool call
	var (
		accumText  strings.Builder
		accumCalls map[int]*provider.ToolCall
	)

	for attempt := 0; attempt <= maxReconnects; attempt++ {
		if attempt > 0 {
			// 从原始消息出发构造本次请求的完整消息列表
			req.Messages = make([]provider.Message, len(originalMessages), len(originalMessages)+1)
			copy(req.Messages, originalMessages)

			// 仅在有实际累积内容时注入 assistant 快照，避免插入空消息
			if accumText.Len() > 0 || len(accumCalls) > 0 {
				req.Messages = append(req.Messages,
					p.buildAssistantMsg(&accumText, accumCalls))
			}

			// 指数退避等待
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := p.chatOnce(ctx, req, events, &accumText, &accumCalls)
		if err == nil {
			return nil
		}
		if !provider.IsRecoverableStreamError(err) {
			return err
		}
		// 可恢复但重连次数已耗尽 → 标记为不可重试，防止上层 runtime 重试叠加放大。
		if attempt == maxReconnects {
			return provider.MarkNonRetryable(err)
		}
	}
	return nil // unreachable，但满足编译器
}

// chatOnce 执行单次 HTTP 请求 + 流消费，不包含重连逻辑。
// accumText 和 accumCalls 为跨重连周期的共享状态引用。
func (p *Provider) chatOnce(
	ctx context.Context,
	req provider.ChatRequest,
	events chan<- provider.StreamEvent,
	accumText *strings.Builder,
	accumCalls *map[int]*provider.ToolCall,
) error {
	payload, err := p.buildRequest(req)
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("openai provider: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai provider: send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("openai provider: close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return p.parseError(resp)
	}

	return p.consumeStream(ctx, resp.Body, events, accumText, accumCalls)
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

// consumeStream 消费 SSE 响应流，使用有界读取器防止缓冲区溢出。
// 所有文本增量同步写入 accumText，tool call 增量同步写入 accumCalls，
// 以便重连时能从断点恢复。
func (p *Provider) consumeStream(
	ctx context.Context,
	body io.Reader,
	events chan<- provider.StreamEvent,
	accumText *strings.Builder,
	accumCalls *map[int]*provider.ToolCall,
) error {
	reader := newBoundedSSEReader(body)

	var (
		finishReason string
		usage        provider.Usage
		done         bool
	)

	dataLines := make([]string, 0, 4)

	// processChunk 解析单个 SSE data payload，更新累积状态并发送事件。
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
				// 同步累积文本（重连时用于构造 assistant 消息）
				accumText.WriteString(choice.Delta.Content)
				if err := emitTextDelta(ctx, events, choice.Delta.Content); err != nil {
					return err
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				if err := mergeToolCallDeltaWithAccum(ctx, events, accumCalls, delta); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// finishStream 统一的流结束处理：发送 message_done 事件。
	finishStream := func() error {
		return emitMessageDone(ctx, events, finishReason, &usage)
	}

	flushPendingData := func() error {
		defer func() { dataLines = dataLines[:0] }()
		return flushDataLines(dataLines, processChunk)
	}

	for {
		line, err := reader.ReadLine()

		if err != nil && !errors.Is(err, io.EOF) {
			// 非 EOF 的读取错误：先刷新缓冲的 data 行，再包装为流中断，
			// 避免中断前最后一段数据丢失。
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			return fmt.Errorf("%w: %w", provider.ErrStreamInterrupted, err)
		}

		trimmed := line

		switch {
		case strings.HasPrefix(trimmed, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		case trimmed == "":
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			if done {
				return finishStream()
			}
		case strings.HasPrefix(trimmed, ":"):
			// SSE comment/heartbeat; ignore.
		}

		if errors.Is(err, io.EOF) {
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
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
	return emitStreamEvent(ctx, events, provider.NewTextDeltaStreamEvent(text))
}

func emitToolCallStart(ctx context.Context, events chan<- provider.StreamEvent, index int, id, name string) error {
	if name == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.NewToolCallStartStreamEvent(index, id, name))
}

// emitToolCallDelta 发送工具调用参数增量事件。
// id 为工具调用 ID，由上游 mergeToolCallDelta 从累积状态中传入。
func emitToolCallDelta(ctx context.Context, events chan<- provider.StreamEvent, index int, id, argumentsDelta string) error {
	if argumentsDelta == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, provider.NewToolCallDeltaStreamEvent(index, id, argumentsDelta))
}

// emitMessageDone 发送消息完成事件。
func emitMessageDone(ctx context.Context, events chan<- provider.StreamEvent, finishReason string, usage *provider.Usage) error {
	return emitStreamEvent(ctx, events, provider.NewMessageDoneStreamEvent(finishReason, usage))
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
		if err := emitToolCallDelta(ctx, events, delta.Index, call.ID, args); err != nil {
			return err
		}
	}
	return nil
}

// buildAssistantMsg 从累积状态构造 assistant 角色的 OpenAI 消息，
// 用于重连时注入请求上下文，实现断点续传。
func (p *Provider) buildAssistantMsg(accumText *strings.Builder, accumCalls map[int]*provider.ToolCall) provider.Message {
	msg := provider.Message{
		Role:    provider.RoleAssistant,
		Content: accumText.String(),
	}
	if len(accumCalls) > 0 {
		calls := make([]provider.ToolCall, 0, len(accumCalls))
		for _, c := range accumCalls {
			calls = append(calls, *c)
		}
		msg.ToolCalls = calls
	}
	return msg
}

// mergeToolCallDeltaWithAccum 在 mergeToolCallDelta 的基础上，
// 同步将 tool call 累积状态写入跨周期的 accumCalls（*map[int]*ToolCall）。
func mergeToolCallDeltaWithAccum(
	ctx context.Context,
	events chan<- provider.StreamEvent,
	accumCalls *map[int]*provider.ToolCall,
	delta toolCallDelta,
) error {
	if *accumCalls == nil {
		*accumCalls = make(map[int]*provider.ToolCall)
	}

	// 先确保 accumCalls 中有对应条目
	call, exists := (*accumCalls)[delta.Index]
	if !exists {
		call = &provider.ToolCall{}
		(*accumCalls)[delta.Index] = call
	}

	// 复用原有逻辑处理事件发送和局部累积
	return mergeToolCallDelta(ctx, events, *accumCalls, delta)
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
