package tui

import (
	"fmt"
	tuiinfra "neo-code/internal/tui/infra"
	tuiservices "neo-code/internal/tui/services"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	fileReferencePrefix  = "@"
	imageReferencePrefix = "@image:"
	imageReferenceUsage  = "@image:<path>"
	fileMenuTitle        = "Files"
	maxWorkspaceFiles    = 4000
	maxFileSuggestions   = 6
	maxImageAttachments  = 3
)

type tokenSelector int

const (
	tokenSelectorFirst tokenSelector = iota
	tokenSelectorLast
)

var readClipboardImage = tuiinfra.ReadClipboardImage
var readClipboardText = tuiinfra.ReadClipboardText
var saveClipboardImageToTempFile = tuiinfra.SaveImageToTempFile

func (a *App) refreshFileCandidates() error {
	candidates, err := collectWorkspaceFiles(a.state.CurrentWorkdir, maxWorkspaceFiles)
	if err != nil {
		return err
	}
	a.fileCandidates = candidates
	if absolute := tuiservices.ResolveWorkspaceDirectory(a.state.CurrentWorkdir); absolute != "" {
		a.fileBrowser.CurrentDirectory = absolute
	}
	a.refreshCommandMenu()
	return nil
}

func collectWorkspaceFiles(root string, limit int) ([]string, error) {
	return tuiservices.CollectWorkspaceFiles(root, limit)
}

func (a App) resolveFileReferenceSuggestions(input string) (start int, end int, query string, suggestions []string, ok bool) {
	start, end, token, ok := currentReferenceToken(input)
	if !ok {
		return 0, 0, "", nil, false
	}

	query = strings.ToLower(strings.TrimPrefix(token, fileReferencePrefix))
	suggestions = collectFileSuggestionMatches(query, a.fileCandidates, maxFileSuggestions)
	return start, end, query, suggestions, true
}

func collectFileSuggestionMatches(query string, candidates []string, limit int) []string {
	return tuiservices.SuggestFileMatches(query, candidates, limit)
}

func tokenRange(input string, selector tokenSelector) (start int, end int, token string, ok bool) {
	if strings.TrimSpace(input) == "" {
		return 0, 0, "", false
	}

	switch selector {
	case tokenSelectorFirst:
		start = 0
		for start < len(input) {
			switch input[start] {
			case ' ', '\t', '\r', '\n':
				start++
			default:
				goto parse
			}
		}
		return 0, 0, "", false
	case tokenSelectorLast:
		end = len(input)
		start = strings.LastIndexAny(input, " \t\r\n")
		if start < 0 {
			start = 0
		} else {
			start++
		}
		if start >= end {
			return 0, 0, "", false
		}
		token = input[start:end]
		return start, end, token, true
	default:
		return 0, 0, "", false
	}

parse:
	end = start
	for end < len(input) {
		switch input[end] {
		case ' ', '\t', '\r', '\n':
			token = input[start:end]
			return start, end, token, true
		default:
			end++
		}
	}
	token = input[start:end]
	return start, end, token, true
}

func currentReferenceToken(input string) (start int, end int, token string, ok bool) {
	start, end, token, ok = tokenRange(input, tokenSelectorLast)
	if !ok {
		return 0, 0, "", false
	}
	if !strings.HasPrefix(token, fileReferencePrefix) && !strings.HasPrefix(token, imageReferencePrefix) {
		return 0, 0, "", false
	}
	return start, end, token, true
}

func (a *App) applyImageReference(input string) error {
	path := extractImageReference(input)
	if path == "" {
		return fmt.Errorf("invalid image reference")
	}
	return a.addImageAttachment(path)
}

// absorbInlineImageReferences 会把输入文本中的 @image:<path> 令牌吸收到附件队列，并返回移除令牌后的文本。
// 该实现保留原始空白布局，仅移除命中的图片令牌，避免改变用户提示词语义。
func (a *App) absorbInlineImageReferences(input string) (string, int, error) {
	if strings.TrimSpace(input) == "" {
		return strings.TrimSpace(input), 0, nil
	}

	var builder strings.Builder
	absorbed := 0
	for i := 0; i < len(input); {
		imagePath, end, ok := parseInlineImageReferenceAt(input, i)
		if ok && looksLikeImagePath(imagePath) {
			if err := a.queueImageAttachmentForPrepare(imagePath); err != nil {
				return "", absorbed, err
			}
			absorbed++
			i = end
			continue
		}
		builder.WriteByte(input[i])
		i++
	}

	return strings.TrimSpace(builder.String()), absorbed, nil
}

// isInlineTokenSpace 判断字符是否属于输入令牌分隔空白字符。
func isInlineTokenSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

// parseInlineImagePathToken 识别 @image:<path> 形式的图片路径令牌，并映射为待发送路径。
func (a *App) parseInlineImagePathToken(token string) (string, bool) {
	path, _, ok := parseInlineImageReferenceAt(strings.TrimSpace(token), 0)
	if !ok {
		return "", false
	}
	path = strings.TrimSpace(path)
	if path == "" || !looksLikeImagePath(path) {
		return "", false
	}

	resolved := path
	if !filepath.IsAbs(resolved) {
		base := strings.TrimSpace(a.state.CurrentWorkdir)
		if base == "" {
			return "", false
		}
		resolved = filepath.Join(base, resolved)
	}
	return resolved, true
}

// parseInlineImageReferenceAt 从输入指定位置解析 @image:<path>，支持引号与空格路径。
func parseInlineImageReferenceAt(input string, start int) (path string, end int, ok bool) {
	if start < 0 || start >= len(input) {
		return "", 0, false
	}
	if start > 0 && !isInlineTokenSpace(input[start-1]) {
		return "", 0, false
	}
	if !strings.HasPrefix(input[start:], imageReferencePrefix) {
		return "", 0, false
	}

	cursor := start + len(imageReferencePrefix)
	if cursor >= len(input) {
		return "", 0, false
	}

	quotedPath, quotedEnd, quoted := readQuotedInlinePath(input, cursor)
	if quoted {
		if strings.TrimSpace(quotedPath) == "" {
			return "", 0, false
		}
		return strings.TrimSpace(quotedPath), quotedEnd, true
	}

	unquotedPath, unquotedEnd := readUnquotedInlinePath(input, cursor)
	unquotedPath = strings.TrimSpace(unquotedPath)
	if unquotedPath == "" {
		return "", 0, false
	}
	return unquotedPath, unquotedEnd, true
}

// readQuotedInlinePath 读取带引号路径，支持 \" 和 \' 转义。
func readQuotedInlinePath(input string, start int) (string, int, bool) {
	if start >= len(input) {
		return "", 0, false
	}
	quote := input[start]
	if quote != '"' && quote != '\'' {
		return "", 0, false
	}
	var builder strings.Builder
	for i := start + 1; i < len(input); i++ {
		ch := input[i]
		if ch == '\\' && i+1 < len(input) {
			next := input[i+1]
			if next == quote || next == '\\' {
				builder.WriteByte(next)
				i++
				continue
			}
		}
		if ch == quote {
			return builder.String(), i + 1, true
		}
		builder.WriteByte(ch)
	}
	return "", 0, false
}

// readUnquotedInlinePath 读取非引号路径，遇到空白或换行结束，支持反斜杠转义空白字符。
func readUnquotedInlinePath(input string, start int) (string, int) {
	var builder strings.Builder
	end := start
	for end < len(input) {
		ch := input[end]
		if isInlineTokenSpace(ch) {
			break
		}
		if ch == '\\' && end+1 < len(input) {
			next := input[end+1]
			if isInlineTokenSpace(next) {
				builder.WriteByte(next)
				end += 2
				continue
			}
		}
		builder.WriteByte(ch)
		end++
	}
	return builder.String(), end
}

// queueImageAttachmentForPrepare 将图片路径排队为待发送附件，不在 TUI 层做文件系统和 MIME 硬校验。
// 真正的可用性校验与错误语义统一在 runtime/session 归一化阶段完成。
func (a *App) queueImageAttachmentForPrepare(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("image path is empty")
	}
	if len(a.pendingImageAttachments) >= maxImageAttachments {
		return fmt.Errorf("maximum %d image attachments allowed", maxImageAttachments)
	}

	resolved := path
	if !filepath.IsAbs(resolved) {
		base := strings.TrimSpace(a.state.CurrentWorkdir)
		if base != "" {
			resolved = filepath.Join(base, resolved)
		}
	}
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("invalid image path: %w", err)
	}

	a.pendingImageAttachments = append(a.pendingImageAttachments, pendingImageAttachment{
		Path:     absPath,
		MimeType: "",
		Size:     0,
		Name:     filepath.Base(absPath),
	})
	a.refreshImageAttachmentDisplay()
	return nil
}

// looksLikeImagePath 使用扩展名快速判断路径是否是常见图片文件。
func looksLikeImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}

func (a *App) applyFileReference(path string) error {
	reference, err := a.fileReferenceForPath(path)
	if err != nil {
		return err
	}

	current := a.input.Value()
	if start, end, _, ok := currentReferenceToken(current); ok {
		current = current[:start] + reference + current[end:]
	} else if strings.TrimSpace(current) == "" {
		current = reference
	} else {
		separator := " "
		if strings.HasSuffix(current, " ") || strings.HasSuffix(current, "\t") {
			separator = ""
		}
		current = current + separator + reference
	}

	a.input.SetValue(current)
	a.state.InputText = current
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	a.state.StatusText = fmt.Sprintf("[System] Added file reference %s.", reference)
	return nil
}

func (a App) fileReferenceForPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("file path is empty")
	}

	resolved := filepath.ToSlash(path)
	if workdir := strings.TrimSpace(a.state.CurrentWorkdir); workdir != "" {
		base, errBase := filepath.Abs(workdir)
		target, errTarget := filepath.Abs(path)
		if errBase == nil && errTarget == nil {
			if rel, errRel := filepath.Rel(base, target); errRel == nil && !strings.HasPrefix(rel, "..") {
				resolved = filepath.ToSlash(rel)
			} else {
				resolved = filepath.ToSlash(target)
			}
		}
	}
	resolved = strings.TrimPrefix(resolved, "./")
	return fileReferencePrefix + resolved, nil
}

func isImageReferenceInput(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), imageReferencePrefix)
}

func extractImageReference(input string) string {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, imageReferencePrefix) {
		return ""
	}
	return strings.TrimPrefix(trimmed, imageReferencePrefix)
}

func (a *App) addImageAttachment(path string) error {
	if err := a.queueImageAttachmentForPrepare(path); err != nil {
		return err
	}
	if count := len(a.pendingImageAttachments); count > 0 {
		a.state.StatusText = fmt.Sprintf("[System] Added image: %s", a.pendingImageAttachments[count-1].Name)
	}
	return nil
}

func (a *App) removeImageAttachment(index int) error {
	if index < 0 || index >= len(a.pendingImageAttachments) {
		return fmt.Errorf("invalid attachment index")
	}

	removed := a.pendingImageAttachments[index]
	a.pendingImageAttachments = append(a.pendingImageAttachments[:index], a.pendingImageAttachments[index+1:]...)

	a.refreshImageAttachmentDisplay()
	a.state.StatusText = fmt.Sprintf("[System] Removed image: %s", removed.Name)
	return nil
}

func (a *App) clearImageAttachments() {
	a.pendingImageAttachments = nil
}

func (a *App) getImageAttachmentCount() int {
	return len(a.pendingImageAttachments)
}

func (a *App) refreshImageAttachmentDisplay() {
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
}

func (a *App) hasImageAttachments() bool {
	return len(a.pendingImageAttachments) > 0
}

func (a *App) getImageAttachments() []pendingImageAttachment {
	return a.pendingImageAttachments
}

func (a *App) loadImageAttachmentData(index int) ([]byte, error) {
	if index < 0 || index >= len(a.pendingImageAttachments) {
		return nil, fmt.Errorf("invalid attachment index")
	}
	return tuiinfra.ReadImageFile(a.pendingImageAttachments[index].Path)
}

func (a *App) addImageFromClipboard() error {
	if len(a.pendingImageAttachments) >= maxImageAttachments {
		return fmt.Errorf("maximum %d image attachments allowed", maxImageAttachments)
	}

	data, imageErr := readClipboardImage()
	if imageErr == nil && len(data) > 0 {
		tmpPath, err := saveClipboardImageToTempFile(data, "paste")
		if err != nil {
			return fmt.Errorf("failed to save clipboard image: %w", err)
		}
		if err := a.queueImageAttachmentForPrepare(tmpPath); err != nil {
			return err
		}
		a.state.StatusText = "[System] Added image from clipboard"
		return nil
	}

	if path, ok := clipboardImagePathFromText(a.state.CurrentWorkdir); ok {
		if err := a.queueImageAttachmentForPrepare(path); err != nil {
			return err
		}
		a.state.StatusText = "[System] Added image from clipboard path"
		return nil
	}

	if imageErr != nil {
		return fmt.Errorf("failed to read clipboard image: %w", imageErr)
	}
	return fmt.Errorf("no image in clipboard")
}

func clipboardImagePathFromText(currentWorkdir string) (string, bool) {
	text, err := readClipboardText()
	if err != nil {
		return "", false
	}
	for _, line := range splitClipboardLines(text) {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		candidate = strings.Trim(candidate, "\"'")
		candidate = normalizeClipboardFileURL(candidate)
		if !looksLikeImagePath(candidate) {
			continue
		}
		if filepath.IsAbs(candidate) {
			return candidate, true
		}
		if strings.TrimSpace(currentWorkdir) == "" {
			continue
		}
		return filepath.Join(currentWorkdir, candidate), true
	}
	return "", false
}

func clipboardFileReferencePathsFromText(text string, currentWorkdir string) ([]string, bool) {
	lines := splitClipboardLines(text)
	if len(lines) == 0 {
		return nil, false
	}

	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}

		candidate = strings.Trim(candidate, "\"'")
		candidate = normalizeClipboardFileURL(candidate)
		if candidate == "" {
			return nil, false
		}

		resolved := candidate
		if !filepath.IsAbs(resolved) {
			if strings.TrimSpace(currentWorkdir) == "" {
				return nil, false
			}
			resolved = filepath.Join(currentWorkdir, resolved)
		}

		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return nil, false
		}
		paths = append(paths, resolved)
	}

	if len(paths) == 0 {
		return nil, false
	}
	return paths, true
}

func splitClipboardLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}

func normalizeClipboardFileURL(candidate string) string {
	trimmed := strings.TrimSpace(candidate)
	if !strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		return trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimPrefix(strings.TrimPrefix(trimmed, "file:///"), "file://")
	}

	path := parsed.Path
	if parsed.Host != "" {
		path = "//" + parsed.Host + path
	}
	if decoded, decodeErr := url.PathUnescape(path); decodeErr == nil {
		path = decoded
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return path
}
