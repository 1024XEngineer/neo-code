package runtime

import (
	"context"
	"errors"
	"strings"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
)

type compactSummaryGenerator struct {
	providerFactory ProviderFactory
	providerConfig  config.ResolvedProviderConfig
	model           string
}

func newCompactSummaryGenerator(
	providerFactory ProviderFactory,
	providerCfg config.ResolvedProviderConfig,
	model string,
) contextcompact.SummaryGenerator {
	return &compactSummaryGenerator{
		providerFactory: providerFactory,
		providerConfig:  providerCfg,
		model:           strings.TrimSpace(model),
	}
}

func (g *compactSummaryGenerator) Generate(ctx context.Context, input contextcompact.SummaryInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if g.providerFactory == nil {
		return "", errors.New("runtime: compact summary generator provider factory is nil")
	}
	if strings.TrimSpace(g.providerConfig.Driver) == "" ||
		strings.TrimSpace(g.providerConfig.BaseURL) == "" ||
		strings.TrimSpace(g.providerConfig.APIKey) == "" {
		return "", errors.New("runtime: compact summary generator provider config is incomplete")
	}

	prompt := agentcontext.BuildCompactPrompt(agentcontext.CompactPromptInput{
		Mode:                  string(input.Mode),
		ManualStrategy:        input.Config.ManualStrategy,
		ManualKeepRecentSpans: input.Config.ManualKeepRecentSpans,
		RemovedSpans:          input.RemovedSpans,
		MaxSummaryChars:       input.Config.MaxSummaryChars,
		ArchivedMessages:      input.ArchivedMessages,
		RetainedMessages:      input.RetainedMessages,
	})

	modelProvider, err := g.providerFactory.Build(ctx, g.providerConfig)
	if err != nil {
		return "", err
	}
	resp, err := modelProvider.Chat(ctx, provider.ChatRequest{
		Model:        g.model,
		SystemPrompt: prompt.SystemPrompt,
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: prompt.UserPrompt,
		}},
	}, nil)
	if err != nil {
		return "", err
	}
	if len(resp.Message.ToolCalls) > 0 {
		return "", errors.New("runtime: compact summary response must not contain tool calls")
	}

	summary := strings.TrimSpace(resp.Message.Content)
	if summary == "" {
		return "", errors.New("runtime: compact summary response is empty")
	}
	return summary, nil
}
