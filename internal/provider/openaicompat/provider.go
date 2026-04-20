package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"neo-code/internal/provider"
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
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New(errorPrefix + "api key is empty")
	}
	return nil
}

// Provider 封装 OpenAI-compatible 协议的运行时配置与 HTTP 客户端。
type Provider struct {
	cfg    provider.RuntimeConfig
	client *http.Client
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
		return p.generateSDKChatCompletions(ctx, req, events)
	case executionModeResponses:
		return p.generateSDKResponses(ctx, req, events)
	default:
		return provider.NewDiscoveryConfigError(
			fmt.Sprintf("openaicompat provider: driver %q resolved unsupported execution mode %q", p.cfg.Driver, mode),
		)
	}
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
