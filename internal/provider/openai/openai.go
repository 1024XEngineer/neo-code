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
	domain "neo-code/internal/provider"
)

type Provider struct {
	cfg    config.ResolvedProviderConfig
	client *http.Client
}

const DriverName = "openai"

// Driver 返回 OpenAI 协议驱动的定义。
func Driver() domain.DriverDefinition {
	return domain.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (domain.Provider, error) {
			return New(cfg)
		},
	}
}

func New(cfg config.ResolvedProviderConfig) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("openai provider: %w", err)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openai provider: api key is empty")
	}

	return &Provider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 90 * time.Second,
		},
	}, nil
}

func (p *Provider) Chat(ctx context.Context, req domain.ChatRequest, events chan<- domain.StreamEvent) (domain.ChatResponse, error) {
	payload, err := p.buildRequest(req)
	if err != nil {
		return domain.ChatResponse{}, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.ChatResponse{}, fmt.Errorf("openai provider: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return domain.ChatResponse{}, fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return domain.ChatResponse{}, fmt.Errorf("openai provider: send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("openai provider: close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return domain.ChatResponse{}, p.parseError(resp)
	}

	return p.consumeStream(ctx, resp.Body, events)
}

func (p *Provider) buildRequest(req domain.ChatRequest) (chatCompletionRequest, error) {
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
			Role:    domain.RoleSystem,
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

func (p *Provider) consumeStream(ctx context.Context, body io.Reader, events chan<- domain.StreamEvent) (domain.ChatResponse, error) {
	reader := bufio.NewReader(body)

	var (
		contentBuilder strings.Builder
		finishReason   string
		usage          domain.Usage
		done           bool
	)

	toolCalls := make(map[int]*domain.ToolCall)
	dataLines := make([]string, 0, 4)

	flushEvent := func() error {
		if len(dataLines) == 0 {
			return nil
		}

		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]

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

		if chunk.Usage != nil {
			usage = domain.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}

		for _, choice := range chunk.Choices {
			if strings.TrimSpace(choice.FinishReason) != "" {
				finishReason = choice.FinishReason
			}

			if text := choice.Delta.Content; text != "" {
				contentBuilder.WriteString(text)
				if err := emitTextDelta(ctx, events, text); err != nil {
					return err
				}
			}

			for _, discovered := range mergeToolCallDeltas(toolCalls, choice.Delta.ToolCalls) {
				if err := emitToolCallStart(ctx, events, discovered.ID, discovered.Name); err != nil {
					return err
				}
			}
		}

		return nil
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return domain.ChatResponse{}, fmt.Errorf("openai provider: read stream: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		case trimmed == "":
			if flushErr := flushEvent(); flushErr != nil {
				return domain.ChatResponse{}, flushErr
			}
			if done {
				return finalizeResponse(contentBuilder.String(), toolCalls, finishReason, usage), nil
			}
		case strings.HasPrefix(trimmed, ":"):
			// SSE comment/heartbeat; ignore it.
		}

		if errors.Is(err, io.EOF) {
			if flushErr := flushEvent(); flushErr != nil {
				return domain.ChatResponse{}, flushErr
			}
			return finalizeResponse(contentBuilder.String(), toolCalls, finishReason, usage), nil
		}
	}
}

func (p *Provider) parseError(resp *http.Response) error {
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("openai provider: read error response: %w", readErr)
	}

	var parsed openAIErrorResponse
	if err := json.Unmarshal(data, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return errors.New(parsed.Error.Message)
	}

	bodyText := strings.TrimSpace(string(data))
	if bodyText == "" {
		return errors.New(resp.Status)
	}

	return fmt.Errorf("openai provider: %s", bodyText)
}

func toOpenAIMessage(message domain.Message) openAIMessage {
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

func emitTextDelta(ctx context.Context, events chan<- domain.StreamEvent, text string) error {
	if events == nil || text == "" {
		return nil
	}

	select {
	case events <- domain.StreamEvent{
		Type: domain.StreamEventTextDelta,
		Text: text,
	}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func emitToolCallStart(ctx context.Context, events chan<- domain.StreamEvent, id, name string) error {
	if events == nil || name == "" {
		return nil
	}

	select {
	case events <- domain.StreamEvent{
		Type:       domain.StreamEventToolCallStart,
		ToolCallID: id,
		ToolName:   name,
	}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// mergeToolCallDeltas 将流式增量合并到 target map 中，并返回本次新发现的 tool call 列表。
// 当一个新的 tool call 索引首次出现且拥有非空工具名称时，视为新发现。
func mergeToolCallDeltas(target map[int]*domain.ToolCall, deltas []toolCallDelta) []domain.ToolCall {
	var discovered []domain.ToolCall
	for _, delta := range deltas {
		call, exists := target[delta.Index]
		if !exists {
			call = &domain.ToolCall{}
			target[delta.Index] = call
		}

		if strings.TrimSpace(delta.ID) != "" {
			call.ID = delta.ID
		}
		if strings.TrimSpace(delta.Function.Name) != "" {
			call.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			call.Arguments += delta.Function.Arguments
		}

		if !exists && strings.TrimSpace(call.Name) != "" {
			discovered = append(discovered, *call)
		}
	}
	return discovered
}

func finalizeResponse(content string, toolCalls map[int]*domain.ToolCall, finishReason string, usage domain.Usage) domain.ChatResponse {
	ordered := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)

	message := domain.Message{
		Role:    domain.RoleAssistant,
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

	return domain.ChatResponse{
		Message:      message,
		FinishReason: finishReason,
		Usage:        usage,
	}
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
	} `json:"error"`
}
