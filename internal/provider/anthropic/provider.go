package anthropic

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "anthropic provider: "

type toolCallState struct {
	ID       string
	Name     string
	SawStart bool
	SawDelta bool
}

// Provider 封装 Anthropic messages 协议的请求发送与流式解析。
type Provider struct {
	cfg    provider.RuntimeConfig
	client anthropic.Client
}

// New 创建 Anthropic provider 实例，并初始化官方 SDK 客户端。
func New(cfg provider.RuntimeConfig) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New(errorPrefix + "api key is empty")
	}

	client := newSDKClient(cfg)

	return &Provider{
		cfg:    cfg,
		client: client,
	}, nil
}

// Generate 发起 Anthropic 流式请求，并将 typed stream 转为统一事件。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	params, err := BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return err
	}

	streamReader := p.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = streamReader.Close() }()

	var (
		finishReason string
		usage        providertypes.Usage
		hasPayload   bool
		toolCallSeq  int
	)
	toolCalls := make(map[int]toolCallState)

	for streamReader.Next() {
		hasPayload = true
		event := streamReader.Current()
		switch variant := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			if variant.Message.Usage.InputTokens > 0 {
				usage.InputTokens = int(variant.Message.Usage.InputTokens)
			}
			if variant.Message.Usage.OutputTokens > 0 {
				usage.OutputTokens = int(variant.Message.Usage.OutputTokens)
			}
		case anthropic.ContentBlockStartEvent:
			switch block := variant.ContentBlock.AsAny().(type) {
			case anthropic.TextBlock:
				if strings.TrimSpace(block.Text) != "" {
					if emitErr := provider.EmitTextDelta(ctx, events, block.Text); emitErr != nil {
						return emitErr
					}
				}
			case anthropic.ToolUseBlock:
				index := int(variant.Index)
				state := toolCalls[index]
				if id := strings.TrimSpace(block.ID); id != "" {
					state.ID = id
				}
				if name := strings.TrimSpace(block.Name); name != "" {
					state.Name = name
				}
				if state.ID == "" {
					toolCallSeq++
					state.ID = "anthropic-call-" + strconv.Itoa(toolCallSeq)
				}

				emitStart := !state.SawStart
				state.SawStart = true
				toolCalls[index] = state

				if emitStart {
					if emitErr := provider.EmitToolCallStart(ctx, events, index, state.ID, state.Name); emitErr != nil {
						return emitErr
					}
				}
				input := strings.TrimSpace(string(block.Input))
				if input != "" && !state.SawDelta {
					state.SawDelta = true
					toolCalls[index] = state
					if emitErr := provider.EmitToolCallDelta(ctx, events, index, state.ID, input); emitErr != nil {
						return emitErr
					}
				}
			}
		case anthropic.ContentBlockDeltaEvent:
			index := int(variant.Index)
			switch delta := variant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if emitErr := provider.EmitTextDelta(ctx, events, delta.Text); emitErr != nil {
					return emitErr
				}
			case anthropic.InputJSONDelta:
				state := toolCalls[index]
				if strings.TrimSpace(state.ID) == "" {
					toolCallSeq++
					state.ID = "anthropic-call-" + strconv.Itoa(toolCallSeq)
				}
				state.SawDelta = true
				toolCalls[index] = state
				if emitErr := provider.EmitToolCallDelta(ctx, events, index, state.ID, delta.PartialJSON); emitErr != nil {
					return emitErr
				}
			}
		case anthropic.MessageDeltaEvent:
			if reason := strings.TrimSpace(string(variant.Delta.StopReason)); reason != "" {
				finishReason = reason
			}
			if variant.Usage.OutputTokens > 0 {
				usage.OutputTokens = int(variant.Usage.OutputTokens)
			}
			if variant.Usage.InputTokens > 0 {
				usage.InputTokens = int(variant.Usage.InputTokens)
			}
		}
	}
	if streamErr := streamReader.Err(); streamErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if mappedErr := mapAnthropicSDKError(streamErr); mappedErr != nil {
			return mappedErr
		}
		return fmt.Errorf("%sstream receive: %w", errorPrefix, streamErr)
	}
	if !hasPayload {
		return fmt.Errorf("%w: empty anthropic stream payload", provider.ErrStreamInterrupted)
	}
	for index, state := range toolCalls {
		if state.SawDelta && !state.SawStart {
			return fmt.Errorf("%sinvalid tool_use stream at index %d: missing content_block_start", errorPrefix, index)
		}
		if state.SawStart && strings.TrimSpace(state.Name) == "" {
			return fmt.Errorf("%sinvalid tool_use stream at index %d: missing tool name", errorPrefix, index)
		}
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return provider.EmitMessageDone(ctx, events, finishReason, &usage)
}

// mapAnthropicSDKError 统一映射 SDK 错误为 provider 领域错误。
func mapAnthropicSDKError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		message := strings.TrimSpace(apiErr.RawJSON())
		if message == "" {
			message = strings.TrimSpace(err.Error())
		}
		return provider.NewProviderErrorFromStatus(apiErr.StatusCode, message)
	}
	return nil
}
