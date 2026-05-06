package qwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "qwen provider: "

type Provider struct {
	cfg             provider.RuntimeConfig
	generateClient  *http.Client
	discoveryClient *http.Client
}

func New(cfg provider.RuntimeConfig) (*Provider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New(errorPrefix + "base url is empty")
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return nil, errors.New(errorPrefix + "api_key_env is empty")
	}
	return &Provider{
		cfg: cfg,
		generateClient: &http.Client{
			Transport: http.DefaultTransport,
		},
		discoveryClient: &http.Client{
			Timeout:   provider.DefaultSDKRequestTimeout,
			Transport: http.DefaultTransport,
		},
	}, nil
}

func (p *Provider) EstimateInputTokens(ctx context.Context, req providertypes.GenerateRequest) (providertypes.BudgetEstimate, error) {
	payload, err := chatcompletions.BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	tokens, err := provider.EstimateSerializedPayloadTokens(payload)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	return providertypes.BudgetEstimate{
		EstimatedInputTokens: tokens,
		EstimateSource:       provider.EstimateSourceLocal,
		GatePolicy:           provider.EstimateGateAdvisory,
	}, nil
}

func (p *Provider) DiscoverModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	requestCfg, err := openaicompat.RequestConfigFromRuntime(p.cfg)
	if err != nil {
		return nil, err
	}
	return openaicompat.DiscoverModelDescriptors(ctx, p.discoveryClient, requestCfg)
}

func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	payload, err := chatcompletions.BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return err
	}
	tc := req.ThinkingConfig

	return provider.RunGenerateWithRetry(ctx, p.cfg, events, func(
		attemptCtx context.Context,
		attemptEvents chan<- providertypes.StreamEvent,
	) error {
		return p.generateOnce(attemptCtx, payload, tc, attemptEvents)
	})
}

func (p *Provider) generateOnce(ctx context.Context, payload chatcompletions.Request, tc *providertypes.ThinkingConfig, events chan<- providertypes.StreamEvent) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%smarshal request: %w", errorPrefix, err)
	}
	if tc != nil {
		body, err = injectQwenParams(body, *tc)
		if err != nil {
			return fmt.Errorf("%sinject params: %w", errorPrefix, err)
		}
	}

	apiKey, err := p.cfg.ResolveAPIKeyValue()
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%screate request: %w", errorPrefix, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.generateClient.Do(req)
	if err != nil {
		return fmt.Errorf("%ssend request: %w", errorPrefix, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return provider.WrapIfThinkingNotSupported(chatcompletions.ParseError(resp))
	}

	return chatcompletions.ConsumeStream(ctx, resp.Body, events)
}

// injectQwenParams 注入 Qwen 特有的 enable_thinking 平级布尔参数及推荐采样参数。
func injectQwenParams(body []byte, tc providertypes.ThinkingConfig) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	if tc.Enabled {
		raw["enable_thinking"] = true
		// thinking 模式推荐采样参数
		if _, ok := raw["temperature"]; !ok {
			raw["temperature"] = 0.6
		}
		if _, ok := raw["top_p"]; !ok {
			raw["top_p"] = 0.95
		}
	} else {
		raw["enable_thinking"] = false
		if _, ok := raw["temperature"]; !ok {
			raw["temperature"] = 0.7
		}
		if _, ok := raw["top_p"]; !ok {
			raw["top_p"] = 0.8
		}
	}
	return json.Marshal(raw)
}
