package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// CompactInput 描述一次手动 compact 请求的最小输入。
type CompactInput struct {
	SessionID string
	RunID     string
}

// CompactResult 汇总一次 compact 完成后的外部可见结果，统一作为返回值和事件 payload 使用。
type CompactResult struct {
	Applied        bool
	BeforeChars    int
	AfterChars     int
	SavedRatio     float64
	TriggerMode    string
	TranscriptID   string
	TranscriptPath string
}

// fromCompactResult 将 contextcompact.Result 映射为 runtime.CompactResult。
func fromCompactResult(result contextcompact.Result) CompactResult {
	return CompactResult{
		Applied:        result.Applied,
		BeforeChars:    result.Metrics.BeforeChars,
		AfterChars:     result.Metrics.AfterChars,
		SavedRatio:     result.Metrics.SavedRatio,
		TriggerMode:    result.Metrics.TriggerMode,
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}
}

// CompactErrorPayload 是 compact_error 事件对外暴露的错误信息。
type CompactErrorPayload struct {
	TriggerMode string `json:"trigger_mode"`
	Message     string `json:"message"`
}

// compactErrorPolicy 描述 compact 失败后是立即中断还是仅记录事件后继续运行。
type compactErrorPolicy uint8

const (
	compactErrorStrict compactErrorPolicy = iota
	compactErrorBestEffort
)

// Compact 串行执行一次手动 compact，并返回本次压缩统计信息。
// 会话级锁确保同一会话的 Run 和 Compact 互斥，不同会话可并行。
func (s *Service) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	if err := ctx.Err(); err != nil {
		return CompactResult{}, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return CompactResult{}, errors.New("runtime: compact session_id is empty")
	}

	sessionMu, releaseLockRef := s.acquireSessionLock(input.SessionID)
	sessionMu.Lock()
	defer func() {
		sessionMu.Unlock()
		releaseLockRef()
	}()

	cfg := s.configManager.Get()
	session, err := s.sessionStore.Load(ctx, input.SessionID)
	if err != nil {
		return CompactResult{}, err
	}

	session, result, err := s.runCompactForSession(ctx, input.RunID, session, cfg, contextcompact.ModeManual, compactErrorStrict)
	if err != nil {
		return CompactResult{}, err
	}

	return fromCompactResult(result), nil
}

// runCompactForSession 负责发出 compact 事件、调用 runner，并在成功后回写会话。
func (s *Service) runCompactForSession(
	ctx context.Context,
	runID string,
	session agentsession.Session,
	cfg config.Config,
	mode contextcompact.Mode,
	errorPolicy compactErrorPolicy,
) (agentsession.Session, contextcompact.Result, error) {
	failCompact := func(err error) (agentsession.Session, contextcompact.Result, error) {
		s.emit(ctx, EventCompactError, runID, session.ID, CompactErrorPayload{
			TriggerMode: string(mode),
			Message:     err.Error(),
		})
		if errorPolicy == compactErrorStrict {
			return session, contextcompact.Result{}, err
		}
		return session, contextcompact.Result{}, nil
	}

	runner := s.compactRunner
	if runner == nil {
		var err error
		runner, err = s.defaultCompactRunner(session, cfg)
		if err != nil {
			return failCompact(err)
		}
	}

	originalMessages := append([]providertypes.Message(nil), session.Messages...)
	originalTaskState := session.TaskState.Clone()
	originalTokenInputTotal := session.TokenInputTotal
	originalTokenOutputTotal := session.TokenOutputTotal
	originalUpdatedAt := session.UpdatedAt
	s.emit(ctx, EventCompactStart, runID, session.ID, string(mode))

	result, err := runner.Run(ctx, contextcompact.Input{
		Mode:      mode,
		SessionID: session.ID,
		Workdir:   agentsession.EffectiveWorkdir(session.Workdir, cfg.Workdir),
		Messages:  session.Messages,
		TaskState: session.TaskState,
		Config:    cfg.Context.Compact,
	})
	if err != nil {
		return failCompact(err)
	}

	if result.Applied {
		session.Messages = append([]providertypes.Message(nil), result.Messages...)
		session.TaskState = result.TaskState.Clone()
		session.TokenInputTotal = 0
		session.TokenOutputTotal = 0
		session.UpdatedAt = time.Now()
		if err := s.sessionStore.Save(ctx, &session); err != nil {
			session.Messages = originalMessages
			session.TaskState = originalTaskState
			session.TokenInputTotal = originalTokenInputTotal
			session.TokenOutputTotal = originalTokenOutputTotal
			session.UpdatedAt = originalUpdatedAt
			return failCompact(err)
		}
	}

	s.emit(ctx, EventCompactDone, runID, session.ID, fromCompactResult(result))
	return session, result, nil
}

// defaultCompactRunner 为 runtime 懒加载默认 compact runner。
func (s *Service) defaultCompactRunner(session agentsession.Session, cfg config.Config) (contextcompact.Runner, error) {
	resolvedProvider, model, err := resolveCompactProviderSelection(session, cfg)
	if err != nil {
		return nil, err
	}
	return contextcompact.NewRunner(newCompactSummaryGenerator(s.providerFactory, resolvedProvider.ToRuntimeConfig(), model)), nil
}

// resolveCompactProviderSelection 优先复用会话最近成功运行时记录的 provider 与 model。
func resolveCompactProviderSelection(session agentsession.Session, cfg config.Config) (config.ResolvedProviderConfig, string, error) {
	sessionProvider := strings.TrimSpace(session.Provider)
	sessionModel := strings.TrimSpace(session.Model)
	if sessionProvider != "" && sessionModel != "" {
		providerCfg, err := cfg.ProviderByName(sessionProvider)
		if err != nil {
			return config.ResolvedProviderConfig{}, "", err
		}
		resolved, err := providerCfg.Resolve()
		if err != nil {
			return config.ResolvedProviderConfig{}, "", err
		}
		return resolved, sessionModel, nil
	}

	resolved, err := config.ResolveSelectedProvider(cfg)
	if err != nil {
		return config.ResolvedProviderConfig{}, "", err
	}
	return resolved, strings.TrimSpace(cfg.CurrentModel), nil
}
