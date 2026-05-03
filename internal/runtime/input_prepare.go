package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
)

const prepareEventEmitTimeout = 200 * time.Millisecond

// NewSessionInputPreparer 创建基于 session 子层实现的输入归一化适配器。
func NewSessionInputPreparer(store agentsession.Store, assetStore agentsession.AssetStore) UserInputPreparer {
	return sessionInputPreparer{
		preparer: agentsession.NewInputPreparer(store, assetStore),
	}
}

// PrepareUserInput 负责在运行前执行输入归一化编排，并发出最小可观测事件。
// Submit 作为运行时提交入口，统一串联输入归一化与执行，避免上层手动编排两段调用。
func (s *Service) Submit(ctx context.Context, input PrepareInput) error {
	prepared, err := s.PrepareUserInput(ctx, input)
	if err != nil {
		return err
	}
	return s.Run(ctx, prepared)
}

func (s *Service) PrepareUserInput(ctx context.Context, input PrepareInput) (UserInput, error) {
	if err := ctx.Err(); err != nil {
		return UserInput{}, err
	}
	if s == nil {
		return UserInput{}, errors.New("runtime: service is nil")
	}
	if s.userInputPreparer == nil {
		err := errors.New("runtime: user input preparer is not configured")
		_ = s.emitPrepareFailure(ctx, input, err)
		return UserInput{}, err
	}

	defaultWorkdir := ""
	sessionAssetPolicy := agentsession.DefaultAssetPolicy()
	if s.configManager != nil {
		cfg := s.configManager.Get()
		defaultWorkdir = strings.TrimSpace(cfg.Workdir)
		sessionAssetPolicy = cfg.Runtime.ResolveSessionAssetPolicy()
	}
	if limitAwareStore, ok := s.sessionAssetStore.(sessionAssetLimitStore); ok {
		limitAwareStore.SetAssetPolicy(sessionAssetPolicy)
	}

	prepared, err := s.userInputPreparer.Prepare(ctx, input, defaultWorkdir)
	if err != nil {
		_ = s.emitPrepareFailure(ctx, input, err)
		return UserInput{}, err
	}

	runID := strings.TrimSpace(input.RunID)
	_ = s.emitPrepareEvent(ctx, EventInputNormalized, runID, prepared.UserInput.SessionID, InputNormalizedPayload{
		TextLength: len([]rune(strings.TrimSpace(input.Text))),
		ImageCount: len(input.Images),
	})
	for index, asset := range prepared.SavedAssets {
		path := ""
		if index >= 0 && index < len(input.Images) {
			path = strings.TrimSpace(input.Images[index].Path)
		}
		_ = s.emitPrepareEvent(ctx, EventAssetSaved, runID, prepared.UserInput.SessionID, AssetSavedPayload{
			Index:    index,
			Path:     path,
			AssetID:  asset.ID,
			MimeType: asset.MimeType,
			Size:     asset.Size,
		})
	}

	return prepared.UserInput, nil
}

// emitPrepareFailure 统一发送输入归一化阶段的失败事件，避免前置副作用变成黑箱。
func (s *Service) emitPrepareFailure(ctx context.Context, input PrepareInput, err error) error {
	if s == nil {
		return nil
	}

	runID := strings.TrimSpace(input.RunID)
	sessionID := strings.TrimSpace(input.SessionID)

	var saveErr *agentsession.AssetSaveError
	if errors.As(err, &saveErr) {
		if session := strings.TrimSpace(saveErr.SessionID); session != "" {
			sessionID = session
		}
		return s.emitPrepareEvent(ctx, EventAssetSaveFailed, runID, sessionID, AssetSaveFailedPayload{
			Index:   saveErr.Index,
			Path:    strings.TrimSpace(saveErr.Path),
			Message: strings.TrimSpace(saveErr.Error()),
		})
	}
	// 会话不存在的错误由 gateway bridge 的 retry 透明处理，不需要暴露给用户
	if errors.Is(err, agentsession.ErrSessionNotFound) {
		return nil
	}
	return s.emitPrepareEvent(ctx, EventError, runID, sessionID, strings.TrimSpace(err.Error()))
}

// emitPrepareEvent 在输入归一化阶段使用限时上下文发事件，避免通道拥塞导致提交链路卡死。
func (s *Service) emitPrepareEvent(ctx context.Context, kind EventType, runID string, sessionID string, payload any) error {
	emitCtx := ctx
	cancel := func() {}
	if _, hasDeadline := emitCtx.Deadline(); !hasDeadline {
		emitCtx, cancel = context.WithTimeout(emitCtx, prepareEventEmitTimeout)
	}
	defer cancel()

	if err := s.emit(emitCtx, kind, runID, sessionID, payload); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

type sessionInputPreparer struct {
	preparer *agentsession.InputPreparer
}

type sessionAssetLimitStore interface {
	SetAssetPolicy(policy agentsession.AssetPolicy)
}

// Prepare 将 runtime 输入 DTO 映射到 session 子层并返回标准 UserInput 结果。
func (p sessionInputPreparer) Prepare(
	ctx context.Context,
	input PrepareInput,
	defaultWorkdir string,
) (PreparedInputResult, error) {
	if p.preparer == nil {
		return PreparedInputResult{}, errors.New("runtime: session input preparer is nil")
	}

	sessionImages := make([]agentsession.PrepareImageInput, 0, len(input.Images))
	for _, image := range input.Images {
		sessionImages = append(sessionImages, agentsession.PrepareImageInput{
			Path:     strings.TrimSpace(image.Path),
			MimeType: strings.TrimSpace(image.MimeType),
		})
	}

	prepared, err := p.preparer.Prepare(ctx, agentsession.PrepareInput{
		SessionID:        strings.TrimSpace(input.SessionID),
		Text:             input.Text,
		Images:           sessionImages,
		DefaultWorkdir:   strings.TrimSpace(defaultWorkdir),
		RequestedWorkdir: strings.TrimSpace(input.Workdir),
	})
	if err != nil {
		return PreparedInputResult{}, err
	}

	if len(prepared.Parts) == 0 {
		return PreparedInputResult{}, fmt.Errorf("runtime: prepared parts is empty")
	}

	return PreparedInputResult{
		UserInput: UserInput{
			SessionID: strings.TrimSpace(prepared.SessionID),
			RunID:     strings.TrimSpace(input.RunID),
			Parts:     prepared.Parts,
			Workdir:   strings.TrimSpace(prepared.Workdir),
			Mode:      strings.TrimSpace(input.Mode),
		},
		SavedAssets: append([]agentsession.AssetMeta(nil), prepared.SavedAssets...),
	}, nil
}
