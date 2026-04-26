package anthropic

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

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
	cfg provider.RuntimeConfig

	mu       sync.Mutex
	prepared *preparedRequest
}

type preparedRequest struct {
	signature string
	params    anthropic.MessageNewParams
}

// EstimateInputTokens 基于 Anthropic 最终请求结构做本地输入 token 估算。
func (p *Provider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	params, err := BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	tokens, err := provider.EstimateSerializedPayloadTokens(params)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	p.storePreparedRequest(provider.BuildGenerateRequestSignature(req), params)
	return providertypes.BudgetEstimate{
		EstimatedInputTokens: tokens,
		EstimateSource:       provider.EstimateSourceLocal,
		GatePolicy:           provider.EstimateGateAdvisory,
	}, nil
}

// New 创建 Anthropic provider 实例。
func New(cfg provider.RuntimeConfig) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return nil, errors.New(errorPrefix + "api_key_env is empty")
	}
	return &Provider{cfg: cfg}, nil
}

// Generate 发起 Anthropic 流式请求，并将重试与超时语义收敛到 provider 公共 runner。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	params, ok := p.takePreparedRequest(provider.BuildGenerateRequestSignature(req))
	if !ok {
		var err error
		params, err = BuildRequest(ctx, p.cfg, req)
		if err != nil {
			return err
		}
	}

	return provider.RunGenerateWithRetry(ctx, p.cfg, events, func(
		attemptCtx context.Context,
		attemptEvents chan<- providertypes.StreamEvent,
	) error {
		return p.generateOnce(attemptCtx, params, attemptEvents)
	})
}

// generateOnce 执行一次 Anthropic 流式尝试，并将 typed stream 转为统一事件。
func (p *Provider) generateOnce(
	ctx context.Context,
	params anthropic.MessageNewParams,
	events chan<- providertypes.StreamEvent,
) error {
	client, err := newGenerateSDKClient(p.cfg)
	if err != nil {
		return err
	}
	streamReader := client.Messages.NewStreaming(ctx, params)
	defer func() { _ = streamReader.Close() }()

	var (
		finishReason string
		usage        providertypes.Usage
		hasChunk     bool
		toolCallSeq  int
	)
	toolCalls := make(map[int]toolCallState)

	for streamReader.Next() {
		hasChunk = true
		event := streamReader.Current()
		switch variant := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			if variant.Message.Usage.JSON.InputTokens.Valid() {
				usage.InputTokens = int(variant.Message.Usage.InputTokens)
				usage.InputObserved = true
			}
			if variant.Message.Usage.JSON.OutputTokens.Valid() {
				usage.OutputTokens = int(variant.Message.Usage.OutputTokens)
				usage.OutputObserved = true
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
			if variant.Usage.JSON.OutputTokens.Valid() {
				usage.OutputTokens = int(variant.Usage.OutputTokens)
				usage.OutputObserved = true
			}
			if variant.Usage.JSON.InputTokens.Valid() {
				usage.InputTokens = int(variant.Usage.InputTokens)
				usage.InputObserved = true
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
	if !hasChunk {
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
	if usage.TotalTokens <= 0 && (usage.InputObserved || usage.OutputObserved) {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if !usage.InputObserved && !usage.OutputObserved {
		return provider.EmitMessageDone(ctx, events, finishReason, nil)
	}
	return provider.EmitMessageDone(ctx, events, finishReason, &usage)
}

// storePreparedRequest 缓存估算阶段已构建的 Anthropic 请求，供同轮发送复用。
func (p *Provider) storePreparedRequest(signature string, params anthropic.MessageNewParams) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prepared = &preparedRequest{
		signature: strings.TrimSpace(signature),
		params:    params,
	}
}

// takePreparedRequest 读取并消费匹配签名的预构建请求，避免跨请求误复用。
func (p *Provider) takePreparedRequest(signature string) (anthropic.MessageNewParams, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepared == nil {
		return anthropic.MessageNewParams{}, false
	}
	current := p.prepared
	p.prepared = nil
	if strings.TrimSpace(signature) == "" || current.signature != strings.TrimSpace(signature) {
		return anthropic.MessageNewParams{}, false
	}
	return current.params, true
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
