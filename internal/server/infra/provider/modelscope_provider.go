package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go-llm-demo/internal/server/domain"
)

var SupportedModels = []string{
	"Qwen/Qwen3-Coder-480B-A35B-Instruct",
	"ZhipuAI/GLM-5",
	"moonshotai/Kimi-K2.5",
	"deepseek-ai/DeepSeek-R1-0528",
}

func DefaultModel() string {
	if len(SupportedModels) == 0 {
		return ""
	}
	return SupportedModels[0]
}

func IsSupportedModel(model string) bool {
	for _, m := range SupportedModels {
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
	return p.Model
}

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

func (p *ModelScopeProvider) Chat(ctx context.Context, messages []domain.Message) (<-chan string, error) {
	out := make(chan string)

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

		req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL, bytes.NewBuffer(jsonData))
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
			fmt.Printf("请求失败：%s %s\n", resp.Status, strings.TrimSpace(string(body)))
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
