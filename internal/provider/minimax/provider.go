package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "minimax provider: "

var thinkTagRe = regexp.MustCompile(`<think>([\s\S]*?)</think>`)

type Provider struct {
	cfg            provider.RuntimeConfig
	generateClient *http.Client
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

func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	payload, err := chatcompletions.BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return err
	}

	return provider.RunGenerateWithRetry(ctx, p.cfg, events, func(
		attemptCtx context.Context,
		attemptEvents chan<- providertypes.StreamEvent,
	) error {
		return p.generateOnce(attemptCtx, payload, attemptEvents)
	})
}

func (p *Provider) generateOnce(ctx context.Context, payload chatcompletions.Request, events chan<- providertypes.StreamEvent) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%smarshal request: %w", errorPrefix, err)
	}

	// MiniMax 始终发送 reasoning_split:true 和 enable_thinking:true（ThinkingForceEnabled）
	body, err = injectMiniMaxParams(body)
	if err != nil {
		return fmt.Errorf("%sinject params: %w", errorPrefix, err)
	}

	apiKey, err := p.cfg.ResolveAPIKeyValue()
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/")
	if !strings.Contains(endpoint, "/") {
		endpoint += "/chat/completions"
	}
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

	return ConsumeMiniMaxStream(ctx, resp.Body, events)
}

func injectMiniMaxParams(body []byte) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	raw["reasoning_split"] = true
	raw["enable_thinking"] = true
	return json.Marshal(raw)
}

// ExtractThinkContent 从 MiniMax 的 <think> 标签或 reasoning_details 中提取思考文本。
func ExtractThinkContent(content string) string {
	matches := thinkTagRe.FindAllStringSubmatch(content, -1)
	var builder strings.Builder
	for _, match := range matches {
		if len(match) > 1 {
			builder.WriteString(strings.TrimSpace(match[1]))
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String())
}
