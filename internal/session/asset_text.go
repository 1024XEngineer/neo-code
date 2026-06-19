package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// AssetTextLoadError 描述加载 session 文本 asset 时出现的可恢复错误。
// Reason 区分错误类别（utf8/empty/exceeded/io）；AssetID 便于上层回传给前端定位。
type AssetTextLoadError struct {
	AssetID string
	Reason  string
	Err     error
}

// Error 实现 error 接口。
func (e *AssetTextLoadError) Error() string {
	if e == nil {
		return ""
	}
	base := "session: load text asset"
	if e.AssetID != "" {
		base = fmt.Sprintf("%s %q", base, e.AssetID)
	}
	if e.Reason != "" {
		base = fmt.Sprintf("%s: %s", base, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", base, e.Err)
	}
	return base
}

// Unwrap 支持 errors.Is / errors.As。
func (e *AssetTextLoadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// TextAssetLoadResult 是 LoadTextAsset 成功加载并截断后的输出。
// Content 是按 UTF-8 解码、字节与字符双阈值兜底后的内容（若被截断，会在末尾追加截断提示）。
// Truncated 为 true 时 Content 已包含截断提示；OriginalBytes 是从 AssetStore 读到的原始字节数
// （受 LimitReader 控制）；KeptChars 是截断后、附加提示前的内容字符数；TotalChars 是最终 Content 的字符数。
type TextAssetLoadResult struct {
	Content       string
	Truncated     bool
	OriginalBytes int64
	KeptChars     int
	TotalChars    int
}

// TextAssetLoadOptions 描述加载文本 asset 时的可调参数。
// FileName 仅用于截断提示的展示，会通过 SanitizeTextAssetFileName 做安全清洗，不作为可信路径。
type TextAssetLoadOptions struct {
	FileName        string
	FallbackName    string
	IncludeBoundary bool // 是否在内容首尾追加 "<file name=...>" 边界；默认 false，runtime 层决定。
}

// LoadTextAsset 从 store 中读取指定文本 asset，按 UTF-8 校验并按 policy 截断。
// 返回结构化结果与错误；非 UTF-8 / 空 / IO 错误都会以 AssetTextLoadError 形式返回。
func LoadTextAsset(
	ctx context.Context,
	store AssetStore,
	sessionID string,
	assetID string,
	policy TextAssetPolicy,
	opts TextAssetLoadOptions,
) (TextAssetLoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "ctx-canceled", Err: err}
	}
	if store == nil {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "store-nil", Err: errors.New("asset store is nil")}
	}
	if assetID == "" {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "missing-asset-id", Err: errors.New("asset id is empty")}
	}
	normalized := NormalizeTextAssetPolicy(policy)
	// 调用方提供的 raw policy 本身就空白名单时直接拒，不应用默认值（与 issue 风险节"白名单关闭=拒"对齐）。
	// 注意：NormalizeTextAssetPolicy 会把空白名单填为默认白名单，因此必须在 normalize 之前检查 raw policy。
	if policy.Whitelist.IsEmpty() {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "whitelist-empty", Err: errors.New("text asset whitelist is empty")}
	}

	rc, meta, err := store.Open(ctx, sessionID, assetID)
	if err != nil {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "open", Err: err}
	}
	defer func() { _ = rc.Close() }()

	// 字节上限保护：避免一次读入超大数据导致 OOM。
	// +1 是为了让"正好等于上限"的场景也能区分"刚好"和"超限"。
	raw, err := io.ReadAll(io.LimitReader(bufio.NewReader(rc), normalized.MaxTextAssetBytes+1))
	if err != nil {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "io", Err: err}
	}
	if err := ctx.Err(); err != nil {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "ctx-canceled", Err: err}
	}
	if int64(len(raw)) == 0 {
		return TextAssetLoadResult{}, &AssetTextLoadError{AssetID: assetID, Reason: "empty", Err: errors.New("asset payload is empty")}
	}
	truncated := false
	if int64(len(raw)) > normalized.MaxTextAssetBytes {
		raw = raw[:normalized.MaxTextAssetBytes]
		truncated = true
	}

	// 字符级截断（按 rune 计数）。先做 UTF-8 校验以便给出明确错误。
	if !utf8.Valid(raw) {
		return TextAssetLoadResult{}, &AssetTextLoadError{
			AssetID: assetID,
			Reason:  "utf8",
			Err:     fmt.Errorf("asset payload is not valid UTF-8 (mime=%q, bytes=%d)", meta.MimeType, len(raw)),
		}
	}
	content, charTruncated := truncateByRuneCount(string(raw), normalized.MaxTextAssetChars)
	if charTruncated {
		truncated = true
	}
	// KeptChars 反映"截断后、附加提示前"的原内容字符数；TotalChars 反映最终返回 Content 的字符数。
	keptChars := utf8.RuneCountInString(content)
	originalBytes := int64(len(raw))

	displayName := SanitizeTextAssetFileName(opts.FileName, opts.FallbackName)
	if displayName == "" {
		displayName = SanitizeTextAssetFileName(meta.MimeType, "text-asset")
	}

	if truncated {
		content = content + fmt.Sprintf(
			"\n\n[truncated: original=%d bytes, kept=%d chars; filename=%s]",
			originalBytes, keptChars, displayName,
		)
	}

	return TextAssetLoadResult{
		Content:       content,
		Truncated:     truncated,
		OriginalBytes: originalBytes,
		KeptChars:     keptChars,
		TotalChars:    utf8.RuneCountInString(content),
	}, nil
}

// truncateByRuneCount 按 UTF-8 rune 数截断字符串；返回 (截断后, 是否截断)。
// 仅在字符数超过上限时触发；不会破坏多字节字符边界。
func truncateByRuneCount(s string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return "", true
	}
	count := utf8.RuneCountInString(s)
	if count <= maxChars {
		return s, false
	}
	var b []byte
	runeIdx := 0
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if runeIdx >= maxChars {
			break
		}
		b = append(b, s[i:i+size]...)
		i += size
		runeIdx++
	}
	return string(b), true
}
