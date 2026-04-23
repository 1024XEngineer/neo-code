package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	"neo-code/internal/provider/openaicompat/responses"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "openaicompat provider: "

const (
	chatEndpointPathCompletions = "/chat/completions"
	chatEndpointPathResponses   = "/responses"
	executionModeCompletions    = provider.ChatAPIModeChatCompletions
	executionModeResponses      = provider.ChatAPIModeResponses
)

// validateRuntimeConfig 校验 OpenAI-compatible 运行时最小配置，避免请求阶段才暴露空配置错误。
func validateRuntimeConfig(cfg provider.RuntimeConfig) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New(errorPrefix + "base url is empty")
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return errors.New(errorPrefix + "api_key_env is empty")
	}
	return nil
}

// Provider 封装 OpenAI-compatible 协议的运行时配置与 HTTP 客户端。
type Provider struct {
	cfg    provider.RuntimeConfig
	client *http.Client

	mu       sync.Mutex
	prepared *preparedRequest
}

type preparedRequest struct {
	mode      string
	signature string
	payload   any
}

// EstimateInputTokens 基于 OpenAI-compatible 最终请求结构做本地输入 token 估算。
func (p *Provider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	mode, err := resolveExecutionMode(p.cfg)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}

	var tokens int
	switch mode {
	case executionModeCompletions:
		payload, buildErr := chatcompletions.BuildRequest(ctx, p.cfg, req)
		if buildErr != nil {
			return providertypes.BudgetEstimate{}, buildErr
		}
		tokens, err = provider.EstimateSerializedPayloadTokens(payload)
		if err == nil {
			p.storePreparedRequest(mode, provider.BuildGenerateRequestSignature(req), payload)
		}
	case executionModeResponses:
		payload, buildErr := responses.BuildRequest(ctx, p.cfg, req)
		if buildErr != nil {
			return providertypes.BudgetEstimate{}, buildErr
		}
		tokens, err = provider.EstimateSerializedPayloadTokens(payload)
		if err == nil {
			p.storePreparedRequest(mode, provider.BuildGenerateRequestSignature(req), payload)
		}
	default:
		return providertypes.BudgetEstimate{}, provider.NewDiscoveryConfigError(
			fmt.Sprintf("openaicompat provider: driver %q resolved unsupported execution mode %q", p.cfg.Driver, mode),
		)
	}
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	return providertypes.BudgetEstimate{
		EstimatedInputTokens: tokens,
		EstimateSource:       provider.EstimateSourceLocal,
		GatePolicy:           provider.EstimateGateAdvisory,
	}, nil
}

// buildOptions 控制 provider 构建时的可选注入项。
type buildOptions struct {
	transport http.RoundTripper
}

// buildOption 是 New 的函数式配置项。
type buildOption func(*buildOptions)

// defaultRetryTransport 返回 OpenAI-compatible 默认使用的 HTTP Transport。
func defaultRetryTransport() http.RoundTripper {
	return http.DefaultTransport
}

// withTransport 注入自定义 HTTP Transport。
func withTransport(rt http.RoundTripper) buildOption {
	return func(o *buildOptions) {
		o.transport = rt
	}
}

// New 创建 OpenAI-compatible provider 实例。
func New(cfg provider.RuntimeConfig, opts ...buildOption) (*Provider, error) {
	if err := validateRuntimeConfig(cfg); err != nil {
		return nil, err
	}

	o := &buildOptions{
		transport: defaultRetryTransport(),
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

// DiscoverModels 通过统一 discovery/http 入口发现可用模型。
func (p *Provider) DiscoverModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	requestCfg, err := RequestConfigFromRuntime(p.cfg)
	if err != nil {
		return nil, err
	}
	return DiscoverModelDescriptors(ctx, p.client, requestCfg)
}

// Generate 发起流式生成请求。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	mode, err := resolveExecutionMode(p.cfg)
	if err != nil {
		return err
	}

	switch mode {
	case executionModeCompletions:
		signature := provider.BuildGenerateRequestSignature(req)
		if payload, ok := p.takePreparedChatCompletionsRequest(mode, signature); ok {
			return p.generateSDKChatCompletions(ctx, payload, events)
		}
		payload, buildErr := chatcompletions.BuildRequest(ctx, p.cfg, req)
		if buildErr != nil {
			return buildErr
		}
		return p.generateSDKChatCompletions(ctx, payload, events)
	case executionModeResponses:
		signature := provider.BuildGenerateRequestSignature(req)
		if payload, ok := p.takePreparedResponsesRequest(mode, signature); ok {
			return p.generateSDKResponses(ctx, payload, events)
		}
		payload, buildErr := responses.BuildRequest(ctx, p.cfg, req)
		if buildErr != nil {
			return buildErr
		}
		return p.generateSDKResponses(ctx, payload, events)
	default:
		return provider.NewDiscoveryConfigError(
			fmt.Sprintf("openaicompat provider: driver %q resolved unsupported execution mode %q", p.cfg.Driver, mode),
		)
	}
}

// storePreparedRequest 缓存估算阶段已构建请求，供同轮发送复用以避免重复构建。
func (p *Provider) storePreparedRequest(mode string, signature string, payload any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prepared = &preparedRequest{
		mode:      mode,
		signature: strings.TrimSpace(signature),
		payload:   payload,
	}
}

// takePreparedChatCompletionsRequest 读取并消费 chat/completions 预构建请求，仅在签名匹配时命中。
func (p *Provider) takePreparedChatCompletionsRequest(mode string, signature string) (chatcompletions.Request, bool) {
	raw, ok := p.takePreparedRequest(mode, signature)
	if !ok {
		return chatcompletions.Request{}, false
	}
	payload, ok := raw.(chatcompletions.Request)
	if !ok {
		return chatcompletions.Request{}, false
	}
	return payload, true
}

// takePreparedResponsesRequest 读取并消费 responses 预构建请求，仅在签名匹配时命中。
func (p *Provider) takePreparedResponsesRequest(mode string, signature string) (responses.Request, bool) {
	raw, ok := p.takePreparedRequest(mode, signature)
	if !ok {
		return responses.Request{}, false
	}
	payload, ok := raw.(responses.Request)
	if !ok {
		return responses.Request{}, false
	}
	return payload, true
}

// takePreparedRequest 读取并消费缓存请求，避免跨请求误复用。
func (p *Provider) takePreparedRequest(mode string, signature string) (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepared == nil {
		return nil, false
	}
	current := p.prepared
	p.prepared = nil
	if current.mode != mode {
		return nil, false
	}
	if strings.TrimSpace(signature) == "" || current.signature != strings.TrimSpace(signature) {
		return nil, false
	}
	return current.payload, true
}

// resolveExecutionMode 解析当前配置对应的 OpenAI-compatible 执行模式。
func resolveExecutionMode(cfg provider.RuntimeConfig) (string, error) {
	if provider.NormalizeProviderDriver(cfg.Driver) != DriverName {
		return "", provider.NewDiscoveryConfigError(
			fmt.Sprintf("openaicompat provider: driver %q is unsupported", cfg.Driver),
		)
	}
	explicitMode, err := provider.NormalizeProviderChatAPIMode(cfg.ChatAPIMode)
	if err != nil {
		return "", provider.NewDiscoveryConfigError(err.Error())
	}
	if explicitMode != "" {
		return explicitMode, nil
	}

	normalizedPath, err := provider.NormalizeProviderChatEndpointPath(cfg.ChatEndpointPath)
	if err != nil {
		return "", provider.NewDiscoveryConfigError(err.Error())
	}
	trimmedPath := strings.Trim(strings.ToLower(strings.TrimSpace(normalizedPath)), "/")
	switch {
	case trimmedPath == "chat/completions", strings.HasSuffix(trimmedPath, "/chat/completions"):
		return executionModeCompletions, nil
	case trimmedPath == "responses", strings.HasSuffix(trimmedPath, "/responses"):
		return executionModeResponses, nil
	case normalizedPath == "", normalizedPath == "/":
		return provider.DefaultProviderChatAPIMode(), nil
	default:
		return "", provider.NewDiscoveryConfigError(
			fmt.Sprintf("openaicompat provider: unsupported chat endpoint path %q", normalizedPath),
		)
	}
}
