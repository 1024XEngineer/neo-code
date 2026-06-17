package session

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// MaxSessionAssetBytes 定义 session_asset 在读写链路中的统一大小上限（20 MiB）。
	MaxSessionAssetBytes int64 = 20 * 1024 * 1024
	// DefaultMaxTextAssetBytes 定义 session 文本 asset 的默认字节上限（256 KiB）。
	// 该默认值用于避免单轮会话上下文被大体量文本文件击穿；硬上限由 MaxTextAssetBytesHardLimit 兜底。
	DefaultMaxTextAssetBytes int64 = 256 * 1024
	// MaxTextAssetBytesHardLimit 是文本 asset 字节上限的硬编码兜底，防止配置或调用方把上限放得过大。
	MaxTextAssetBytesHardLimit int64 = 4 * 1024 * 1024
	// DefaultMaxTextAssetChars 定义 session 文本 asset 的默认字符上限（约 25 万字符，按 UTF-8 解码后计数）。
	DefaultMaxTextAssetChars int = 250_000
	// MaxTextAssetCharsHardLimit 是文本 asset 字符上限的硬编码兜底。
	MaxTextAssetCharsHardLimit int = 4_000_000
	// MaxTextAssetFileNameBytes 定义文本 asset 文件名（用于上下文标签）的最大字节数。
	MaxTextAssetFileNameBytes int = 128
)

// AssetPolicy 描述 session_asset 在单文件维度的存储与读写策略。
type AssetPolicy struct {
	MaxSessionAssetBytes int64
}

// DefaultAssetPolicy 返回 session_asset 策略的默认值。
func DefaultAssetPolicy() AssetPolicy {
	return AssetPolicy{
		MaxSessionAssetBytes: MaxSessionAssetBytes,
	}
}

// NormalizeAssetPolicy 归一化 session_asset 策略并施加硬上限兜底。
func NormalizeAssetPolicy(policy AssetPolicy) AssetPolicy {
	normalized := policy
	if normalized.MaxSessionAssetBytes <= 0 {
		normalized.MaxSessionAssetBytes = MaxSessionAssetBytes
	}
	if normalized.MaxSessionAssetBytes > MaxSessionAssetBytes {
		normalized.MaxSessionAssetBytes = MaxSessionAssetBytes
	}
	return normalized
}

// TextAssetWhitelist 描述文本类附件的扩展名与 MIME 双向白名单。
// 同时提供按扩展名（无 "." 前缀、小写）和按 MIME（小写）查询的能力，
// 允许调用方在不修改核心代码的前提下扩展支持类型（与默认项做 union 合并）。
type TextAssetWhitelist struct {
	// extensionToMime 记录扩展名到 MIME 的映射。
	extensionToMime map[string]string
	// mimeSet 记录支持的全部 MIME 集合，便于按 MIME 反向查扩展名。
	mimeSet map[string]struct{}
}

// DefaultTextAssetWhitelist 返回内置的文本附件白名单（与 issue #701 验收清单对齐）。
func DefaultTextAssetWhitelist() TextAssetWhitelist {
	return NewTextAssetWhitelist(map[string]string{
		"txt":  "text/plain",
		"md":   "text/markdown",
		"json": "application/json",
		"yaml": "text/yaml",
		"yml":  "application/x-yaml",
		"csv":  "text/csv",
	})
}

// NewTextAssetWhitelist 通过扩展名→MIME 映射构造白名单；空映射会得到一个空的、拒绝一切的实例。
func NewTextAssetWhitelist(extensionToMime map[string]string) TextAssetWhitelist {
	normalized := make(map[string]string, len(extensionToMime))
	mimes := make(map[string]struct{}, len(extensionToMime))
	for ext, mime := range extensionToMime {
		cleanExt := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, ".")))
		cleanMime := strings.ToLower(strings.TrimSpace(mime))
		if cleanExt == "" || cleanMime == "" {
			continue
		}
		normalized[cleanExt] = cleanMime
		mimes[cleanMime] = struct{}{}
	}
	return TextAssetWhitelist{
		extensionToMime: normalized,
		mimeSet:         mimes,
	}
}

// WithExtensions 在现有白名单上追加扩展名→MIME 项，返回新的白名单实例（不可变）。
// 重复键以 base 为准；返回的实例不与 base 共享内部 map。
func (w TextAssetWhitelist) WithExtensions(extensionToMime map[string]string) TextAssetWhitelist {
	merged := make(map[string]string, len(w.extensionToMime)+len(extensionToMime))
	for k, v := range w.extensionToMime {
		merged[k] = v
	}
	for k, v := range extensionToMime {
		cleanExt := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(k, ".")))
		cleanMime := strings.ToLower(strings.TrimSpace(v))
		if cleanExt == "" || cleanMime == "" {
			continue
		}
		merged[cleanExt] = cleanMime
	}
	mimes := make(map[string]struct{}, len(merged))
	for _, mime := range merged {
		mimes[mime] = struct{}{}
	}
	return TextAssetWhitelist{extensionToMime: merged, mimeSet: mimes}
}

// IsEmpty 报告白名单是否为空。空白名单会拒绝所有文本 asset。
func (w TextAssetWhitelist) IsEmpty() bool {
	return len(w.extensionToMime) == 0
}

// LookupByExtension 按文件名（不区分大小写，自动去除路径分隔符）解析扩展名对应的 MIME；未命中返回空。
func (w TextAssetWhitelist) LookupByExtension(fileName string) string {
	ext := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(fileName)), ".")))
	if ext == "" {
		return ""
	}
	return w.extensionToMime[ext]
}

// LookupByMime 按 MIME（不区分大小写）判断是否在白名单中。
func (w TextAssetWhitelist) LookupByMime(mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "" {
		return false
	}
	_, ok := w.mimeSet[mime]
	return ok
}

// Extensions 返回当前白名单包含的全部扩展名（只读快照）。
func (w TextAssetWhitelist) Extensions() []string {
	if len(w.extensionToMime) == 0 {
		return nil
	}
	out := make([]string, 0, len(w.extensionToMime))
	for ext := range w.extensionToMime {
		out = append(out, ext)
	}
	return out
}

// TextAssetPolicy 描述 session 文本类 asset 的存储与读取策略。
type TextAssetPolicy struct {
	// Whitelist 是文本 asset 的扩展名/MIME 白名单。
	Whitelist TextAssetWhitelist
	// MaxTextAssetBytes 是单个文本 asset 的字节上限。
	MaxTextAssetBytes int64
	// MaxTextAssetChars 是单个文本 asset 在 UTF-8 解码后允许保留的最大字符数。
	MaxTextAssetChars int
}

// DefaultTextAssetPolicy 返回 session 文本 asset 策略的默认值。
func DefaultTextAssetPolicy() TextAssetPolicy {
	return TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: DefaultMaxTextAssetBytes,
		MaxTextAssetChars: DefaultMaxTextAssetChars,
	}
}

// NormalizeTextAssetPolicy 归一化文本 asset 策略并施加硬上限兜底。
// 零值或负值回填默认值；超过硬上限的值会被截到硬上限。
func NormalizeTextAssetPolicy(policy TextAssetPolicy) TextAssetPolicy {
	normalized := policy
	if normalized.Whitelist.IsEmpty() {
		normalized.Whitelist = DefaultTextAssetWhitelist()
	}
	if normalized.MaxTextAssetBytes <= 0 {
		normalized.MaxTextAssetBytes = DefaultMaxTextAssetBytes
	}
	if normalized.MaxTextAssetBytes > MaxTextAssetBytesHardLimit {
		normalized.MaxTextAssetBytes = MaxTextAssetBytesHardLimit
	}
	if normalized.MaxTextAssetChars <= 0 {
		normalized.MaxTextAssetChars = DefaultMaxTextAssetChars
	}
	if normalized.MaxTextAssetChars > MaxTextAssetCharsHardLimit {
		normalized.MaxTextAssetChars = MaxTextAssetCharsHardLimit
	}
	return normalized
}

// SanitizeTextAssetFileName 把原始文件名清洗为"只用于上下文标签"的安全字符串。
// 处理项：去路径分隔符、控制字符、引号、反引号；裁剪到 MaxTextAssetFileNameBytes；空值返回 fallback。
// 注意：返回值**只作为文本内容里的展示标签**，禁止被当作文件系统路径或受信字符串使用。
func SanitizeTextAssetFileName(raw string, fallback string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		cleaned = strings.TrimSpace(fallback)
	}
	if cleaned == "" {
		return ""
	}
	// 仅取 basename（按 '/' 或 '\\' 切分最后一段），避免目录穿越/绝对路径泄漏到标签。
	// 使用纯字符串处理，不依赖 filepath.Base —— 后者在不同 OS 上对控制字符和末尾分隔符行为不一致。
	if idx := strings.LastIndexAny(cleaned, "/\\"); idx >= 0 {
		cleaned = cleaned[idx+1:]
	}
	// 替换路径分隔符与控制字符为下划线（控制字符单独走一遍确保万无一失）。
	var b strings.Builder
	b.Grow(len(cleaned))
	for _, r := range cleaned {
		switch {
		case r == '/' || r == '\\':
			b.WriteByte('_')
		case r < 0x20 || r == 0x7f:
			b.WriteByte('_')
		case r == '"' || r == '\'' || r == '`':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return ""
	}
	if len(out) > MaxTextAssetFileNameBytes {
		// 按 rune 逐步缩减到字节上限内，保留完整 UTF-8 字符边界，
		// 避免在多字节字符（如中文）中间切断产生非法 UTF-8。
		runes := []rune(out)
		cut := len(runes)
		for cut > 0 && len(string(runes[:cut])) > MaxTextAssetFileNameBytes {
			cut--
		}
		out = string(runes[:cut])
	}
	return out
}

// String 返回当前白名单包含的全部 (扩展名 → MIME) 对，用于错误信息或调试输出。
func (w TextAssetWhitelist) String() string {
	if w.IsEmpty() {
		return "empty"
	}
	parts := make([]string, 0, len(w.extensionToMime))
	for ext, mime := range w.extensionToMime {
		parts = append(parts, fmt.Sprintf("%s→%s", ext, mime))
	}
	return strings.Join(parts, ",")
}
