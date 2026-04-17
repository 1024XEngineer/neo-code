package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const selfHealingReminder = "System Reminder: You have made multiple consecutive attempts without making substantial progress. Please stop your current repetitive or ineffective strategy. Carefully review the previous errors, change your approach, or ask the user directly for help."
const selfHealingRepeatReminder = "System Reminder: You are repeatedly calling the same tool with the exact same arguments. This is an infinite loop. Please change your parameters, try a different tool, or ask the user for help."

// computeToolSignature 计算单轮执行的工具签名，用于循环检测。
func computeToolSignature(calls []providertypes.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, call := range calls {
		sb.WriteString(call.Name)
		sb.WriteString(":")

		// 尝试将 JSON 参数进行规范化序列化，以消除空格、换行和字段顺序带来的哈希差异
		var parsed interface{}
		if err := json.Unmarshal([]byte(call.Arguments), &parsed); err == nil {
			if canonicalBytes, err := json.Marshal(parsed); err == nil {
				sb.WriteString(string(canonicalBytes))
			} else {
				sb.WriteString(call.Arguments) // 序列化失败，降级为原始字符串
			}
		} else {
			sb.WriteString(call.Arguments) // 解析失败，降级为原始字符串
		}

		sb.WriteString(";")
	}
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:])
}

// Run 执行一次完整的 ReAct 闭环：保存用户输入、驱动模型、执行工具并发出事件。
// 已有会话会先加锁再加载/更新，确保同一会话并发 Run 不会出现状态覆盖；
// 新会话在创建后再绑定会话锁，不同会话可并行执行。
// 当前实现不再设置内部轮数上限，因此 Run 仅在拿到最终 assistant 回复、遇到错误或收到外部取消时结束。
// 这也意味着同一 session 的锁会覆盖整个运行周期，调用方需要依赖模型终止条件或取消机制兜底。
func (s *Service) Run(ctx context.Context, input UserInput) (err error) {
	var statePtr *runState

	runCtx, cancel := context.WithCancel(ctx)
	runToken := s.startRun(cancel)
	defer func() {
		cancel()
		s.finishRun(runToken)
	}()
	defer func() {
		s.emitRunTermination(runCtx, input, statePtr, err)
	}()
	ctx = runCtx

	if err = validateUserInputParts(input.Parts); err != nil {
		return err
	}

	initialCfg := s.configManager.Get()
	sessionID := strings.TrimSpace(input.SessionID)
	releaseSessionLock := s.bindSessionLock(sessionID)
	defer func() {
		releaseSessionLock()
	}()

	sessionTitle := sessionTitleFromParts(input.Parts)
	session, err := s.loadOrCreateSession(ctx, input.SessionID, sessionTitle, initialCfg.Workdir, input.Workdir)
	if err != nil {
		return s.handleRunError(ctx, input.RunID, input.SessionID, err)
	}

	if sessionID == "" {
		releaseSessionLock = s.bindSessionLock(session.ID)
	}

	state := newRunState(input.RunID, session)
	state.taskID = strings.TrimSpace(input.TaskID)
	state.agentID = strings.TrimSpace(input.AgentID)
	if input.CapabilityToken != nil {
		token := input.CapabilityToken.Normalize()
		state.capabilityToken = &token
	}
	statePtr = &state
	if err := s.appendUserMessageAndSave(ctx, &state, input.Parts); err != nil {
		return s.handleRunError(ctx, state.runID, state.session.ID, err)
	}

	for turn := 0; ; turn++ {
		state.turn = turn
		s.transitionRunPhase(ctx, &state, controlplane.PhasePlan)

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
				if provider.IsContextTooLong(err) && state.reactiveCompactAttempts < maxReactiveCompactAttempts {
					state.reactiveCompactAttempts++
					degradedCfg := snapshot.config
					degradedCfg.Context.Compact.ManualKeepRecentMessages = degradeKeepRecentMessages(
						snapshot.config.Context.Compact.ManualKeepRecentMessages,
						state.reactiveCompactAttempts,
					)
					_, _ = s.applyCompactForState(ctx, &state, degradedCfg, contextcompact.ModeReactive, compactErrorBestEffort)
					continue
				}
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}

			if strings.TrimSpace(turnResult.assistant.Role) == "" {
				turnResult.assistant.Role = providertypes.RoleAssistant
			}
			if err := s.appendAssistantMessageAndSave(
				ctx,
				&state,
				snapshot,
				turnResult.assistant,
				turnResult.inputTokens,
				turnResult.outputTokens,
			); err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}
			s.emitTokenUsage(ctx, &state, turnResult)

			if len(turnResult.assistant.ToolCalls) == 0 {
				s.emitRunScoped(ctx, EventAgentDone, &state, turnResult.assistant)
				s.triggerMemoExtraction(state.session.ID, state.session.Messages, state.rememberedThisRun)
				return nil
			}
			s.transitionRunPhase(ctx, &state, controlplane.PhaseExecute)
			if err := s.executeAssistantToolCalls(ctx, &state, snapshot, turnResult.assistant); err != nil {
				return s.handleRunError(ctx, state.runID, state.session.ID, err)
			}
			s.transitionRunPhase(ctx, &state, controlplane.PhaseVerify)

			var evidence []controlplane.ProgressEvidenceRecord
			toolCallCount := len(turnResult.assistant.ToolCalls)
			currentSignature := computeToolSignature(turnResult.assistant.ToolCalls)

			state.mu.Lock()
			if len(state.session.Messages) >= toolCallCount {
				for i := len(state.session.Messages) - toolCallCount; i < len(state.session.Messages); i++ {
					if msg := state.session.Messages[i]; msg.Role == providertypes.RoleTool && !msg.IsError {
						evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceNewInfoNonDup})
						break
					}
				}
			}

			state.progress = controlplane.ApplyProgressEvidence(state.progress, evidence, currentSignature)
			streak := state.progress.LastScore.NoProgressStreak
			repeatStreak := state.progress.LastScore.RepeatCycleStreak
			currentScore := state.progress.LastScore
			state.mu.Unlock()

			s.emitRunScoped(ctx, EventProgressEvaluated, &state, ProgressEvaluatedPayload{Score: currentScore})

			repeatLimit := snapshot.config.Runtime.MaxRepeatCycleStreak
			if repeatLimit <= 0 {
				repeatLimit = config.DefaultMaxRepeatCycleStreak
			}

			if repeatStreak >= repeatLimit {
				err = ErrRepeatCycleLimit
				return err
			}

			limit := snapshot.noProgressStreakLimit
			if streak >= limit {
				err = ErrNoProgressStreakLimit
				return err
			}

			break
		}
	}
}

// prepareTurnSnapshot 基于当前会话状态冻结一轮推理所需的请求快照。
func (s *Service) prepareTurnSnapshot(ctx context.Context, state *runState) (turnSnapshot, bool, error) {
	cfg := s.configManager.Get()
	activeWorkdir := agentsession.EffectiveWorkdir(state.session.Workdir, cfg.Workdir)
	activeSkills, err := s.resolveActiveSkills(ctx, state)
	if err != nil {
		return turnSnapshot{}, false, err
	}

	builtContext, err := s.contextBuilder.Build(ctx, agentcontext.BuildInput{
		Messages:     state.session.Messages,
		TaskState:    state.session.TaskState,
		Todos:        cloneTodosForPersistence(state.session.Todos),
		ActiveSkills: activeSkills,
		Metadata: agentcontext.Metadata{
			Workdir:             activeWorkdir,
			Shell:               cfg.Shell,
			Provider:            cfg.SelectedProvider,
			Model:               cfg.CurrentModel,
			SessionInputTokens:  state.session.TokenInputTotal,
			SessionOutputTokens: state.session.TokenOutputTotal,
		},
		Compact: agentcontext.CompactOptions{
			DisableMicroCompact:           cfg.Context.Compact.MicroCompactDisabled,
			AutoCompactThreshold:          s.autoCompactThresholdForState(ctx, cfg, state),
			MicroCompactRetainedToolSpans: cfg.Context.Compact.MicroCompactRetainedToolSpans,
			ReadTimeMaxMessageSpans:       cfg.Context.Compact.ReadTimeMaxMessageSpans,
		},
	})
	if err != nil {
		return turnSnapshot{}, false, err
	}
	if strings.Contains(builtContext.SystemPrompt, "## Todo State") {
		s.emitRunScoped(ctx, EventTodoSummaryInjected, state, TodoEventPayload{})
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

	state.mu.Lock()
	streak := state.progress.LastScore.NoProgressStreak
	repeatStreak := state.progress.LastScore.RepeatCycleStreak
	state.mu.Unlock()

	limit := resolveNoProgressStreakLimit(cfg.Runtime)
	repeatLimit := resolveRepeatCycleStreakLimit(cfg.Runtime)
	systemPrompt, repeatInjected := withSelfHealingRepeatReminder(builtContext.SystemPrompt, repeatStreak, repeatLimit)
	if !repeatInjected {
		systemPrompt = withSelfHealingReminder(systemPrompt, streak, limit)
	}

	model := strings.TrimSpace(cfg.CurrentModel)
	return turnSnapshot{
		config:                cfg,
		providerConfig:        resolvedProvider.ToRuntimeConfig(),
		model:                 model,
		workdir:               activeWorkdir,
		toolTimeout:           time.Duration(cfg.ToolTimeoutSec) * time.Second,
		noProgressStreakLimit: limit,
		request: providertypes.GenerateRequest{
			Model:              model,
			SystemPrompt:       systemPrompt,
			Messages:           builtContext.Messages,
			Tools:              toolSpecs,
			SessionAssetReader: s.buildSessionAssetReader(ctx, state.session.ID),
		},
	}, false, nil
}

// resolveNoProgressStreakLimit 统一解析熔断阈值，避免运行期出现无效值导致分支行为不一致。
func resolveNoProgressStreakLimit(rc config.RuntimeConfig) int {
	if rc.MaxNoProgressStreak <= 0 {
		return config.DefaultMaxNoProgressStreak
	}
	return rc.MaxNoProgressStreak
}

// resolveRepeatCycleStreakLimit 统一解析重复调用循环阈值。
func resolveRepeatCycleStreakLimit(rc config.RuntimeConfig) int {
	if rc.MaxRepeatCycleStreak <= 0 {
		return config.DefaultMaxRepeatCycleStreak
	}
	return rc.MaxRepeatCycleStreak
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
			s.emitRunScoped(ctx, EventProviderRetry, state,
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
				s.emitRunScoped(ctx, EventAgentChunk, state, text)
			},
			OnToolCallStart: func(payload providertypes.ToolCallStartPayload) {
				s.emitRunScoped(ctx, EventToolCallThinking, state, payload.Name)
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
	s.emitRunScoped(ctx, EventTokenUsage, state, TokenUsagePayload{
		InputTokens:         result.inputTokens,
		OutputTokens:        result.outputTokens,
		SessionInputTokens:  state.session.TokenInputTotal,
		SessionOutputTokens: state.session.TokenOutputTotal,
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

// autoCompactThreshold 返回当前配置下的自动 compact 触发阈值。
func (s *Service) autoCompactThreshold(ctx context.Context, cfg config.Config) int {
	return s.autoCompactThresholdForState(ctx, cfg, nil)
}

// autoCompactThresholdForState 返回当前配置下的自动 compact 触发阈值，并在单次 run 内按关键输入缓存结果。
func (s *Service) autoCompactThresholdForState(ctx context.Context, cfg config.Config, state *runState) int {
	if !cfg.Context.AutoCompact.Enabled {
		return 0
	}
	if cfg.Context.AutoCompact.InputTokenThreshold > 0 {
		return cfg.Context.AutoCompact.InputTokenThreshold
	}

	key := autoCompactCacheKeyFromConfig(cfg)
	if state != nil && state.autoCompactCache.valid && state.autoCompactCache.key == key {
		return state.autoCompactCache.threshold
	}

	threshold := fallbackAutoCompactThreshold(cfg)
	cacheable := true
	if s != nil && s.autoCompactThresholdResolver != nil {
		resolvedThreshold, err := s.autoCompactThresholdResolver.ResolveAutoCompactThreshold(ctx, cfg)
		if err != nil {
			cacheable = false
		} else if resolvedThreshold > 0 {
			threshold = resolvedThreshold
		}
	}
	if state != nil && cacheable {
		state.autoCompactCache = autoCompactThresholdCache{
			key:       key,
			threshold: threshold,
			valid:     true,
		}
	}
	return threshold
}

// degradeKeepRecentMessages 根据 reactive compact 尝试次数逐步减少保留消息数。
func degradeKeepRecentMessages(base int, attempt int) int {
	for i := 1; i < attempt; i++ {
		base = base / 2
	}
	if base < 1 {
		return 1
	}
	return base
}

// validateUserInputParts 校验输入 parts 的结构合法性和语义有效性，避免空白文本触发无效运行。
func validateUserInputParts(parts []providertypes.ContentPart) error {
	if len(parts) == 0 {
		return errors.New("runtime: input parts is empty")
	}
	if err := providertypes.ValidateParts(parts); err != nil {
		return fmt.Errorf("runtime: invalid input parts: %w", err)
	}
	if !hasUserInputParts(parts) {
		return errors.New("runtime: input content is empty")
	}
	return nil
}

// hasUserInputParts 判断用户输入是否包含可执行语义，图片输入也应被视为有效请求。
func hasUserInputParts(parts []providertypes.ContentPart) bool {
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		case providertypes.ContentPartImage:
			if part.Image != nil {
				return true
			}
		}
	}
	return false
}

// sessionTitleFromParts extracts a sensible title from the input parts.
func sessionTitleFromParts(parts []providertypes.ContentPart) string {
	for _, part := range parts {
		if part.Kind == providertypes.ContentPartText && strings.TrimSpace(part.Text) != "" {
			return strings.TrimSpace(part.Text)
		}
	}
	return "Image Message"
}

// fallbackAutoCompactThreshold 返回自动推导失败时仍可继续使用的保底阈值。
func fallbackAutoCompactThreshold(cfg config.Config) int {
	if cfg.Context.AutoCompact.FallbackInputTokenThreshold > 0 {
		return cfg.Context.AutoCompact.FallbackInputTokenThreshold
	}
	return 0
}

// bindSessionLock 获取并持有指定会话锁，返回对应的释放函数。
func (s *Service) bindSessionLock(sessionID string) func() {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return func() {}
	}
	sessionMu, releaseLockRef := s.acquireSessionLock(id)
	sessionMu.Lock()
	return func() {
		sessionMu.Unlock()
		releaseLockRef()
	}
}

// withSelfHealingReminder 在无进展临界轮次注入自愈提醒，保持提示词拼接规则集中。
func withSelfHealingReminder(systemPrompt string, streak int, limit int) string {
	if streak != limit-1 {
		return systemPrompt
	}
	trimmed := strings.TrimSpace(systemPrompt)
	if trimmed == "" {
		return selfHealingReminder
	}
	return trimmed + "\n\n" + selfHealingReminder
}

// withSelfHealingRepeatReminder 在重复循环临界轮次注入循环自愈提醒，避免模型继续相同工具调用。
func withSelfHealingRepeatReminder(systemPrompt string, repeatStreak int, repeatLimit int) (string, bool) {
	if repeatStreak != repeatLimit-1 {
		return systemPrompt, false
	}
	trimmed := strings.TrimSpace(systemPrompt)
	if trimmed == "" {
		return selfHealingRepeatReminder, true
	}
	return trimmed + "\n\n" + selfHealingRepeatReminder, true
}

// autoCompactCacheKeyFromConfig 提取会影响自动压缩阈值解析的配置维度，用于 run 内缓存命中判断。
func autoCompactCacheKeyFromConfig(cfg config.Config) autoCompactThresholdCacheKey {
	return autoCompactThresholdCacheKey{
		provider:                  strings.TrimSpace(cfg.SelectedProvider),
		model:                     strings.TrimSpace(cfg.CurrentModel),
		autoCompactEnabled:        cfg.Context.AutoCompact.Enabled,
		autoCompactInputThreshold: cfg.Context.AutoCompact.InputTokenThreshold,
		autoCompactReserveTokens:  cfg.Context.AutoCompact.ReserveTokens,
		autoCompactFallback:       cfg.Context.AutoCompact.FallbackInputTokenThreshold,
	}
}
