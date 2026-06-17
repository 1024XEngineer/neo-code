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
// 文本附件策略（textPolicy）由 PrepareUserInput 在每次调用时通过 sessionTextPolicyInjectable
// 重新注入；构造阶段使用 session 默认值兜底，避免未配置时走到 nil 路径。
func NewSessionInputPreparer(store agentsession.Store, assetStore agentsession.AssetStore) UserInputPreparer {
	preparer := agentsession.NewInputPreparer(store, assetStore)
	preparer.SetTextAssetPolicy(agentsession.DefaultTextAssetPolicy())
	return sessionInputPreparer{
		preparer: preparer,
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
	textAssetPolicy := agentsession.DefaultTextAssetPolicy()
	textAssetEnabled := true
	if s.configManager != nil {
		cfg := s.configManager.Get()
		defaultWorkdir = strings.TrimSpace(cfg.Workdir)
		sessionAssetPolicy = cfg.Runtime.ResolveSessionAssetPolicy()
		textAssetPolicy = cfg.Runtime.ResolveTextAssetPolicy()
		textAssetEnabled = cfg.Runtime.IsTextAssetEnabled()
	}
	if limitAwareStore, ok := s.sessionAssetStore.(sessionAssetLimitStore); ok {
		limitAwareStore.SetAssetPolicy(sessionAssetPolicy)
		// 同步设置文本附件策略（与图片策略互不影响，按 mime 路由）。
		if textAwareStore, okText := s.sessionAssetStore.(sessionTextAssetLimitStore); okText {
			textAwareStore.SetTextAssetPolicy(textAssetPolicy)
		}
	}

	// 同步把 text policy 注入到 user input preparer，让文本 asset 能被解析。
	if injectable, ok := s.userInputPreparer.(sessionTextPolicyInjectable); ok {
		injectable.SetTextAssetPolicy(textAssetPolicy)
	}

	prepared, err := s.userInputPreparer.Prepare(ctx, input, defaultWorkdir)
	if err != nil {
		_ = s.emitPrepareFailure(ctx, input, err)
		return UserInput{}, err
	}

	// 文本附件内联：在提交会话前把 prepared.Parts 里的文本 asset 读取并替换为 text part。
	// 关闭开关时跳过内联，并把文本 asset 的 image part 丢弃，避免非 image/* mime 进入 provider 失败。
	runID := strings.TrimSpace(input.RunID)
	textAssetCount := 0
	if textAssetEnabled {
		inlineResult := s.inlineUserInputTextAssets(ctx, prepared.UserInput.SessionID, input, prepared.UserInput.Parts, textAssetPolicy)
		prepared.UserInput.Parts = inlineResult.Parts
		textAssetCount = inlineResult.Inlined
	} else {
		// text_asset_enabled=false：丢弃文本 asset image part，emit EventError 告知用户。
		prepared.UserInput.Parts = dropTextAssetImageParts(prepared.UserInput.Parts, textAssetPolicy, func(assetID string, mime string) {
			_ = s.emitPrepareEvent(ctx, EventError, runID, prepared.UserInput.SessionID,
				"text asset dropped (text_asset_enabled=false): asset_id="+assetID+" mime="+mime)
		})
	}

	_ = s.emitPrepareEvent(ctx, EventInputNormalized, runID, prepared.UserInput.SessionID, InputNormalizedPayload{
		TextLength:     len([]rune(strings.TrimSpace(input.Text))),
		ImageCount:     len(input.Images),
		TextAssetCount: textAssetCount,
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

// SetTextAssetPolicy 注入文本附件策略到内部 session.InputPreparer。
// 该方法用于实现 sessionTextPolicyInjectable 接口，注入过程对调用方透明。
func (p sessionInputPreparer) SetTextAssetPolicy(policy agentsession.TextAssetPolicy) {
	if p.preparer == nil {
		return
	}
	p.preparer.SetTextAssetPolicy(policy)
}

type sessionAssetLimitStore interface {
	SetAssetPolicy(policy agentsession.AssetPolicy)
}

type sessionTextAssetLimitStore interface {
	SetTextAssetPolicy(policy agentsession.TextAssetPolicy)
}

// sessionTextPolicyInjectable 表示 user input preparer 支持运行时注入文本附件策略。
// 引入这个内部接口是为了在不改动 UserInputPreparer 公开契约的前提下注入配置。
type sessionTextPolicyInjectable interface {
	SetTextAssetPolicy(policy agentsession.TextAssetPolicy)
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
			AssetID:  strings.TrimSpace(image.AssetID),
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
			SessionID:        strings.TrimSpace(prepared.SessionID),
			RunID:            strings.TrimSpace(input.RunID),
			Parts:            prepared.Parts,
			Workdir:          strings.TrimSpace(prepared.Workdir),
			Mode:             strings.TrimSpace(input.Mode),
			DisableTools:     input.DisableTools,
			ThinkingOverride: cloneThinkingOverride(input.ThinkingOverride),
		},
		SavedAssets: append([]agentsession.AssetMeta(nil), prepared.SavedAssets...),
	}, nil
}
