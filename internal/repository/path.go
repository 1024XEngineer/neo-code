package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/security"
)

var errInvalidMode = errors.New("repository: invalid retrieval mode")

type FileReader func(path string) ([]byte, error)

const (
	defaultRetrievalLimit = 20
	maxRetrievalLimit     = 50
	defaultContextLines   = 3
	maxContextLines       = 8
	maxSnippetLines       = 20
	maxSnippetLineRunes   = 512
)

// normalizeRetrievalQuery 统一校验检索请求并补齐默认值。
func normalizeRetrievalQuery(workdir string, query RetrievalQuery) (string, string, RetrievalQuery, error) {
	normalized := query
	normalized.Value = strings.TrimSpace(query.Value)
	if normalized.Value == "" {
		return "", "", RetrievalQuery{}, errors.New("repository: query value is empty")
	}

	root, _, err := security.ResolveWorkspacePath(workdir, ".")
	if err != nil {
		return "", "", RetrievalQuery{}, err
	}
	scope, err := resolveScopeDir(root, query.ScopeDir)
	if err != nil {
		return "", "", RetrievalQuery{}, err
	}

	switch normalized.Mode {
	case RetrievalModePath, RetrievalModeGlob, RetrievalModeText, RetrievalModeSymbol:
	default:
		return "", "", RetrievalQuery{}, errInvalidMode
	}
	normalized.Limit = normalizeLimit(normalized.Limit, defaultRetrievalLimit, maxRetrievalLimit)
	normalized.ContextLines = normalizeLimit(normalized.ContextLines, defaultContextLines, maxContextLines)
	return root, scope, normalized, nil
}

// resolveScopeDir 解析检索范围目录，空值时返回整个工作区根。
func resolveScopeDir(root string, scopeDir string) (string, error) {
	_, target, err := security.ResolveWorkspacePath(root, scopeDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("repository: inspect scope dir %q: %w", scopeDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository: scope dir %q is not a directory", scopeDir)
	}
	return target, nil
}

// splitNonEmptyLines 将文本按行拆分并去除空白行。
func splitNonEmptyLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			trimmed = append(trimmed, line)
		}
	}
	return trimmed
}

// trimSnippetText 将片段限制到指定最大行数，并返回保留行数和是否被裁剪。
func trimSnippetText(text string, maxLines int) snippetResult {
	lines := splitNonEmptyLines(text)
	if len(lines) == 0 || maxLines <= 0 {
		return snippetResult{}
	}
	truncated := false
	for index, line := range lines {
		shortened, changed := truncateSnippetLine(line, maxSnippetLineRunes)
		if changed {
			truncated = true
		}
		lines[index] = shortened
	}

	result := snippetResult{
		text:      strings.Join(lines, "\n"),
		lines:     len(lines),
		truncated: truncated,
	}
	if len(lines) > maxLines {
		result.text = strings.Join(lines[:maxLines], "\n")
		result.lines = maxLines
		result.truncated = true
	}
	return result
}

func truncateSnippetLine(line string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return "", line != ""
	}
	runes := []rune(line)
	if len(runes) <= maxRunes {
		return line, false
	}
	return string(runes[:maxRunes]), true
}

// snippetAroundLine 生成命中行上下文片段，并返回建议的 line hint。
func snippetAroundLine(content string, lineNumber int, contextLines int) (string, int) {
	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(rawLines) == 0 {
		return "", 0
	}
	if lineNumber <= 0 {
		lineNumber = 1
	}
	if lineNumber > len(rawLines) {
		lineNumber = len(rawLines)
	}

	start := lineNumber - contextLines
	if start < 1 {
		start = 1
	}
	end := lineNumber + contextLines
	if end > len(rawLines) {
		end = len(rawLines)
	}
	snippet := trimSnippetText(strings.Join(rawLines[start-1:end], "\n"), maxSnippetLines)
	return snippet.text, lineNumber
}

// walkWorkspaceFiles 遍历工作区文件，同时跳过已约定的噪声目录，并支持取消信号快速中断。
func walkWorkspaceFiles(
	ctx context.Context,
	root string,
	scope string,
	visit func(path string) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonicalRoot, _, err := security.ResolveWorkspacePath(root, ".")
	if err != nil {
		return err
	}
	return filepath.WalkDir(scope, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && skipDirEntry(entry) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		resolvedPath, resolveErr := security.ResolveWorkspaceWalkPathFromRoot(canonicalRoot, path, entry)
		if resolveErr != nil {
			return resolveErr
		}
		return visit(resolvedPath)
	})
}

// skipDirEntry 与 filesystem 工具保持一致地忽略高噪声目录。
func skipDirEntry(entry fs.DirEntry) bool {
	name := strings.ToLower(strings.TrimSpace(entry.Name()))
	switch name {
	case ".git", ".idea", ".vscode", "node_modules":
		return true
	default:
		return false
	}
}

// normalizeLimit 统一应用默认值与硬上限。
func normalizeLimit(value int, defaultValue int, maxValue int) int {
	if value <= 0 {
		return defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// filepathSlashClean 统一清理 git 输出中的路径分隔符。
func filepathSlashClean(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(path))
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
