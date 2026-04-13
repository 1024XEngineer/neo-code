package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	"neo-code/internal/provider/streaming"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// Run 执行一次完整的 ReAct 闭环：保存用户输入、驱动模型、执行工具并发出事件。
// 已有会话会先加锁再加载/更新，确保同一会话并发 Run 不会出现状态覆盖；
// 新会话在创建后再绑定会话锁，不同会话可并行执行。
func (s *Service) Run(ctx context.Context, input UserInput) error {
	if strings.TrimSpace(input.Content) == "" {
		return errors.New("runtime: input content is empty")
	}

	runCtx, cancel := context.WithCancel(ctx)
	runToken := s.startRun(cancel)
	defer func() {
		cancel()
		s.finishRun(runToken)
	}()
	ctx = runCtx

	initialCfg := s.configManager.Get()
	sessionID := strings.TrimSpace(input.SessionID)
	releaseSessionLock := func() {}
	defer func() {
		releaseSessionLock()
	}()

	if sessionID != "" {
		sessionMu, releaseLockRef := s.acquireSessionLock(sessionID)
		sessionMu.Lock()
		releaseSessionLock = func() {
			sessionMu.Unlock()
			releaseLockRef()
		}
	}

	session, err := s.loadOrCreateSession(ctx, input.SessionID, input.Content, initialCfg.Workdir, input.Workdir)
	if err != nil {
		return s.handleRunError(ctx, input.RunID, input.SessionID, err)
	}

	if sessionID == "" {
		sessionMu, releaseLockRef := s.acquireSessionLock(session.ID)
		sessionMu.Lock()
		releaseSessionLock = func() {
			sessionMu.Unlock()
			releaseLockRef()
		}
	}

	state := newRunState(input.RunID, session)
	if err := s.appendUserMessageAndSave(ctx, &state, input.Content); err != nil {
		return s.handleRunError(ctx, state.runID, state.session.ID, err)
	}

	for turn := 0; ; turn++ {
		maxLoops := resolveMaxLoops(s.configManager.Get())
		if turn >= maxLoops {
			err := errors.New("runtime: max loop reached")
			s.emit(ctx, EventError, state.runID, state.session.ID, err.Error())
			return err
		}

		for {
			if err := ctx.Err(); err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}

			snapshot, rebuilt, err := s.prepareTurnSnapshot(ctx, &state)
			if err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}
			if rebuilt {
				continue
			}

			turnResult, err := s.callProviderWithRetry(ctx, &state, snapshot)
			if err != nil {
				if provider.IsContextTooLong(err) && !state.reactiveCompactUsed {
					state.reactiveCompactUsed = true
					_, _ = s.applyCompactForState(ctx, &state, snapshot.config, contextcompact.ModeReactive, compactErrorBestEffort)
					continue
				}
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}

			if strings.TrimSpace(turnResult.assistant.Role) == "" {
				turnResult.assistant.Role = providertypes.RoleAssistant
			}
			state.recordUsage(turnResult.inputTokens, turnResult.outputTokens)
			if err := s.appendAssistantMessageAndSave(ctx, &state, snapshot, turnResult.assistant); err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}
			s.emitTokenUsage(ctx, &state, turnResult)

			if len(turnResult.assistant.ToolCalls) == 0 {
				s.emit(ctx, EventAgentDone, state.runID, state.session.ID, turnResult.assistant)
				s.triggerMemoExtraction(state.session.Messages)
				return nil
			}
			if err := s.executeAssistantToolCalls(ctx, &state, snapshot, turnResult.assistant); err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}
			break
		}
	}
}

// prepareTurnSnapshot 基于当前会话状态冻结一轮推理所需的请求快照。
func (s *Service) prepareTurnSnapshot(ctx context.Context, state *runState) (turnSnapshot, bool, error) {
	cfg := s.configManager.Get()
	activeWorkdir := agentsession.EffectiveWorkdir(state.session.Workdir, cfg.Workdir)

	builtContext, err := s.contextBuilder.Build(ctx, agentcontext.BuildInput{
		Messages: state.session.Messages,
		Metadata: agentcontext.Metadata{
			Workdir:             activeWorkdir,
			Shell:               cfg.Shell,
			Provider:            cfg.SelectedProvider,
			Model:               cfg.CurrentModel,
			SessionInputTokens:  state.tokenInputTotal,
			SessionOutputTokens: state.tokenOutputTotal,
		},
		Compact: agentcontext.CompactOptions{
			DisableMicroCompact:  cfg.Context.Compact.MicroCompactDisabled,
			AutoCompactThreshold: autoCompactThreshold(cfg),
		},
	})
	if err != nil {
		return turnSnapshot{}, false, err
	}

	if builtContext.AutoCompactSuggested && !state.compactApplied {
		applied, err := s.applyCompactForState(ctx, state, cfg, contextcompact.ModeAuto, compactErrorBestEffort)
		if err != nil {
			return turnSnapshot{}, false, err
		}
		if applied {
			return turnSnapshot{}, true, nil
		}
	}

	toolSpecs, err := s.toolManager.ListAvailableSpecs(ctx, tools.SpecListInput{
		SessionID: state.session.ID,
	})
	if err != nil {
		return turnSnapshot{}, false, err
	}

	resolvedProvider, err := config.ResolveSelectedProvider(cfg)
	if err != nil {
		return turnSnapshot{}, false, err
	}

	model := strings.TrimSpace(cfg.CurrentModel)
	return turnSnapshot{
		config:         cfg,
		providerConfig: resolvedProvider.ToRuntimeConfig(),
		model:          model,
		workdir:        activeWorkdir,
		toolTimeout:    time.Duration(cfg.ToolTimeoutSec) * time.Second,
		request: providertypes.GenerateRequest{
			Model:        model,
			SystemPrompt: builtContext.SystemPrompt,
			Messages:     builtContext.Messages,
			Tools:        toolSpecs,
		},
	}, false, nil
}

// callProviderWithRetry 使用冻结后的 turnSnapshot 执行 provider 调用与必要重试。
func (s *Service) callProviderWithRetry(
	ctx context.Context,
	state *runState,
	snapshot turnSnapshot,
) (providerTurnResult, error) {
	var lastErr error

	for retryAttempt := 0; retryAttempt <= defaultProviderRetryMax; retryAttempt++ {
		if retryAttempt > 0 {
			wait := providerRetryBackoff(retryAttempt)
			s.emit(ctx, EventProviderRetry, state.runID, state.session.ID,
				fmt.Sprintf("retrying provider call (attempt %d/%d, wait=%.1fs)...",
					retryAttempt, defaultProviderRetryMax, wait.Seconds()))

			select {
			case <-ctx.Done():
				return providerTurnResult{}, ctx.Err()
			case <-time.After(wait):
			}
		}

		modelProvider, err := s.providerFactory.Build(ctx, snapshot.providerConfig)
		if err != nil {
			return providerTurnResult{}, err
		}

		streamOutcome := generateStreamingMessage(ctx, modelProvider, snapshot.request, streaming.Hooks{
			OnTextDelta: func(text string) {
				s.emit(ctx, EventAgentChunk, state.runID, state.session.ID, text)
			},
			OnToolCallStart: func(payload providertypes.ToolCallStartPayload) {
				s.emit(ctx, EventToolCallThinking, state.runID, state.session.ID, payload.Name)
			},
		})
		if streamOutcome.err != nil {
			lastErr = streamOutcome.err
			if !isRetryableProviderError(lastErr) {
				return providerTurnResult{}, lastErr
			}
			if ctx.Err() != nil {
				return providerTurnResult{}, ctx.Err()
			}
			continue
		}

		return providerTurnResult{
			assistant:    streamOutcome.message,
			inputTokens:  streamOutcome.inputTokens,
			outputTokens: streamOutcome.outputTokens,
		}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("max retries exceeded")
	}
	return providerTurnResult{}, fmt.Errorf("runtime: max retries exhausted, last error: %w", lastErr)
}

// emitTokenUsage 在单轮 provider 调用成功后发出 token_usage 事件。
func (s *Service) emitTokenUsage(ctx context.Context, state *runState, result providerTurnResult) {
	if result.inputTokens == 0 && result.outputTokens == 0 {
		return
	}
	s.emit(ctx, EventTokenUsage, state.runID, state.session.ID, TokenUsagePayload{
		InputTokens:         result.inputTokens,
		OutputTokens:        result.outputTokens,
		SessionInputTokens:  state.tokenInputTotal,
		SessionOutputTokens: state.tokenOutputTotal,
	})
}

// applyCompactForState 在运行中执行 compact，并把结果同步回 runState。
func (s *Service) applyCompactForState(
	ctx context.Context,
	state *runState,
	cfg config.Config,
	mode contextcompact.Mode,
	errorPolicy compactErrorPolicy,
) (bool, error) {
	session, result, err := s.runCompactForSession(ctx, state.runID, state.session, cfg, mode, errorPolicy)
	if err != nil {
		return false, err
	}
	state.session = session
	if result.Applied {
		state.resetTokenTotals()
		state.compactApplied = true
		return true, nil
	}
	return false, nil
}

// resolveMaxLoops 收敛运行时最大推理轮数的默认值逻辑。
func resolveMaxLoops(cfg config.Config) int {
	if cfg.MaxLoops <= 0 {
		return defaultMaxLoops
	}
	return cfg.MaxLoops
}

// autoCompactThreshold 返回当前配置下的自动 compact 触发阈值。
func autoCompactThreshold(cfg config.Config) int {
	if cfg.Context.AutoCompact.Enabled && cfg.Context.AutoCompact.InputTokenThreshold > 0 {
		return cfg.Context.AutoCompact.InputTokenThreshold
	}
	return 0
}
