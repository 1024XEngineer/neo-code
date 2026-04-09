package runtime

import (
	"context"
	"errors"
	"strings"

	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

type compactSummaryGenerator struct {
	providerFactory ProviderFactory
	providerConfig  provider.RuntimeConfig
	model           string
}

func newCompactSummaryGenerator(
	providerFactory ProviderFactory,
	providerCfg provider.RuntimeConfig,
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
	if err := ensureProviderDriverCapabilities(g.providerFactory, g.providerConfig, true, false); err != nil {
		return "", err
	}

	prompt := agentcontext.BuildCompactPrompt(agentcontext.CompactPromptInput{
		Mode:                     string(input.Mode),
		ManualStrategy:           input.Config.ManualStrategy,
		ManualKeepRecentMessages: input.Config.ManualKeepRecentMessages,
		ArchivedMessageCount:     input.ArchivedMessageCount,
		MaxSummaryChars:          input.Config.MaxSummaryChars,
		ArchivedMessages:         input.ArchivedMessages,
		RetainedMessages:         input.RetainedMessages,
	})

	modelProvider, err := g.providerFactory.Build(ctx, g.providerConfig)
	if err != nil {
		return "", err
	}

	// 使用流式事件通道收集 compact 摘要响应。
	streamEvents := make(chan providertypes.StreamEvent, 32)
	streamDone := make(chan error, 1)
	acc := newStreamAccumulator()

	go func() {
		var streamErr error
		defer func() {
			streamDone <- streamErr
		}()

		for {
			select {
			case event, ok := <-streamEvents:
				if !ok {
					return
				}
				if err := handleProviderStreamEvent(event, acc, nil, nil, nil); err != nil && streamErr == nil {
					// 记录首个协议错误后继续排空事件通道，避免 provider 在后续发送时阻塞。
					streamErr = err
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	err = modelProvider.Generate(ctx, providertypes.GenerateRequest{
		Model:        g.model,
		SystemPrompt: prompt.SystemPrompt,
		Messages: []providertypes.Message{{
			Role:    providertypes.RoleUser,
			Content: prompt.UserPrompt,
		}},
	}, streamEvents)
	close(streamEvents)
	streamErr := <-streamDone

	if err != nil {
		return "", err
	}
	if streamErr != nil {
		return "", streamErr
	}

	message, err := acc.buildMessage()
	if err != nil {
		return "", err
	}
	if len(message.ToolCalls) > 0 {
		return "", errors.New("runtime: compact summary response must not contain tool calls")
	}

	summary := strings.TrimSpace(message.Content)
	if summary == "" {
		return "", errors.New("runtime: compact summary response is empty")
	}
	return summary, nil
}
