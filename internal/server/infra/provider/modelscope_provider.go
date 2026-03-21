package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go-llm-demo/config"
	"go-llm-demo/internal/server/domain"
)

const (
	requestTimeout = 30 * time.Second
	maxRetries     = 2
)

var fallbackSupportedModels = []string{
	"Qwen/Qwen3-Coder-480B-A35B-Instruct",
	"ZhipuAI/GLM-5",
	"moonshotai/Kimi-K2.5",
	"deepseek-ai/DeepSeek-R1-0528",
}

func SupportedModels() []string {
	if config.GlobalAppConfig != nil && len(config.GlobalAppConfig.Models.Chat.Models) > 0 {
		models := make([]string, 0, len(config.GlobalAppConfig.Models.Chat.Models))
		for _, model := range config.GlobalAppConfig.Models.Chat.Models {
			if strings.TrimSpace(model.Name) != "" {
				models = append(models, model.Name)
			}
		}
		if len(models) > 0 {
			return models
		}
	}
	models := make([]string, len(fallbackSupportedModels))
	copy(models, fallbackSupportedModels)
	return models
}

func DefaultModel() string {
	defaultModel := config.GetDefaultChatModel()
	if defaultModel != "" {
		return defaultModel
	}
	supported := SupportedModels()
	if len(supported) == 0 {
		return fallbackSupportedModels[0]
	}
	return supported[0]
}

func IsSupportedModel(model string) bool {
	for _, m := range SupportedModels() {
		if m == model {
			return true
		}
	}
	return false
}

type ModelScopeProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func (p *ModelScopeProvider) GetModelName() string {
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

func (p *ModelScopeProvider) Chat(ctx context.Context, messages []domain.Message) (<-chan string, error) {
	out := make(chan string)

	baseURL := p.BaseURL
	if configURL, exists := config.GetChatModelURL(p.Model); exists && configURL != "" {
		baseURL = configURL
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("chat model URL is not configured for %q", p.Model)
	}

	go func() {
		defer close(out)

		body := map[string]any{
			"model":    p.Model,
			"messages": messages,
			"stream":   true,
		}
		jsonData, err := json.Marshal(body)
		if err != nil {
			fmt.Println("JSON 编码错误:", err)
			return
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
			fmt.Println("请求发送错误:", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("请求失败：%s %s\n", resp.Status, strings.TrimSpace(string(body)))
			return
		}

		reader := bufio.NewReader(resp.Body)
		receivedContent := false
		receivedDone := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					if !receivedDone {
						if !receivedContent {
							fmt.Println("chat stream ended without content")
						} else {
							fmt.Println("chat stream ended unexpectedly before completion")
						}
					}
					return
				}
				fmt.Println("读取错误:", err)
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
				receivedDone = true
				break
			}

			text, err := decodeStreamContent(data)
			if err != nil {
				fmt.Println("JSON 解码错误:", err)
				return
			}
			if text == "" {
				continue
			}
			receivedContent = true
			select {
			case <-ctx.Done():
				return
			case out <- text:
			}
		}
	}()

	return out, nil
}

func decodeStreamContent(data string) (string, error) {
	var res StreamResponse
	if err := json.Unmarshal([]byte(data), &res); err != nil {
		return "", fmt.Errorf("chat stream decode failed: %w", err)
	}
	if len(res.Choices) == 0 {
		return "", nil
	}
	return res.Choices[0].Delta.Content, nil
}

func httpClient() *http.Client {
	return &http.Client{Timeout: requestTimeout}
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

func validateModelScopeAPIKey(ctx context.Context, cfg *config.AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	modelName := strings.TrimSpace(cfg.AI.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(cfg.Models.Chat.DefaultModel)
	}
	baseURL, ok := config.GetChatModelURLFromConfig(cfg, modelName)
	if !ok || strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("chat model URL is not configured for %q", modelName)
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
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.AI.APIKey))
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
