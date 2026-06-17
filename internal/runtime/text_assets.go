package runtime

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// textAssetInlineResult 描述一次文本附件内联的统计结果，供上层事件与日志使用。
type textAssetInlineResult struct {
	Parts     []providertypes.ContentPart
	Inlined   int
	Truncated int
	Failed    int
}

// inlineUserInputTextAssets 包装 inlineTextSessionAssets，并把失败回写到 prepare 事件。
// 返回新的 parts 切片与内联统计；PrepareUserInput 用 Inlined 作为 TextAssetCount 上报。
func (s *Service) inlineUserInputTextAssets(
	ctx context.Context,
	sessionID string,
	input PrepareInput,
	parts []providertypes.ContentPart,
	policy agentsession.TextAssetPolicy,
) textAssetInlineResult {
	if s == nil {
		return textAssetInlineResult{Parts: parts}
	}
	runID := strings.TrimSpace(input.RunID)
	fileNames := textAssetFileNameMap(input.Images)
	normalized := agentsession.NormalizeTextAssetPolicy(policy)
	newParts, detail := inlineTextSessionAssets(
		ctx,
		s.sessionAssetStore,
		sessionID,
		parts,
		normalized,
		fileNames,
		func(assetID string, err error) {
			// 文本附件内联失败通过 EventError 上报；不引入新事件类型，避免协议扩散。
			_ = s.emitPrepareEvent(ctx, EventError, runID, sessionID, "text asset inline failed: "+err.Error())
		},
	)
	return textAssetInlineResult{
		Parts:     newParts,
		Inlined:   detail.Inlined,
		Truncated: detail.Truncated,
		Failed:    detail.Failed,
	}
}

// inlineTextSessionAssets 把 prepared.Parts 里 mime 属于会话文本白名单的 session_asset image
// part 读取后替换为 text part，让 Provider 完全不感知"文件"概念。
// 不在白名单内的 image part 保持原样，函数对非 asset 类型 part（text、remote image）透明。
//
// 行为细节：
//   - 每遇到一个目标 part，调用 session.LoadTextAsset 做 UTF-8 校验 + 字节/字符双阈值截断。
//   - 成功 → 用 NewTextPart(content) 替换原 part。
//   - 失败 → 调用者提供的 onError 钩子被触发（用于 emit 事件），该 part 被丢弃（避免把坏数据送给 provider）。
//   - onError 为 nil 时失败静默丢弃。
func inlineTextSessionAssets(
	ctx context.Context,
	store agentsession.AssetStore,
	sessionID string,
	parts []providertypes.ContentPart,
	policy agentsession.TextAssetPolicy,
	originalFileNames map[string]string,
	onError func(assetID string, err error),
) ([]providertypes.ContentPart, textAssetInlineResult) {
	result := textAssetInlineResult{}
	if store == nil || len(parts) == 0 {
		return parts, result
	}
	normalized := agentsession.NormalizeTextAssetPolicy(policy)
	if normalized.Whitelist.IsEmpty() {
		return parts, result
	}
	out := make([]providertypes.ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartImage || part.Image == nil {
			out = append(out, part)
			continue
		}
		// 只对 session asset 类的 image part 做内联，remote URL 直接保留。
		if part.Image.SourceType != providertypes.ImageSourceSessionAsset {
			out = append(out, part)
			continue
		}
		if part.Image.Asset == nil || strings.TrimSpace(part.Image.Asset.ID) == "" {
			out = append(out, part)
			continue
		}
		assetMime := strings.TrimSpace(part.Image.Asset.MimeType)
		if !normalized.Whitelist.LookupByMime(assetMime) {
			out = append(out, part)
			continue
		}
		opts := agentsession.TextAssetLoadOptions{
			FallbackName: assetMime,
		}
		if originalFileNames != nil {
			if name, ok := originalFileNames[part.Image.Asset.ID]; ok {
				opts.FileName = name
			}
		}
		loadResult, err := agentsession.LoadTextAsset(ctx, store, sessionID, part.Image.Asset.ID, normalized, opts)
		if err != nil {
			result.Failed++
			if onError != nil {
				onError(part.Image.Asset.ID, err)
			}
			// 失败 part 直接丢弃，不影响其它 part。
			continue
		}
		result.Inlined++
		if loadResult.Truncated {
			result.Truncated++
		}
		out = append(out, providertypes.NewTextPart(loadResult.Content))
	}
	result.Parts = out
	return out, result
}

// textAssetFileNameMap 把 input.Images 里携带的路径映射为 asset_id → 文件名，
// 供 inlineTextSessionAssets 在截断提示里展示原始文件名。
// 当 runtime 输入未携带文件名时返回 nil（fallback 到 mime 名称）。
func textAssetFileNameMap(images []UserImageInput) map[string]string {
	if len(images) == 0 {
		return nil
	}
	out := make(map[string]string, len(images))
	for _, img := range images {
		if strings.TrimSpace(img.AssetID) == "" {
			continue
		}
		if name := strings.TrimSpace(img.Path); name != "" {
			out[img.AssetID] = name
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dropTextAssetImageParts 在 text_asset_enabled=false 时把 prepared.Parts 里属于文本白名单的
// session_asset image part 丢弃，避免非 image/* mime 进入 provider 的 image-source resolver 失败。
// 复用 inlineTextSessionAssets 的识别条件（ContentPartImage + ImageSourceSessionAsset + whitelist），
// 但丢弃而非读取替换。onDropped 钩子用于 emit 事件告知调用方，可为 nil。
func dropTextAssetImageParts(
	parts []providertypes.ContentPart,
	policy agentsession.TextAssetPolicy,
	onDropped func(assetID string, mime string),
) []providertypes.ContentPart {
	if len(parts) == 0 {
		return parts
	}
	normalized := agentsession.NormalizeTextAssetPolicy(policy)
	if normalized.Whitelist.IsEmpty() {
		return parts
	}
	out := make([]providertypes.ContentPart, 0, len(parts))
	for _, part := range parts {
		// 只丢弃 session asset 类的文本 image part；图片 image part 和非 asset part 原样保留。
		if part.Kind == providertypes.ContentPartImage && part.Image != nil &&
			part.Image.SourceType == providertypes.ImageSourceSessionAsset &&
			part.Image.Asset != nil && strings.TrimSpace(part.Image.Asset.ID) != "" &&
			normalized.Whitelist.LookupByMime(part.Image.Asset.MimeType) {
			if onDropped != nil {
				onDropped(part.Image.Asset.ID, part.Image.Asset.MimeType)
			}
			continue
		}
		out = append(out, part)
	}
	return out
}
