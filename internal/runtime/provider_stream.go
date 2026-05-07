package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/streaming"
)

// streamGenerateResult 统一承载一次流式生成的消息、用量与消费错误。
type streamGenerateResult struct {
	message        providertypes.Message
	inputTokens    int
	outputTokens   int
	inputObserved  bool
	outputObserved bool
	err            error
}

// generateStreamingMessage 负责执行一次基于流式事件的生成调用，并收敛最终 assistant 消息与 usage。
func generateStreamingMessage(
	ctx context.Context,
	modelProvider provider.Provider,
	req providertypes.GenerateRequest,
	hooks streaming.Hooks,
) streamGenerateResult {
	acc := streaming.NewAccumulator()
	streamEvents := make(chan providertypes.StreamEvent, 32)
	streamDone := make(chan streamGenerateResult, 1)

	go func() {
		outcome := streamGenerateResult{}
		defer func() {
			streamDone <- outcome
		}()

		userOnMessageDone := hooks.OnMessageDone
		hooksCopy := hooks
		hooksCopy.OnMessageDone = func(payload providertypes.MessageDonePayload) {
			if payload.Usage != nil {
				outcome.inputTokens = payload.Usage.InputTokens
				outcome.outputTokens = payload.Usage.OutputTokens
				outcome.inputObserved = payload.Usage.InputObserved
				outcome.outputObserved = payload.Usage.OutputObserved
			}
			if userOnMessageDone != nil {
				userOnMessageDone(payload)
			}
		}

		for event := range streamEvents {
			if err := streaming.HandleEvent(event, acc, hooksCopy); err != nil && outcome.err == nil {
				outcome.err = err
			}
		}
	}()

	generateErr := modelProvider.Generate(ctx, req, streamEvents)
	close(streamEvents)
	outcome := <-streamDone
	if outcome.err != nil {
		if generateErr != nil {
			outcome.err = fmt.Errorf("runtime: provider stream handling failed after provider error: %v: %w", generateErr, outcome.err)
		}
		return outcome
	}
	if generateErr != nil {
		outcome.err = generateErr
		return outcome
	}
	if !acc.MessageDone() {
		outcome.err = fmt.Errorf("%w: provider stream ended without message_done event", provider.ErrStreamInterrupted)
		return outcome
	}

	message, err := acc.BuildMessage()
	if err != nil {
		outcome.err = err
		return outcome
	}

	// 将累积的 thinking 文本写入 ThinkingMetadata，以备续轮回传给 provider。
	if thinking := acc.ThinkingContent(); thinking != "" {
		storeThinkingMetadata(&message, thinking)
	}

	outcome.message = message
	return outcome
}

// storeThinkingMetadata 将 thinking 文本存入消息的 ThinkingMetadata 以备续轮使用。
func storeThinkingMetadata(msg *providertypes.Message, thinking string) {
	meta, _ := json.Marshal(map[string]string{"reasoning_content": thinking})
	msg.ThinkingMetadata = json.RawMessage(meta)
}
