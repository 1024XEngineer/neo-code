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

// CompactInput 描述一次手动 compact 请求所需的最小输入。
type CompactInput struct {
	SessionID string
	RunID     string
}

// CompactResult 汇总 compact 执行后的外部可见结果。
type CompactResult struct {
	Applied        bool
	BeforeChars    int
	AfterChars     int
	SavedRatio     float64
	TriggerMode    string
	TranscriptID   string
	TranscriptPath string
}

// CompactDonePayload 是 compact 完成事件向上层暴露的摘要信息。
type CompactDonePayload struct {
	Applied        bool    `json:"applied"`
	BeforeChars    int     `json:"before_chars"`
	AfterChars     int     `json:"after_chars"`
	SavedRatio     float64 `json:"saved_ratio"`
	TriggerMode    string  `json:"trigger_mode"`
	TranscriptID   string  `json:"transcript_id"`
	TranscriptPath string  `json:"transcript_path"`
}

// CompactErrorPayload 是 compact 失败事件向上层暴露的错误信息。
type CompactErrorPayload struct {
	TriggerMode string `json:"trigger_mode"`
	Message     string `json:"message"`
}

// Compact 串行执行手动 compact，并返回本次压缩的统计结果。
func (s *Service) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	if err := ctx.Err(); err != nil {
		return CompactResult{}, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return CompactResult{}, errors.New("runtime: compact session_id is empty")
	}

	s.operationMu.Lock()
	defer s.operationMu.Unlock()

	cfg := s.configManager.Get()
	session, err := s.sessionStore.Load(ctx, input.SessionID)
	if err != nil {
		return CompactResult{}, err
	}

	session, result, err := s.runCompactForSession(ctx, input.RunID, session, cfg, true)
	if err != nil {
		return CompactResult{}, err
	}

	return CompactResult{
		Applied:        result.Applied,
		BeforeChars:    result.Metrics.BeforeChars,
		AfterChars:     result.Metrics.AfterChars,
		SavedRatio:     result.Metrics.SavedRatio,
		TriggerMode:    result.Metrics.TriggerMode,
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}, nil
}

// runCompactForSession 负责发出 compact 事件、调用 runner，并在成功后回写会话。
func (s *Service) runCompactForSession(
	ctx context.Context,
	runID string,
	session agentsession.Session,
	cfg config.Config,
	failOnError bool,
) (agentsession.Session, contextcompact.Result, error) {
	runner := s.compactRunner
	if runner == nil {
		var err error
		runner, err = s.defaultCompactRunner(session, cfg)
		if err != nil {
			s.emit(ctx, EventCompactError, runID, session.ID, CompactErrorPayload{
				TriggerMode: string(contextcompact.ModeManual),
				Message:     err.Error(),
			})
			if failOnError {
				return session, contextcompact.Result{}, err
			}
			return session, contextcompact.Result{}, nil
		}
	}

	originalMessages := append([]providertypes.Message(nil), session.Messages...)
	s.emit(ctx, EventCompactStart, runID, session.ID, string(contextcompact.ModeManual))

	result, err := runner.Run(ctx, contextcompact.Input{
		Mode:      contextcompact.ModeManual,
		SessionID: session.ID,
		Workdir:   cfg.Workdir,
		Messages:  session.Messages,
		Config:    cfg.Context.Compact,
	})
	if err != nil {
		s.emit(ctx, EventCompactError, runID, session.ID, CompactErrorPayload{
			TriggerMode: string(contextcompact.ModeManual),
			Message:     err.Error(),
		})
		if failOnError {
			return session, contextcompact.Result{}, err
		}
		return session, contextcompact.Result{}, nil
	}

	if result.Applied {
		session.Messages = append([]providertypes.Message(nil), result.Messages...)
		session.UpdatedAt = time.Now()
		if err := s.sessionStore.Save(ctx, &session); err != nil {
			s.emit(ctx, EventCompactError, runID, session.ID, CompactErrorPayload{
				TriggerMode: string(contextcompact.ModeManual),
				Message:     err.Error(),
			})
			session.Messages = originalMessages
			if failOnError {
				return session, contextcompact.Result{}, err
			}
			return session, contextcompact.Result{}, nil
		}
	}

	donePayload := CompactDonePayload{
		Applied:        result.Applied,
		BeforeChars:    result.Metrics.BeforeChars,
		AfterChars:     result.Metrics.AfterChars,
		SavedRatio:     result.Metrics.SavedRatio,
		TriggerMode:    string(contextcompact.ModeManual),
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}
	s.emit(ctx, EventCompactDone, runID, session.ID, donePayload)

	return session, result, nil
}

// defaultCompactRunner 为手动 compact 选择摘要生成器并构造默认 runner。
func (s *Service) defaultCompactRunner(session agentsession.Session, cfg config.Config) (contextcompact.Runner, error) {
	resolvedProvider, model, err := resolveCompactProviderSelection(session, cfg)
	if err != nil {
		return nil, err
	}
	return contextcompact.NewRunner(newCompactSummaryGenerator(s.providerFactory, resolvedProvider.ToRuntimeConfig(), model)), nil
}

// resolveCompactProviderSelection 优先复用会话记录的 provider/model，缺失时再回退当前配置。
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

	resolved, err := resolveSelectedProviderFromConfig(cfg)
	if err != nil {
		return config.ResolvedProviderConfig{}, "", err
	}
	return resolved, strings.TrimSpace(cfg.CurrentModel), nil
}

// resolveSelectedProviderFromConfig 统一解析当前选中的 provider 配置并补全密钥。
func resolveSelectedProviderFromConfig(cfg config.Config) (config.ResolvedProviderConfig, error) {
	providerCfg, err := cfg.SelectedProviderConfig()
	if err != nil {
		return config.ResolvedProviderConfig{}, err
	}
	return providerCfg.Resolve()
}
