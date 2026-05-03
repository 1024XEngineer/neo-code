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
	"neo-code/internal/promptasset"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/runtime/streaming"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

var selfHealingReminder = promptasset.NoProgressReminder()

var selfHealingRepeatReminder = promptasset.RepeatCycleReminder()

const (
	usageSourceObserved  = "observed"
	usageSourceEstimated = "estimated"
	usageSourceUnknown   = "unknown"
)

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

// computeTodoStateSignature 计算当前 Todo 列表的状态签名，用于识别 dispatch 是否产生了真实状态变化。
func computeTodoStateSignature(items []agentsession.TodoItem) string {
	normalized := cloneTodosForPersistence(items)
	if len(normalized) == 0 {
		return ""
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// Run 执行一次完整的 ReAct 闭环：保存用户输入、驱动模型、执行工具并发出事件。
// 已有会话会先加锁再加载/更新，确保同一会话并发 Run 不会出现状态覆盖；
// 新会话在创建后再绑定会话锁，不同会话可并行执行。
// Run 会执行受配置约束的最大轮数，避免 provider 异常输出时出现无限循环。
func (s *Service) Run(ctx context.Context, input UserInput) (err error) {
	var statePtr *runState

	runCtx, cancel := context.WithCancel(ctx)
	runToken := s.startRun(cancel, input.RunID)
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

	initialCfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		return s.handleRunError(err)
	}
	sessionID := strings.TrimSpace(input.SessionID)
	releaseSessionLock := s.bindSessionLock(sessionID)
	defer func() {
		releaseSessionLock()
	}()

	sessionTitle := sessionTitleFromParts(input.Parts)
	session, err := s.loadOrCreateSession(ctx, input.SessionID, sessionTitle, initialCfg.Workdir, input.Workdir)
	if err != nil {
		return s.handleRunError(err)
	}
	if applyRequestedAgentMode(&session, input.Mode) {
		session.UpdatedAt = time.Now()
		if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(session)); err != nil {
			return s.handleRunError(err)
		}
	}

	if sessionID == "" {
		releaseSessionLock = s.bindSessionLock(session.ID)
	}

	state := newRunState(input.RunID, session)
	state.runToken = runToken
	state.planningEnabled = strings.TrimSpace(input.Mode) != "" ||
		session.CurrentPlan != nil ||
		agentsession.NormalizeAgentMode(session.AgentMode) == agentsession.AgentModePlan
	state.taskID = strings.TrimSpace(input.TaskID)
	state.agentID = strings.TrimSpace(input.AgentID)
	state.taskKind = inferTaskKindFromInput(input.Parts)
	state.userGoal = strings.TrimSpace(renderPartsForVerification(input.Parts))
	if input.CapabilityToken != nil {
		token := input.CapabilityToken.Normalize()
		state.capabilityToken = &token
	}
	s.bindRunState(runToken, &state)
	statePtr = &state

	effectiveWorkdir := agentsession.EffectiveWorkdir(state.session.Workdir, initialCfg.Workdir)
	_ = s.runHookPoint(ctx, &state, runtimehooks.HookPointSessionStart, runtimehooks.HookContext{
		Metadata: map[string]any{
			"session_id": state.session.ID,
			"run_id":     strings.TrimSpace(input.RunID),
			"workdir":    strings.TrimSpace(effectiveWorkdir),
		},
	})

	submitHookOutput := s.runHookPoint(ctx, &state, runtimehooks.HookPointUserPromptSubmit, runtimehooks.HookContext{
		Metadata: map[string]any{
			"session_id": state.session.ID,
			"run_id":     strings.TrimSpace(input.RunID),
			"workdir":    strings.TrimSpace(effectiveWorkdir),
		},
	})
	if submitHookOutput.Blocked {
		s.emitRunScoped(ctx, EventHookBlocked, &state, HookBlockedPayload{
			HookID:   strings.TrimSpace(submitHookOutput.BlockedBy),
			Source:   string(findHookBlockSource(submitHookOutput)),
			Point:    string(runtimehooks.HookPointUserPromptSubmit),
			Reason:   findHookBlockMessage(submitHookOutput),
			Enforced: true,
		})
		return s.handleRunError(errors.New(findHookBlockMessage(submitHookOutput)))
	}
	if err := s.appendUserMessageAndSave(ctx, &state, input.Parts); err != nil {
		return s.handleRunError(err)
	}
	s.emitRuntimeSnapshotUpdated(ctx, &state, "session_start")

	maxTurns := resolveRuntimeMaxTurns(initialCfg.Runtime)
	for turn := 0; ; turn++ {
		if turn >= maxTurns {
			state.maxTurnsReached = true
			state.maxTurnsLimit = maxTurns
			return s.handleRunError(newMaxTurnLimitError(maxTurns))
		}
		state.turn = turn
		state.compactCount = 0
		state.nextAttemptSeq = 1
		stage := resolvePlanningStageForState(&state)
		if err := s.setBaseRunState(ctx, &state, baseRunStateForPlanningStage(stage)); err != nil {
			return s.handleRunError(err)
		}

	turnAttempt:
		for {
			if err := ctx.Err(); err != nil {
				return s.handleRunError(err)
			}

			snapshot, rebuilt, err := s.prepareTurnBudgetSnapshot(ctx, &state)
			if err != nil {
				return s.handleRunError(err)
			}
			if rebuilt {
				continue
			}

			modelProvider, err := s.providerFactory.Build(ctx, snapshot.ProviderConfig)
			if err != nil {
				return s.handleRunError(err)
			}

			decision, err := s.evaluateTurnBudget(ctx, &state, snapshot, modelProvider)
			if err != nil {
				return s.handleRunError(err)
			}
			switch decision.Action {
			case controlplane.TurnBudgetActionCompact:
				applied, err := s.applyCompactForState(
					ctx,
					&state,
					snapshot.Config,
					contextcompact.ModeProactive,
					compactErrorBestEffort,
				)
				if err != nil {
					return s.handleRunError(err)
				}
				if !applied {
					state.compactCount++
				}
				continue
			case controlplane.TurnBudgetActionStop:
				state.budgetExceeded = true
				return nil
			}

			turnOutput, err := s.callProvider(ctx, &state, snapshot, modelProvider)
			if err != nil {
				if provider.IsContextTooLong(err) &&
					state.reactiveCompactAttempts < snapshot.Config.Context.Budget.MaxReactiveCompacts {
					state.reactiveCompactAttempts++
					degradedCfg := snapshot.Config
					degradedCfg.Context.Compact.ManualKeepRecentMessages = degradeKeepRecentMessages(
						snapshot.Config.Context.Compact.ManualKeepRecentMessages,
						state.reactiveCompactAttempts,
					)
					_, _ = s.applyCompactForState(ctx, &state, degradedCfg, contextcompact.ModeReactive, compactErrorBestEffort)
					continue
				}
				return s.handleRunError(err)
			}

			if strings.TrimSpace(turnOutput.assistant.Role) == "" {
				turnOutput.assistant.Role = providertypes.RoleAssistant
			}
			reconciled, err := s.reconcileLedger(&state, decision, turnOutput.usageObservation)
			if err != nil {
				return s.handleRunError(err)
			}
			hasToolCalls := len(turnOutput.assistant.ToolCalls) > 0
			state.mu.Lock()
			if hasToolCalls {
				state.mustUseToolAfterFinalContinue = false
				state.noToolAfterFinalContinueStreak = 0
			} else if state.mustUseToolAfterFinalContinue {
				state.pendingFinalProgress = false
				state.noToolAfterFinalContinueStreak++
			}
			state.mu.Unlock()
			if hasToolCalls {
				if err := s.appendAssistantMessageAndSave(
					ctx,
					&state,
					snapshot,
					turnOutput.assistant,
					reconciled.inputTokens,
					reconciled.outputTokens,
				); err != nil {
					return s.handleRunError(err)
				}
			} else {
				if err := s.persistAssistantTurnUsageAndMetadata(
					ctx,
					&state,
					snapshot,
					reconciled.inputTokens,
					reconciled.outputTokens,
				); err != nil {
					return s.handleRunError(err)
				}
			}
			s.emitLedgerReconciled(ctx, &state, turnOutput.usageObservation, reconciled)
			s.emitTokenUsage(ctx, &state, reconciled)
			if snapshot.InjectFullPlan && rememberFullPlanRevision(&state.session) {
				state.touchSession()
				if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session)); err != nil {
					return s.handleRunError(err)
				}
			}

			state.mu.Lock()
			state.completion = collectCompletionState(
				&state,
				turnOutput.assistant,
				hasToolCalls,
			)
			completionState, completed := controlplane.EvaluateCompletion(
				state.completion,
				hasToolCalls,
			)
			state.completion = completionState
			state.mu.Unlock()

			if !hasToolCalls {
				stage = resolvePlanningStageForState(&state)
				if stage == planStagePlan {
					planOutput, hasPlanOutput, err := maybeParsePlanTurnOutput(turnOutput.assistant)
					if err != nil {
						return s.handleRunError(err)
					}
					if hasPlanOutput {
						nextPlan, err := buildPlanArtifact(state.session.CurrentPlan, planOutput)
						if err != nil {
							return s.handleRunError(err)
						}
						applyCurrentPlanRevision(&state.session, nextPlan)
						state.touchSession()
						if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session)); err != nil {
							return s.handleRunError(err)
						}
						planMessage := providertypes.Message{
							Role: providertypes.RoleAssistant,
							Parts: []providertypes.ContentPart{
								providertypes.NewTextPart(resolvePlanDisplayText(planOutput, nextPlan.Spec)),
							},
						}
						if err := s.appendAssistantMessageOnlyAndSave(ctx, &state, planMessage); err != nil {
							return s.handleRunError(err)
						}
						s.emitRunScoped(ctx, EventAgentDone, &state, planMessage)
						return nil
					}
				}
				completionSignaled, err := maybeParseCompletionTurnOutput(turnOutput.assistant)
				if err != nil {
					return s.handleRunError(err)
				}
				if err := s.setBaseRunState(ctx, &state, controlplane.RunStateVerify); err != nil {
					return s.handleRunError(err)
				}
				completionHookOutput := s.runHookPoint(
					ctx,
					&state,
					runtimehooks.HookPointBeforeCompletionDecision,
					runtimehooks.HookContext{
						Metadata: map[string]any{
							"completion_passed": completed,
							"has_tool_calls":    hasToolCalls,
							"assistant_role":    strings.TrimSpace(turnOutput.assistant.Role),
							"workdir":           strings.TrimSpace(snapshot.Workdir),
						},
					},
				)
				if completionHookOutput.Blocked {
					s.emitRunScoped(ctx, EventHookBlocked, &state, HookBlockedPayload{
						HookID:   strings.TrimSpace(completionHookOutput.BlockedBy),
						Source:   string(findHookBlockSource(completionHookOutput)),
						Point:    string(runtimehooks.HookPointBeforeCompletionDecision),
						Reason:   findHookBlockMessage(completionHookOutput),
						Enforced: false,
					})
				}

				s.emitRunScopedOptional(EventVerificationStarted, &state, VerificationStartedPayload{
					CompletionPassed:        completed,
					CompletionBlockedReason: strings.TrimSpace(string(state.completion.CompletionBlockedReason)),
				})
				acceptanceDecision, err := s.beforeAcceptFinal(ctx, &state, snapshot, turnOutput.assistant, completed)
				if err != nil {
					return s.handleRunError(err)
				}
				for _, result := range acceptanceDecision.VerifierResults {
					s.emitRunScopedOptional(EventVerificationStageFinished, &state, VerificationStageFinishedPayload{
						Name:       result.Name,
						Status:     result.Status,
						Summary:    result.Summary,
						Reason:     result.Reason,
						ErrorClass: result.ErrorClass,
					})
				}
				s.emitRunScopedOptional(EventVerificationFinished, &state, VerificationFinishedPayload{
					AcceptanceStatus: acceptanceDecision.Status,
					StopReason:       acceptanceDecision.StopReason,
					ErrorClass:       acceptanceDecision.ErrorClass,
				})
				s.emitRunScopedOptional(EventAcceptanceDecided, &state, AcceptanceDecidedPayload{
					Status:                  acceptanceDecision.Status,
					StopReason:              acceptanceDecision.StopReason,
					ErrorClass:              acceptanceDecision.ErrorClass,
					CompletionBlockedReason: strings.TrimSpace(acceptanceDecision.CompletionBlockedReason),
					UserVisibleSummary:      acceptanceDecision.UserVisibleSummary,
					InternalSummary:         acceptanceDecision.InternalSummary,
					ContinueHint:            acceptanceDecision.ContinueHint,
				})
				applyAcceptanceResultProgress(&state, acceptanceDecision)

				switch acceptanceDecision.Status {
				case acceptance.AcceptanceAccepted:
					state.lastAcceptanceBlockedReason = ""
					state.mustUseToolAfterFinalContinue = false
					state.noToolAfterFinalContinueStreak = 0
					if markCurrentPlanCompleted(&state.session, completionSignaled) {
						state.touchSession()
						if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session)); err != nil {
							return s.handleRunError(err)
						}
					}
					if err := s.appendAssistantMessageOnlyAndSave(ctx, &state, turnOutput.assistant); err != nil {
						return s.handleRunError(err)
					}
					s.emitRunScopedOptional(EventVerificationCompleted, &state, VerificationCompletedPayload{
						StopReason: acceptanceDecision.StopReason,
					})
					recordAcceptanceTerminal(&state, acceptanceDecision)
					s.emitRunScoped(ctx, EventAgentDone, &state, turnOutput.assistant)
					s.triggerMemoExtraction(state.session.ID, state.session.Messages, state.rememberedThisRun)
					return nil
				case acceptance.AcceptanceContinue:
					state.lastAcceptanceBlockedReason = strings.TrimSpace(acceptanceDecision.CompletionBlockedReason)
					state.mustUseToolAfterFinalContinue = true
					if state.noToolAfterFinalContinueStreak == 0 {
						state.noToolAfterFinalContinueStreak = 1
					}
					reminder := strings.TrimSpace(buildAcceptanceContinueHint(acceptanceDecision))
					if reminder == "" {
						reminder = finalContinueReminder
					}
					if err := s.appendSystemMessageAndSave(ctx, &state, reminder); err != nil {
						return s.handleRunError(err)
					}
					break turnAttempt
				case acceptance.AcceptanceIncomplete:
					state.lastAcceptanceBlockedReason = ""
					state.mustUseToolAfterFinalContinue = false
					state.noToolAfterFinalContinueStreak = 0
					if err := s.appendAssistantMessageOnlyAndSave(ctx, &state, turnOutput.assistant); err != nil {
						return s.handleRunError(err)
					}
					recordAcceptanceTerminal(&state, acceptanceDecision)
					s.emitRunScoped(ctx, EventAgentDone, &state, turnOutput.assistant)
					return nil
				case acceptance.AcceptanceFailed:
					state.lastAcceptanceBlockedReason = ""
					state.mustUseToolAfterFinalContinue = false
					state.noToolAfterFinalContinueStreak = 0
					if err := s.appendAssistantMessageOnlyAndSave(ctx, &state, turnOutput.assistant); err != nil {
						return s.handleRunError(err)
					}
					s.emitRunScopedOptional(EventVerificationFailed, &state, VerificationFailedPayload{
						StopReason: acceptanceDecision.StopReason,
						ErrorClass: acceptanceDecision.ErrorClass,
					})
					recordAcceptanceTerminal(&state, acceptanceDecision)
					s.emitRunScoped(ctx, EventAgentDone, &state, turnOutput.assistant)
					return nil
				default:
					state.lastAcceptanceBlockedReason = ""
					state.mustUseToolAfterFinalContinue = false
					state.noToolAfterFinalContinueStreak = 0
					if err := s.appendAssistantMessageOnlyAndSave(ctx, &state, turnOutput.assistant); err != nil {
						return s.handleRunError(err)
					}
					recordAcceptanceTerminal(&state, acceptanceDecision)
					s.emitRunScoped(ctx, EventAgentDone, &state, turnOutput.assistant)
					return nil
				}
			}

			beforeTask := state.session.TaskState.Clone()
			beforeTodos := cloneTodosForPersistence(state.session.Todos)
			if err := s.setBaseRunState(ctx, &state, controlplane.RunStateExecute); err != nil {
				return s.handleRunError(err)
			}
			summary, err := s.executeAssistantToolCalls(ctx, &state, snapshot, turnOutput.assistant)
			if err != nil {
				return s.handleRunError(err)
			}

			state.mu.Lock()
			state.completion = applyToolExecutionCompletion(state.completion, summary)
			afterTask := state.session.TaskState.Clone()
			afterTodos := cloneTodosForPersistence(state.session.Todos)
			progressRunState := controlplane.RunStateExecute
			if resolvePlanningStageForState(&state) == planStagePlan {
				progressRunState = controlplane.RunStatePlan
			}
			progressInput := collectProgressInput(
				progressRunState,
				beforeTask,
				afterTask,
				beforeTodos,
				afterTodos,
				summary,
				snapshot.NoProgressStreakLimit,
				snapshot.RepeatCycleStreakLimit,
			)
			state.progress = controlplane.EvaluateProgress(state.progress, progressInput)
			currentScore := state.progress.LastScore
			if shouldPromotePendingFinalProgress(
				currentScore,
				summary,
				state.completion,
				state.lastAcceptanceBlockedReason,
			) {
				state.pendingFinalProgress = true
			}
			state.mu.Unlock()

			s.emitRunScoped(ctx, EventProgressEvaluated, &state, ProgressEvaluatedPayload{Score: currentScore})
			if err := s.setBaseRunState(ctx, &state, controlplane.RunStateVerify); err != nil {
				return s.handleRunError(err)
			}
			break
		}
	}
}

// prepareTurnBudgetSnapshot 基于当前会话状态冻结一次预算尝试所需的 request 与预算事实。
func (s *Service) prepareTurnBudgetSnapshot(ctx context.Context, state *runState) (TurnBudgetSnapshot, bool, error) {
	cfg, err := s.loadConfigSnapshot(ctx)
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	activeWorkdir := agentsession.EffectiveWorkdir(state.session.Workdir, cfg.Workdir)
	activeSkills, err := s.resolveActiveSkills(ctx, state)
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	repositorySummary, repositoryContext, err := s.buildRepositoryContext(ctx, state, activeWorkdir)
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	stage := resolvePlanningStageForState(state)
	readOnly := isReadOnlyPlanningStage(stage)
	injectFullPlan := planningNeedsFullPlan(state)

	builtContext, err := s.contextBuilder.Build(ctx, agentcontext.BuildInput{
		Messages:          state.session.Messages,
		TaskState:         state.session.TaskState,
		Todos:             cloneTodosForPersistence(state.session.Todos),
		AgentMode:         state.session.AgentMode,
		PlanStage:         stage,
		CurrentPlan:       state.session.CurrentPlan.Clone(),
		InjectFullPlan:    injectFullPlan,
		ActiveSkills:      activeSkills,
		RepositorySummary: repositorySummary,
		Repository:        repositoryContext,
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
			MicroCompactRetainedToolSpans: cfg.Context.Compact.MicroCompactRetainedToolSpans,
			ReadTimeMaxMessageSpans:       cfg.Context.Compact.ReadTimeMaxMessageSpans,
		},
	})
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	if strings.Contains(builtContext.SystemPrompt, "## Todo State") {
		s.emitRunScoped(ctx, EventTodoSummaryInjected, state, TodoEventPayload{})
	}

	toolSpecs, err := s.toolManager.ListAvailableSpecs(ctx, tools.SpecListInput{
		SessionID: state.session.ID,
		Mode:      string(agentsession.NormalizeAgentMode(state.session.AgentMode)),
		ReadOnly:  readOnly,
	})
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	toolSpecs = prioritizeToolSpecsBySkillHints(toolSpecs, activeSkills)

	resolvedProvider, err := config.ResolveSelectedProvider(cfg)
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}
	providerRuntimeCfg, err := resolvedProvider.ToRuntimeConfig()
	if err != nil {
		return TurnBudgetSnapshot{}, false, err
	}

	state.mu.Lock()
	score := state.progress.LastScore
	state.mu.Unlock()

	limit := resolveNoProgressStreakLimit(cfg.Runtime)
	repeatLimit := resolveRepeatCycleStreakLimit(cfg.Runtime)
	systemPrompt := withProgressReminder(builtContext.SystemPrompt, score)
	if notificationHint := strings.TrimSpace(s.drainHookNotificationsForTurn(state)); notificationHint != "" {
		systemPrompt = mergeEphemeralHookNotificationIntoSystemPrompt(systemPrompt, notificationHint)
	}
	promptBudget, budgetSource := s.resolvePromptBudget(ctx, cfg)
	model := strings.TrimSpace(cfg.CurrentModel)
	requestMessages := append([]providertypes.Message(nil), builtContext.Messages...)
	request := providertypes.GenerateRequest{
		Model:              model,
		SystemPrompt:       systemPrompt,
		Messages:           requestMessages,
		Tools:              toolSpecs,
		SessionAssetReader: s.buildSessionAssetReader(ctx, state.session.ID),
	}
	attemptSeq := state.nextAttemptSeq
	if attemptSeq <= 0 {
		attemptSeq = 1
	}
	return newTurnBudgetSnapshot(
		attemptSeq,
		cfg,
		providerRuntimeCfg,
		model,
		activeWorkdir,
		time.Duration(cfg.ToolTimeoutSec)*time.Second,
		promptBudget,
		budgetSource,
		state.compactCount,
		limit,
		repeatLimit,
		injectFullPlan,
		request,
	), false, nil
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

// resolveRuntimeMaxTurns 统一解析运行期最大轮数，避免无效配置导致主循环失控。
func resolveRuntimeMaxTurns(rc config.RuntimeConfig) int {
	if rc.MaxTurns <= 0 {
		return config.DefaultMaxTurns
	}
	return rc.MaxTurns
}

// callProvider 使用冻结后的 TurnBudgetSnapshot 执行单次 provider 调用。
func (s *Service) callProvider(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	modelProvider provider.Provider,
) (turnProviderOutput, error) {
	streamOutcome := generateStreamingMessage(ctx, modelProvider, snapshot.Request, streaming.Hooks{
		OnTextDelta: func(text string) {
			s.emitRunScoped(ctx, EventAgentChunk, state, text)
		},
		OnToolCallStart: func(payload providertypes.ToolCallStartPayload) {
			s.emitRunScoped(ctx, EventToolCallThinking, state, payload.Name)
		},
	})
	if streamOutcome.err != nil {
		return turnProviderOutput{}, streamOutcome.err
	}

	return turnProviderOutput{
		assistant: streamOutcome.message,
		usageObservation: newTurnBudgetUsageObservation(
			snapshot.ID,
			streamOutcome.inputTokens,
			streamOutcome.outputTokens,
			streamOutcome.inputObserved,
			streamOutcome.outputObserved,
		),
	}, nil
}

// emitTokenUsage 在单轮 provider 调用成功后发出 token_usage 事件。
func (s *Service) emitTokenUsage(ctx context.Context, state *runState, result ledgerReconcileResult) {
	if result.inputTokens == 0 && result.outputTokens == 0 && !result.hasUnknownUsage {
		return
	}
	s.emitRunScoped(ctx, EventTokenUsage, state, TokenUsagePayload{
		InputTokens:         result.inputTokens,
		OutputTokens:        result.outputTokens,
		InputSource:         result.inputSource,
		OutputSource:        result.outputSource,
		HasUnknownUsage:     result.hasUnknownUsage,
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
	applied := false
	if err := s.enterTemporaryRunState(ctx, state, controlplane.RunStateCompacting); err != nil {
		return false, err
	}
	defer func() {
		_ = s.leaveTemporaryRunState(ctx, state, controlplane.RunStateCompacting)
	}()

	err := func() error {
		session, result, compactErr := s.runCompactForSession(ctx, state.runID, state.session, cfg, mode, errorPolicy)
		if compactErr != nil {
			return compactErr
		}
		state.session = session
		if result.Applied {
			if markCurrentPlanContextDirty(&state.session) {
				state.session.UpdatedAt = time.Now()
				if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(state.session)); err != nil {
					return err
				}
			}
			if mode == contextcompact.ModeProactive || mode == contextcompact.ModeReactive {
				state.compactCount++
			}
			state.resetTokenTotals()
			state.nextAttemptSeq++
			applied = true
		}
		return nil
	}()
	if err != nil {
		return false, err
	}
	return applied, nil
}

// resolvePromptBudget 解析当前请求链路使用的 prompt budget 与来源标签。
func (s *Service) resolvePromptBudget(ctx context.Context, cfg config.Config) (int, string) {
	if cfg.Context.Budget.PromptBudget > 0 {
		return cfg.Context.Budget.PromptBudget, "explicit"
	}
	promptBudget := cfg.Context.Budget.FallbackPromptBudget
	source := "fallback"
	if s != nil && s.budgetResolver != nil {
		resolvedBudget, resolvedSource, err := s.budgetResolver.ResolvePromptBudget(ctx, cfg)
		if err == nil && resolvedBudget > 0 {
			promptBudget = resolvedBudget
			if strings.TrimSpace(resolvedSource) != "" {
				source = resolvedSource
			}
		}
	}
	return promptBudget, source
}

// evaluateTurnBudget 对冻结请求执行发送前输入 token 估算，并产出唯一预算动作。
func (s *Service) evaluateTurnBudget(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	modelProvider provider.Provider,
) (controlplane.TurnBudgetDecision, error) {
	providerEstimate, err := modelProvider.EstimateInputTokens(ctx, snapshot.Request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return controlplane.TurnBudgetDecision{}, err
		}
		if !shouldBypassEstimateFailure(err) {
			return controlplane.TurnBudgetDecision{}, fmt.Errorf("runtime: estimate input tokens: %w", err)
		}
		s.emitRunScoped(ctx, EventBudgetEstimateFailed, state, newBudgetEstimateFailedPayload(snapshot.ID, err))
		decision := controlplane.TurnBudgetDecision{
			ID:                 snapshot.ID,
			Action:             controlplane.TurnBudgetActionAllow,
			Reason:             controlplane.BudgetDecisionReasonEstimateFailedBypass,
			PromptBudget:       snapshot.PromptBudget,
			EstimateGatePolicy: provider.EstimateGateAdvisory,
		}
		s.emitRunScoped(ctx, EventBudgetChecked, state, newBudgetCheckedPayload(decision))
		return decision, nil
	}
	estimate := newTurnBudgetEstimate(snapshot.ID, providerEstimate)
	decision := controlplane.DecideTurnBudget(
		estimate,
		snapshot.PromptBudget,
		snapshot.CompactCount,
	)
	s.emitRunScoped(ctx, EventBudgetChecked, state, newBudgetCheckedPayload(decision))
	return decision, nil
}

// shouldBypassEstimateFailure 判断估算失败是否允许降级放行，仅对可恢复 provider 错误放行。
func shouldBypassEstimateFailure(err error) bool {
	var providerErr *provider.ProviderError
	return errors.As(err, &providerErr) && providerErr.Retryable
}

// reconcileLedger 根据 observed usage 或发送前 estimate 生成本轮账本写入结果。
func (s *Service) reconcileLedger(
	state *runState,
	decision controlplane.TurnBudgetDecision,
	observation TurnBudgetUsageObservation,
) (ledgerReconcileResult, error) {
	if decision.ID != observation.ID {
		return ledgerReconcileResult{}, fmt.Errorf("runtime: turn budget id mismatch between decision and usage observation")
	}
	reconciled := ledgerReconcileResult{
		inputSource:  usageSourceUnknown,
		outputSource: usageSourceUnknown,
	}
	if observation.InputObserved {
		reconciled.inputTokens = observation.InputTokens
		reconciled.inputSource = usageSourceObserved
	} else {
		reconciled.inputTokens = decision.EstimatedInputTokens
		reconciled.inputSource = usageSourceEstimated
	}
	if observation.OutputObserved {
		reconciled.outputTokens = observation.OutputTokens
		reconciled.outputSource = usageSourceObserved
	}
	if observation.InputObserved && observation.OutputObserved {
		return reconciled, nil
	}
	reconciled.hasUnknownUsage = true
	if state != nil {
		state.session.HasUnknownUsage = true
		state.hasUnknownUsage = true
	}
	return reconciled, nil
}

// emitLedgerReconciled 发出本轮 usage 调和结果，便于区分 observed 与估算值。
func (s *Service) emitLedgerReconciled(
	ctx context.Context,
	state *runState,
	observation TurnBudgetUsageObservation,
	result ledgerReconcileResult,
) {
	s.emitRunScoped(ctx, EventLedgerReconciled, state, newLedgerReconciledPayload(observation, result))
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

// sessionTitleFromParts 从输入 parts 中提取一个合适的会话标题。
func sessionTitleFromParts(parts []providertypes.ContentPart) string {
	for _, part := range parts {
		if part.Kind == providertypes.ContentPartText && strings.TrimSpace(part.Text) != "" {
			return strings.TrimSpace(part.Text)
		}
	}
	return "Image Message"
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

// withProgressReminder 根据当前 progress 快照选择并注入唯一的自愈提醒。
func withProgressReminder(systemPrompt string, score controlplane.ProgressScore) string {
	var reminder string
	switch score.ReminderKind {
	case controlplane.ReminderKindRepeatCycle:
		reminder = selfHealingRepeatReminder
	case controlplane.ReminderKindNoProgress, controlplane.ReminderKindGenericStalled:
		reminder = selfHealingReminder
	default:
		return systemPrompt
	}

	trimmed := strings.TrimSpace(systemPrompt)
	if trimmed == "" {
		return reminder
	}
	return trimmed + "\n\n" + reminder
}

// computeRequestHash 计算冻结请求的稳定指纹，避免 compact 前后的估算结果串用。
func computeRequestHash(req providertypes.GenerateRequest) string {
	hashInput := struct {
		Model        string                  `json:"model"`
		SystemPrompt string                  `json:"system_prompt"`
		Messages     []providertypes.Message `json:"messages"`
		Tools        []tools.ToolSpec        `json:"tools"`
	}{
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		Messages:     cloneMessages(req.Messages),
		Tools:        append([]tools.ToolSpec(nil), req.Tools...),
	}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
