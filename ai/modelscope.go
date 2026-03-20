package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"neo-code/config"
)

// SupportedModels 为所有允许的 ModelScope 模型列表。
var SupportedModels = []string{
	"Qwen/Qwen3-Coder-480B-A35B-Instruct",
	"ZhipuAI/GLM-5",
	"moonshotai/Kimi-K2.5",
	"deepseek-ai/DeepSeek-R1-0528",
}

// DefaultModel 返回默认的模型，当前从配置文件中获取。
func DefaultModel() string {
	defaultModel := config.GetDefaultChatModel()
	if defaultModel != "" {
		return defaultModel
	}

	// 回退到原来的逻辑
	if len(SupportedModels) == 0 {
		return "Qwen/Qwen3-Coder-480B-A35B-Instruct" // 确保有一个合理的默认值
	}
	return SupportedModels[0]
}

// IsSupportedModel 检查模型是否在允许列表中。
func IsSupportedModel(model string) bool {
	for _, m := range SupportedModels {
		if m == model {
			return true
		}
	}
	return false
}

// ModelScopeProvider 是 ModelScope 模型的实现
type ModelScopeProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

type ModelScopeEmbeddingProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

// GetModelName 返回模型名称
func (p *ModelScopeProvider) GetModelName() string {
	if p.Model != "" {
		return p.Model
	}
	// 如果模型为空，返回默认模型
	return DefaultModel()
}

// StreamResponse 定义 ModelScope 模型的流式返回结构
type StreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type EmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Embeddings []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"embeddings"`
	Output struct {
		Embeddings    [][]float64 `json:"embeddings"`
		TextEmbedding []float64   `json:"text_embedding"`
	} `json:"output"`
	Embedding []float64 `json:"embedding"`
}

// Chat 实现了 ModelScope 模型的流式对话
func (p *ModelScopeProvider) Chat(ctx context.Context, messages []Message) (<-chan string, error) {
	out := make(chan string)

	// 获取模型对应的URL
	baseURL := p.BaseURL
	if configURL, exists := config.GetChatModelURL(p.Model); exists && configURL != "" {
		baseURL = configURL
	}

	go func() {
		defer close(out)
		// 这里调用 API 接口
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

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Println("请求创建错误:", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("请求发送错误:", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("请求失败: %s %s\n", resp.Status, strings.TrimSpace(string(body)))
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("读取错误:", err)
				return
			}
			line = strings.TrimSpace(line)
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				break
			}
			var res StreamResponse
			if err := json.Unmarshal([]byte(data), &res); err != nil {
				fmt.Println("JSON 解码错误:", err)
				continue
			}
			if len(res.Choices) > 0 {
				select {
				case <-ctx.Done():
					return
				case out <- res.Choices[0].Delta.Content:
				}
			}
		}

	}()

	return out, nil
}

func (p *ModelScopeEmbeddingProvider) GetModelName() string {
	if p.Model != "" {
		return p.Model
	}
	// 如果模型为空，返回默认嵌入模型
	return config.GetDefaultEmbeddingModel()
}

func (p *ModelScopeEmbeddingProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	// 获取模型对应的URL
	baseURL := p.BaseURL
	if configURL, exists := config.GetEmbeddingModelURL(p.Model); exists && configURL != "" {
		baseURL = configURL
	}

	body := map[string]any{
		"model": p.Model,
		"input": text,
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embedding request marshal failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("embedding request create failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding response read failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("embedding request failed: %s %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
	}

	var res EmbeddingResponse
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("embedding response decode failed: %w", err)
	}

	switch {
	case len(res.Data) > 0 && len(res.Data[0].Embedding) > 0:
		return res.Data[0].Embedding, nil
	case len(res.Embeddings) > 0 && len(res.Embeddings[0].Embedding) > 0:
		return res.Embeddings[0].Embedding, nil
	case len(res.Output.Embeddings) > 0 && len(res.Output.Embeddings[0]) > 0:
		return res.Output.Embeddings[0], nil
	case len(res.Output.TextEmbedding) > 0:
		return res.Output.TextEmbedding, nil
	case len(res.Embedding) > 0:
		return res.Embedding, nil
	default:
		return nil, fmt.Errorf("embedding response missing vector data")
	}
}
