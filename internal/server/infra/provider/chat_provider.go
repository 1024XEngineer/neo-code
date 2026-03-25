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
	ErrInvalidAPIKey        = errors.New("无效的 API Key")
	ErrAPIKeyValidationSoft = errors.New("API Key 校验结果不确定")
)

type ChatCompletionProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

// GetModelName 返回提供方当前模型，缺省时使用默认模型。
func (p *ChatCompletionProvider) GetModelName() string {
	if p.Model != "" {
		return p.Model
	}
	return DefaultModel()
}

type StreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// NewChatProvider 为指定模型创建已配置的聊天提供方。
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

// ValidateChatAPIKey 按当前提供方配置校验运行时 API Key。
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

// Chat 向聊天补全接口发送流式请求并返回文本分片。
func (p *ChatCompletionProvider) Chat(ctx context.Context, messages []domain.Message) (<-chan string, error) {
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

	out := make(chan string)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
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

			text, err := decodeStreamContent(data)
			if err != nil {
				emitStreamErrorMessage(ctx, out, err)
				return
			}
			if text == "" {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- text:
			}
		}
	}()

	return out, nil
}

func emitStreamErrorMessage(ctx context.Context, out chan<- string, err error) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("\n[STREAM_ERROR] %v", err)
	select {
	case <-ctx.Done():
	case out <- msg:
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

func decodeStreamContent(data string) (string, error) {
	var res StreamResponse
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		return "", fmt.Errorf("chat stream decode failed: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", nil
	}
	content := res.Choices[0].Delta.Content
	content = stripThinkingTags(content)
	return content, nil
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
		return fmt.Errorf("配置不能为空")
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
