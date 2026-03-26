package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"neocode/internal/provider"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Provider implements the normalized Provider interface against an OpenAI-compatible chat completions API.
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// New constructs an OpenAI-compatible provider.
func New(name, baseURL, apiKey string, timeout time.Duration) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	return &Provider{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the configured provider name.
func (p *Provider) Name() string {
	return p.name
}

// Chat sends a normalized chat request to the OpenAI-compatible endpoint.
func (p *Provider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	httpResp, err := p.sendRequest(ctx, buildChatCompletionRequest(req, req.Stream))
	if err != nil {
		return provider.ChatResponse{}, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return provider.ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return provider.ChatResponse{}, fmt.Errorf("provider returned %s: %s", httpResp.Status, strings.TrimSpace(string(body)))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return provider.ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return provider.ChatResponse{}, fmt.Errorf("provider returned no choices")
	}

	choice := parsed.Choices[0]
	return provider.ChatResponse{
		Message:      fromChatMessage(choice.Message),
		FinishReason: choice.FinishReason,
		Usage: provider.Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}, nil
}

// ChatStream streams content deltas while reconstructing the final response.
func (p *Provider) ChatStream(
	ctx context.Context,
	req provider.ChatRequest,
	onDelta func(delta string) error,
) (provider.ChatResponse, error) {
	httpResp, err := p.sendRequest(ctx, buildChatCompletionRequest(req, true))
	if err != nil {
		return provider.ChatResponse{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
		return provider.ChatResponse{}, fmt.Errorf("provider returned %s: %s", httpResp.Status, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	finalMessage := provider.Message{Role: provider.RoleAssistant}
	finishReason := ""
	toolCallsByIndex := make(map[int]*provider.ToolCall)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk chatCompletionStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return provider.ChatResponse{}, fmt.Errorf("decode stream chunk: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.Delta.Role != "" {
			finalMessage.Role = choice.Delta.Role
		}
		if choice.Delta.Content != "" {
			finalMessage.Content += choice.Delta.Content
			if onDelta != nil {
				if err := onDelta(choice.Delta.Content); err != nil {
					return provider.ChatResponse{}, err
				}
			}
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			accumulator, ok := toolCallsByIndex[toolCall.Index]
			if !ok {
				accumulator = &provider.ToolCall{}
				toolCallsByIndex[toolCall.Index] = accumulator
			}
			if toolCall.ID != "" {
				accumulator.ID = toolCall.ID
			}
			if toolCall.Function.Name != "" {
				accumulator.Name += toolCall.Function.Name
			}
			if toolCall.Function.Arguments != "" {
				accumulator.Arguments += toolCall.Function.Arguments
			}
		}
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		return provider.ChatResponse{}, fmt.Errorf("read stream: %w", err)
	}

	if len(toolCallsByIndex) > 0 {
		indices := make([]int, 0, len(toolCallsByIndex))
		for index := range toolCallsByIndex {
			indices = append(indices, index)
		}
		sort.Ints(indices)

		finalMessage.ToolCalls = make([]provider.ToolCall, 0, len(indices))
		for _, index := range indices {
			finalMessage.ToolCalls = append(finalMessage.ToolCalls, *toolCallsByIndex[index])
		}
	}

	return provider.ChatResponse{
		Message:      finalMessage,
		FinishReason: finishReason,
	}, nil
}

func (p *Provider) sendRequest(ctx context.Context, requestBody chatCompletionRequest) (*http.Response, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if requestBody.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}

	return httpResp, nil
}

func buildChatCompletionRequest(req provider.ChatRequest, stream bool) chatCompletionRequest {
	requestBody := chatCompletionRequest{
		Model:    req.Model,
		Messages: make([]chatMessage, 0, len(req.Messages)),
		Stream:   stream,
	}

	for _, message := range req.Messages {
		requestBody.Messages = append(requestBody.Messages, toChatMessage(message))
	}

	if len(req.Tools) > 0 {
		requestBody.Tools = make([]chatTool, 0, len(req.Tools))
		for _, spec := range req.Tools {
			requestBody.Tools = append(requestBody.Tools, chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name:        spec.Name,
					Description: spec.Description,
					Parameters:  spec.InputSchema,
				},
			})
		}
	}

	return requestBody
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type chatCompletionStreamResponse struct {
	Choices []struct {
		Delta struct {
			Role      string               `json:"role"`
			Content   string               `json:"content"`
			ToolCalls []chatStreamToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type chatStreamToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

func toChatMessage(message provider.Message) chatMessage {
	converted := chatMessage{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
	}

	if len(message.ToolCalls) > 0 {
		converted.ToolCalls = make([]chatToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, chatToolCall{
				ID:   call.ID,
				Type: "function",
				Function: chatToolCallFunction{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
	}

	if message.Content != "" || len(message.ToolCalls) == 0 {
		converted.Content = message.Content
	} else {
		converted.Content = nil
	}

	return converted
}

func fromChatMessage(message chatMessage) provider.Message {
	converted := provider.Message{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
	}

	if content, ok := message.Content.(string); ok {
		converted.Content = content
	}

	if len(message.ToolCalls) > 0 {
		converted.ToolCalls = make([]provider.ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, provider.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
	}

	return converted
}
