package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
)

const (
	requestTimeout = 90 * time.Second
	maxRetries     = 2
)

var (
	ErrInvalidAPIKey        = errors.New("鏃犳晥鐨?API Key")
	ErrAPIKeyValidationSoft = errors.New("API Key 鏍￠獙缁撴灉涓嶇‘瀹?")
)

type ChatCompletionProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

// GetModelName 杩斿洖鎻愪緵鏂瑰綋鍓嶆ā鍨嬶紝缂虹渷鏃朵娇鐢ㄩ粯璁ゆā鍨嬨€?
func (p *ChatCompletionProvider) GetModelName() string {
	if p.Model != "" {
		return p.Model
	}
	return DefaultModel()
}

type StreamResponse struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// NewChatProvider 涓烘寚瀹氭ā鍨嬪垱寤哄凡閰嶇疆鐨勮亰澶╂彁渚涙柟銆?
func NewChatProvider(model string) (domain.ChatProvider, error) {
	if configs.GlobalAppConfig == nil {
		return nil, fmt.Errorf("config.yaml is not loaded")
	}

	providerName := CurrentProvider()
	if model == "" {
		model = DefaultModel()
	}
	if model == "" {
		return nil, fmt.Errorf("ai.model is required for provider %s", providerName)
	}
	baseURL, err := ResolveChatEndpoint(configs.GlobalAppConfig, model)
	if err != nil {
		return nil, err
	}
	apiKey := configs.RuntimeAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("missing %s environment variable", configs.RuntimeAPIKeyEnvVarName())
	}

	return &ChatCompletionProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

// ValidateChatAPIKey 鎸夊綋鍓嶆彁渚涙柟閰嶇疆鏍￠獙杩愯鏃?API Key銆?
func ValidateChatAPIKey(ctx context.Context, cfg *configs.AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}

	providerName := providerNameFromConfig(cfg)
	if providerName == "" {
		return fmt.Errorf("unsupported ai.provider: %s", cfg.AI.Provider)
	}
	if strings.TrimSpace(cfg.AI.Model) == "" {
		cfg.AI.Model = DefaultModelForProvider(providerName)
	}
	if strings.TrimSpace(cfg.AI.Model) == "" {
		return fmt.Errorf("ai.model is required for provider %s", providerName)
	}

	return validateChatAPIKey(ctx, cfg)
}

// Chat 鍚戣亰澶╄ˉ鍏ㄦ帴鍙ｅ彂閫佹祦寮忚姹傚苟杩斿洖鏂囨湰鍒嗙墖銆?
func (p *ChatCompletionProvider) Chat(ctx context.Context, messages []domain.Message, tools []domain.ToolSchema) (<-chan domain.ChatEvent, error) {
	baseURL := strings.TrimSpace(p.BaseURL)
	if baseURL == "" {
		var err error
		baseURL, err = ResolveChatEndpoint(configs.GlobalAppConfig, p.GetModelName())
		if err != nil {
			return nil, err
		}
	}

	modelName := p.GetModelName()
	body := map[string]any{
		"model":    modelName,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("chat request marshal failed: %w", err)
	}

	resp, err := doRequestWithRetry(ctx, func(reqCtx context.Context) (*http.Response, error) {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("chat request create failed: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("retryable chat status: %s %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return resp, nil
	})
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chat request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	out := make(chan domain.ChatEvent)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		toolCalls := map[int]*domain.ChatToolCall{}
		hasToolCalls := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				emitStreamErrorMessage(ctx, out, streamReadError(err))
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				break
			}

			text, deltas, finishReason, err := decodeStreamDelta(data)
			if err != nil {
				emitStreamErrorMessage(ctx, out, err)
				return
			}
			if text != "" {
				text = stripThinkingTags(text)
				select {
				case <-ctx.Done():
					return
				case out <- domain.ChatEvent{Type: domain.ChatEventDelta, Content: text}:
				}
			}
			if len(deltas) > 0 {
				hasToolCalls = true
				for _, delta := range deltas {
					updateToolCall(toolCalls, delta)
				}
			}
			if finishReason == "tool_calls" {
				break
			}
		}

		if hasToolCalls {
			for _, call := range orderedToolCalls(toolCalls) {
				select {
				case <-ctx.Done():
					return
				case out <- domain.ChatEvent{Type: domain.ChatEventToolCall, ToolCall: call}:
				}
			}
		}
	}()

	return out, nil
}

func emitStreamErrorMessage(ctx context.Context, out chan<- domain.ChatEvent, err error) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("\n[STREAM_ERROR] %v", err)
	select {
	case <-ctx.Done():
	case out <- domain.ChatEvent{Type: domain.ChatEventDelta, Content: msg}:
	}
}

func streamReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("chat stream ended unexpectedly before completion")
	}
	return fmt.Errorf("chat stream read failed: %w", err)
}

func decodeStreamDelta(data string) (string, []toolCallDelta, string, error) {
	var res StreamResponse
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		return "", nil, "", fmt.Errorf("chat stream decode failed: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", nil, "", nil
	}
	choice := res.Choices[0]
	return choice.Delta.Content, choice.Delta.ToolCalls, choice.FinishReason, nil
}

func stripThinkingTags(content string) string {
	thinkStart := "<think>"
	thinkEnd := "</think>"
	for {
		start := strings.Index(content, thinkStart)
		if start == -1 {
			break
		}
		end := strings.Index(content, thinkEnd)
		if end == -1 {
			break
		}
		end += len(thinkEnd)
		content = content[:start] + content[end:]
	}
	return content
}

func updateToolCall(calls map[int]*domain.ChatToolCall, delta toolCallDelta) {
	call, ok := calls[delta.Index]
	if !ok {
		call = &domain.ChatToolCall{Type: "function"}
		calls[delta.Index] = call
	}
	if strings.TrimSpace(delta.ID) != "" {
		call.ID = delta.ID
	}
	if strings.TrimSpace(delta.Type) != "" {
		call.Type = delta.Type
	}
	if strings.TrimSpace(delta.Function.Name) != "" {
		call.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		call.Function.Arguments += delta.Function.Arguments
	}
}

func orderedToolCalls(calls map[int]*domain.ChatToolCall) []*domain.ChatToolCall {
	if len(calls) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(calls))
	for idx := range calls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	ordered := make([]*domain.ChatToolCall, 0, len(indexes))
	for _, idx := range indexes {
		ordered = append(ordered, calls[idx])
	}
	return ordered
}

func httpClient() *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &http.Client{Timeout: requestTimeout, Transport: tr}
}

func doRequestWithRetry(ctx context.Context, do func(context.Context) (*http.Response, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := do(ctx)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil || !isRetryableError(err) || attempt == maxRetries {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return nil, lastErr
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if strings.Contains(err.Error(), "retryable chat status:") {
		return true
	}
	return false
}

func validateChatAPIKey(ctx context.Context, cfg *configs.AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("閰嶇疆涓嶈兘涓虹┖")
	}

	modelName := strings.TrimSpace(cfg.AI.Model)
	if modelName == "" {
		modelName = DefaultModelForProvider(cfg.AI.Provider)
	}
	baseURL, err := ResolveChatEndpoint(cfg, modelName)
	if err != nil {
		return err
	}

	body := map[string]any{
		"model":    modelName,
		"messages": []domain.Message{{Role: "user", Content: "ping"}},
		"stream":   false,
	}
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("api key validation request marshal failed: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("api key validation request create failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RuntimeAPIKey())
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient().Do(req)
	if err != nil {
		if requestCtx.Err() != nil || isRetryableError(err) {
			return fmt.Errorf("%w: %v", ErrAPIKeyValidationSoft, err)
		}
		return fmt.Errorf("api key validation failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("%w: %v", ErrAPIKeyValidationSoft, readErr)
	}

	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrInvalidAPIKey, strings.TrimSpace(string(bodyBytes)))
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: %s %s", ErrAPIKeyValidationSoft, resp.Status, strings.TrimSpace(string(bodyBytes)))
	default:
		return fmt.Errorf("api key validation failed: %s %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
}
