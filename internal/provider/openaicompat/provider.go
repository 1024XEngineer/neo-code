package openaicompat

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	"neo-code/internal/provider/openaicompat/shared"
	providertypes "neo-code/internal/provider/types"
)

// Provider 封装 OpenAI 兼容 API 的客户端配置和 HTTP 连接。
type Provider struct {
	cfg    provider.RuntimeConfig
	client *http.Client
}

const defaultAPIStyleChatCompletions = "chat_completions"

// buildOptions 控制构造行为，用于注入自定义 Transport 等选项。
type buildOptions struct {
	transport http.RoundTripper
}

// buildOption 是 New() 的函数式配置选项。
type buildOption func(*buildOptions)

// withTransport 注入自定义 HTTP Transport（如 RetryTransport）。
func withTransport(rt http.RoundTripper) buildOption {
	return func(o *buildOptions) {
		o.transport = rt
	}
}

// New 创建 OpenAI provider 实例。cfg 必须包含有效的接入地址和 API Key。
func New(cfg provider.RuntimeConfig, opts ...buildOption) (*Provider, error) {
	if err := shared.ValidateRuntimeConfig(cfg); err != nil {
		return nil, err
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

// DiscoverModels 通过 /models 端点查询可用模型列表。
func (p *Provider) DiscoverModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	rawModels, err := p.fetchModels(ctx)
	if err != nil {
		return nil, err
	}

	descriptors := make([]providertypes.ModelDescriptor, 0, len(rawModels))
	for _, raw := range rawModels {
		descriptor, ok := providertypes.DescriptorFromRawModel(raw)
		if !ok {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return providertypes.MergeModelDescriptors(descriptors), nil
}

// Generate 发起 SSE 流式生成请求。
// 流中途断连或协议错误时直接返回错误，由上层调用方决定重试策略。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	apiStyle := normalizedAPIStyle(p.cfg.APIStyle)

	switch apiStyle {
	case defaultAPIStyleChatCompletions:
		impl, err := chatcompletions.New(p.cfg, p.client)
		if err != nil {
			return err
		}
		return impl.Generate(ctx, req, events)
	case "responses":
		return fmt.Errorf("openaicompat provider: api_style %q is not supported yet", apiStyle)
	default:
		return fmt.Errorf("openaicompat provider: unsupported api_style %q", apiStyle)
	}
}

// normalizedAPIStyle 统一规范化 openaicompat 的 api_style，并为空值回退到 chat_completions。
func normalizedAPIStyle(apiStyle string) string {
	normalized := provider.NormalizeProviderAPIStyle(apiStyle)
	if normalized == "" {
		return defaultAPIStyleChatCompletions
	}
	return normalized
}
