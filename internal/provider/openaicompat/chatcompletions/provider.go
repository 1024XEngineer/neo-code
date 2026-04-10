package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/shared"
	providertypes "neo-code/internal/provider/types"
)

// Provider 封装 Chat Completions 端点的请求组装、发送与流式响应解析。
type Provider struct {
	cfg    provider.RuntimeConfig
	client *http.Client
}

// New 基于共享运行时配置与 HTTP client 创建 Chat Completions provider。
func New(cfg provider.RuntimeConfig, client *http.Client) (*Provider, error) {
	if err := shared.ValidateRuntimeConfig(cfg); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("openai provider: client is nil")
	}

	return &Provider{
		cfg:    cfg,
		client: client,
	}, nil
}

// Generate 发起 SSE 流式生成请求。
// 流中途断连或协议错误时直接返回错误，由上层调用方决定重试策略。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	payload, err := BuildRequest(p.cfg, req)
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("openai provider: marshal request: %w", err)
	}

	endpoint := shared.Endpoint(p.cfg.BaseURL, "/chat/completions")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openai provider: build request: %w", err)
	}
	shared.SetBearerAuthorization(httpReq.Header, p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai provider: send request: %w", err)
	}
	defer func(body io.ReadCloser) {
		if closeErr := body.Close(); closeErr != nil {
			log.Printf("openai provider: close response body: %v", closeErr)
		}
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return ParseError(resp)
	}

	return p.ConsumeStream(ctx, resp.Body, events)
}
