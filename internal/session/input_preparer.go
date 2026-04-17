package session

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const imageOnlySessionTitle = "Image Message"

// PrepareImageInput 表示一次用户输入中附带的本地图片引用。
type PrepareImageInput struct {
	Path     string
	MimeType string
}

// PrepareInput 定义会话输入归一化的领域输入参数。
type PrepareInput struct {
	SessionID        string
	Text             string
	Images           []PrepareImageInput
	DefaultWorkdir   string
	RequestedWorkdir string
}

// PreparedInput 表示归一化完成后可直接进入 runtime 的标准输入结果。
type PreparedInput struct {
	SessionID   string
	Workdir     string
	Parts       []providertypes.ContentPart
	SavedAssets []AssetMeta
}

// AssetSaveError 描述图片落盘阶段的结构化失败信息，便于上层统一事件化处理。
type AssetSaveError struct {
	SessionID string
	Index     int
	Path      string
	Err       error
}

func (e *AssetSaveError) Error() string {
	if e == nil {
		return "session: asset save failed"
	}
	if strings.TrimSpace(e.Path) == "" {
		return fmt.Sprintf("session: save asset at index %d: %v", e.Index, e.Err)
	}
	return fmt.Sprintf("session: save asset %q at index %d: %v", e.Path, e.Index, e.Err)
}

func (e *AssetSaveError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InputPreparer 负责把用户文本/图片输入归一化为会话级标准 parts。
type InputPreparer struct {
	store      Store
	assetStore AssetStore
}

type assetCleanupStore interface {
	DeleteAsset(ctx context.Context, sessionID string, assetID string) error
}

// NewInputPreparer 创建会话输入归一化组件。
func NewInputPreparer(store Store, assetStore AssetStore) *InputPreparer {
	return &InputPreparer{
		store:      store,
		assetStore: assetStore,
	}
}

// Prepare 负责会话解析/创建、附件落盘与 parts 组装。
func (p *InputPreparer) Prepare(ctx context.Context, input PrepareInput) (PreparedInput, error) {
	if err := ctx.Err(); err != nil {
		return PreparedInput{}, err
	}
	if p == nil || p.store == nil {
		return PreparedInput{}, fmt.Errorf("session: input preparer store is not configured")
	}
	if len(input.Images) > 0 && p.assetStore == nil {
		return PreparedInput{}, fmt.Errorf("session: asset store is not configured")
	}

	trimmedText := strings.TrimSpace(input.Text)
	if trimmedText == "" && len(input.Images) == 0 {
		return PreparedInput{}, fmt.Errorf("session: input content is empty")
	}

	sessionTitle := buildSessionTitle(trimmedText, len(input.Images) > 0)
	session, sessionCreated, pendingUpdate, err := p.loadOrCreateSession(
		ctx,
		input.SessionID,
		sessionTitle,
		input.DefaultWorkdir,
		input.RequestedWorkdir,
	)
	if err != nil {
		return PreparedInput{}, err
	}

	parts := make([]providertypes.ContentPart, 0, 1+len(input.Images))
	if trimmedText != "" {
		parts = append(parts, providertypes.NewTextPart(trimmedText))
	}

	savedAssets := make([]AssetMeta, 0, len(input.Images))
	for index, image := range input.Images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			p.rollbackCreatedSession(ctx, session.ID, sessionCreated)
			p.cleanupSavedAssets(ctx, session.ID, savedAssets)
			return PreparedInput{}, &AssetSaveError{
				SessionID: session.ID,
				Index:     index,
				Path:      path,
				Err:       fmt.Errorf("image path is empty"),
			}
		}
		mimeType := strings.TrimSpace(image.MimeType)

		meta, err := p.saveImageAsset(ctx, session.ID, session.Workdir, path, mimeType)
		if err != nil {
			p.rollbackCreatedSession(ctx, session.ID, sessionCreated)
			p.cleanupSavedAssets(ctx, session.ID, savedAssets)
			return PreparedInput{}, &AssetSaveError{
				SessionID: session.ID,
				Index:     index,
				Path:      path,
				Err:       err,
			}
		}
		savedAssets = append(savedAssets, meta)
		parts = append(parts, providertypes.NewSessionAssetImagePart(meta.ID, meta.MimeType))
	}

	if err := providertypes.ValidateParts(parts); err != nil {
		p.rollbackCreatedSession(ctx, session.ID, sessionCreated)
		p.cleanupSavedAssets(ctx, session.ID, savedAssets)
		return PreparedInput{}, fmt.Errorf("session: normalize parts: %w", err)
	}
	if err := p.persistSessionWorkdirUpdate(ctx, pendingUpdate); err != nil {
		p.rollbackCreatedSession(ctx, session.ID, sessionCreated)
		p.cleanupSavedAssets(ctx, session.ID, savedAssets)
		return PreparedInput{}, err
	}

	return PreparedInput{
		SessionID:   session.ID,
		Workdir:     session.Workdir,
		Parts:       parts,
		SavedAssets: savedAssets,
	}, nil
}

// saveImageAsset 按会话工作目录解析并校验图片路径后落盘，禁止越界访问工作目录外文件。
func (p *InputPreparer) saveImageAsset(
	ctx context.Context,
	sessionID string,
	workdir string,
	path string,
	mimeType string,
) (AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}

	absolutePath, err := resolveImagePath(workdir, path)
	if err != nil {
		return AssetMeta{}, err
	}
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}

	file, err := os.Open(absolutePath)
	if err != nil {
		return AssetMeta{}, fmt.Errorf("open image file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}

	resolvedMimeType, err := resolveImageMimeType(ctx, path, mimeType, file)
	if err != nil {
		return AssetMeta{}, err
	}
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}

	meta, err := p.assetStore.SaveAsset(ctx, sessionID, file, resolvedMimeType)
	if err != nil {
		return AssetMeta{}, err
	}
	return meta, nil
}

// resolveImageMimeType 解析图片 MIME 类型，仅允许 image/*，并要求声明值与文件头探测一致。
func resolveImageMimeType(ctx context.Context, path string, declared string, file *os.File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	detected, err := detectImageMimeTypeFromFile(ctx, file)
	if err != nil {
		return "", err
	}

	declaredMime := normalizeMimeType(declared)
	if declaredMime != "" {
		if !strings.HasPrefix(declaredMime, "image/") {
			return "", fmt.Errorf("declared mime type %q is not an image", declared)
		}
		if declaredMime != detected {
			return "", fmt.Errorf("declared mime type %q mismatches detected %q", declaredMime, detected)
		}
		return detected, nil
	}

	extMime := normalizeMimeType(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))))
	if extMime != "" && strings.HasPrefix(extMime, "image/") && extMime != detected {
		return "", fmt.Errorf("file extension mime %q mismatches detected %q", extMime, detected)
	}
	return detected, nil
}

// detectImageMimeTypeFromFile 根据文件头探测 MIME，且要求结果为 image/*。
func detectImageMimeTypeFromFile(ctx context.Context, file *os.File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	buffer := make([]byte, 512)
	n, readErr := file.Read(buffer)
	if readErr != nil && readErr != io.EOF {
		return "", fmt.Errorf("detect image mime type: %w", readErr)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("reset image reader: %w", err)
	}

	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(buffer[:n])))
	if strings.HasPrefix(detected, "image/") {
		return detected, nil
	}
	return "", fmt.Errorf("unsupported image format")
}

// normalizeMimeType 清洗 MIME 字符串并移除参数段，返回小写标准形式。
func normalizeMimeType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, ";"); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	return normalized
}

// resolveImagePath 以会话工作目录为基准解析图片路径并强制限制在工作目录内。
func resolveImagePath(workdir string, path string) (string, error) {
	base := strings.TrimSpace(workdir)
	if base == "" {
		return "", fmt.Errorf("resolve image path: workdir is empty")
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve image path base: %w", err)
	}

	target := strings.TrimSpace(path)
	if target == "" {
		return "", fmt.Errorf("resolve image path: path is empty")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseAbs, target)
	}

	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve image path: %w", err)
	}
	if err := ensurePathWithinBase(baseAbs, targetAbs); err != nil {
		return "", fmt.Errorf("resolve image path: %w", err)
	}

	resolved := targetAbs
	if linkTarget, linkErr := filepath.EvalSymlinks(targetAbs); linkErr == nil {
		if err := ensurePathWithinBase(baseAbs, linkTarget); err != nil {
			return "", fmt.Errorf("resolve image path: %w", err)
		}
		resolved = linkTarget
	}
	return resolved, nil
}

// sessionWorkdirUpdate 描述已有会话 workdir 的待提交变更，确保 Prepare 成功后再落盘。
type sessionWorkdirUpdate struct {
	session Session
	dirty   bool
}

func (p *InputPreparer) loadOrCreateSession(
	ctx context.Context,
	sessionID string,
	title string,
	defaultWorkdir string,
	requestedWorkdir string,
) (Session, bool, sessionWorkdirUpdate, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionWorkdir, err := resolveWorkdirForInput(defaultWorkdir, "", requestedWorkdir)
		if err != nil {
			return Session{}, false, sessionWorkdirUpdate{}, err
		}
		session := NewWithWorkdir(title, sessionWorkdir)
		if err := p.store.Save(ctx, &session); err != nil {
			return Session{}, false, sessionWorkdirUpdate{}, err
		}
		return session, true, sessionWorkdirUpdate{}, nil
	}

	session, err := p.store.Load(ctx, sessionID)
	if err != nil {
		return Session{}, false, sessionWorkdirUpdate{}, err
	}
	if strings.TrimSpace(requestedWorkdir) == "" && strings.TrimSpace(session.Workdir) != "" {
		return session, false, sessionWorkdirUpdate{}, nil
	}

	resolved, err := resolveWorkdirForInput(defaultWorkdir, session.Workdir, requestedWorkdir)
	if err != nil {
		return Session{}, false, sessionWorkdirUpdate{}, err
	}
	if session.Workdir == resolved {
		return session, false, sessionWorkdirUpdate{}, nil
	}

	session.Workdir = resolved
	session.UpdatedAt = time.Now()
	return session, false, sessionWorkdirUpdate{
		session: session,
		dirty:   true,
	}, nil
}

// rollbackCreatedSession 在本次 Prepare 新建会话后发生错误时回滚会话目录，避免残留孤儿会话。
func (p *InputPreparer) rollbackCreatedSession(ctx context.Context, sessionID string, created bool) {
	if !created {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	_ = p.store.DeleteSession(ctx, sessionID)
}

// persistSessionWorkdirUpdate 在 Prepare 其余步骤完成后统一提交会话 workdir 更新，避免失败时出现部分提交。
func (p *InputPreparer) persistSessionWorkdirUpdate(ctx context.Context, pending sessionWorkdirUpdate) error {
	if !pending.dirty {
		return nil
	}
	if err := p.store.Save(ctx, &pending.session); err != nil {
		return err
	}
	return nil
}

// cleanupSavedAssets 在 Prepare 失败时尽力回收已落盘的附件，减少 existing session 残留垃圾文件。
func (p *InputPreparer) cleanupSavedAssets(ctx context.Context, sessionID string, assets []AssetMeta) {
	if len(assets) == 0 || ctx.Err() != nil {
		return
	}
	cleanupStore, ok := p.assetStore.(assetCleanupStore)
	if !ok {
		return
	}
	for _, asset := range assets {
		if strings.TrimSpace(asset.ID) == "" {
			continue
		}
		_ = cleanupStore.DeleteAsset(ctx, sessionID, asset.ID)
	}
}

func resolveWorkdirForInput(defaultWorkdir string, currentWorkdir string, requestedWorkdir string) (string, error) {
	base := EffectiveWorkdir(currentWorkdir, defaultWorkdir)
	if strings.TrimSpace(requestedWorkdir) == "" {
		return ResolveExistingDir(base)
	}

	target := strings.TrimSpace(requestedWorkdir)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return ResolveExistingDir(target)
}

func buildSessionTitle(text string, hasImages bool) string {
	if strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	if hasImages {
		return imageOnlySessionTitle
	}
	return "New Session"
}
