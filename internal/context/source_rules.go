package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	ruleFileName      = "AGENTS.md"
	maxRuleFileRunes  = 4000
	maxTotalRuleRunes = 12000
)

// ruleDocument 表示单个规则文件在提示词中的投影。
type ruleDocument struct {
	Path      string
	Content   string
	Truncated bool
}

// ruleFileFinder 抽象目录扫描逻辑，便于在测试中注入假实现。
type ruleFileFinder func(string) (string, error)

// loadProjectRules 从 workdir 向上逐级发现并加载规则文件。
func loadProjectRules(ctx context.Context, workdir string) ([]ruleDocument, error) {
	paths, err := discoverRuleFiles(ctx, workdir)
	if err != nil {
		return nil, err
	}

	return loadRuleDocuments(ctx, paths, os.ReadFile)
}

// loadRuleDocuments 读取并截断规则文件内容，避免单文件过大挤占上下文预算。
func loadRuleDocuments(ctx context.Context, paths []string, readFile func(string) ([]byte, error)) ([]ruleDocument, error) {
	documents := make([]ruleDocument, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data, err := readFile(path)
		if err != nil {
			return nil, fmt.Errorf("context: read %s: %w", path, err)
		}

		content, truncated := truncateRunes(strings.TrimSpace(string(data)), maxRuleFileRunes)
		documents = append(documents, ruleDocument{
			Path:      path,
			Content:   content,
			Truncated: truncated,
		})
	}

	return documents, nil
}

// discoverRuleFiles 执行默认规则文件发现策略。
func discoverRuleFiles(ctx context.Context, workdir string) ([]string, error) {
	return discoverRuleFilesWithFinder(ctx, workdir, findExactRuleFile)
}

// discoverRuleFilesWithFinder 从当前目录向上遍历到根目录，收集匹配规则文件。
// 返回顺序会反转为“从上层到下层”，让更顶层规则先出现。
func discoverRuleFilesWithFinder(ctx context.Context, workdir string, finder ruleFileFinder) ([]string, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return nil, nil
	}

	dir := filepath.Clean(workdir)
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	paths := make([]string, 0, 4)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		match, err := finder(dir)
		if err != nil {
			break
		}
		if match != "" {
			paths = append(paths, match)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	return paths, nil
}

// findExactRuleFile 在单个目录内查找严格大小写匹配的 AGENTS.md。
func findExactRuleFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("context: read dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() == ruleFileName {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", nil
}

// renderProjectRulesSection 在总预算内渲染规则分节。
// 当预算不足时，优先保留靠近工作目录的规则并标记截断提示。
func renderProjectRulesSection(documents []ruleDocument) promptSection {
	if len(documents) == 0 {
		return promptSection{}
	}

	const totalTruncationNotice = "\n[additional project rules truncated to fit total limit]\n"

	var builder strings.Builder

	remaining := maxTotalRuleRunes
	totalBudgetTruncated := false
	for _, document := range documents {
		if remaining <= 0 {
			totalBudgetTruncated = true
			break
		}

		fullChunk := renderRuleDocumentChunk(document)
		fullChunkRunes := runeCount(fullChunk)
		if fullChunkRunes <= remaining {
			builder.WriteString(fullChunk)
			remaining -= fullChunkRunes
			continue
		}

		totalBudgetTruncated = true
		chunkBudget := remaining
		if noticeRunes := runeCount(totalTruncationNotice); noticeRunes < chunkBudget {
			chunkBudget -= noticeRunes
		}
		chunk := renderRuleDocumentChunkWithinBudget(document, chunkBudget)
		builder.WriteString(chunk)
		remaining -= runeCount(chunk)
		break
	}

	if totalBudgetTruncated {
		if runeCount(totalTruncationNotice) <= remaining {
			builder.WriteString(totalTruncationNotice)
		}
	}

	return promptSection{
		title:   "Project Rules",
		content: strings.TrimSpace(builder.String()),
	}
}

// renderRuleDocumentChunk 渲染单个规则文件块（不考虑总预算）。
func renderRuleDocumentChunk(document ruleDocument) string {
	var builder strings.Builder
	builder.WriteString("\n### ")
	builder.WriteString(document.Path)
	builder.WriteString("\n")
	if document.Content != "" {
		builder.WriteString("\n")
		builder.WriteString(document.Content)
		builder.WriteString("\n")
	}
	if document.Truncated {
		builder.WriteString("\n[truncated to fit per-file limit]\n")
	}

	return builder.String()
}

// renderRuleDocumentChunkWithinBudget 在给定字符预算内渲染单个规则文件块。
// 若预算不足以容纳标题，则直接返回空字符串。
func renderRuleDocumentChunkWithinBudget(document ruleDocument, budget int) string {
	if budget <= 0 {
		return ""
	}

	header := "\n### " + document.Path + "\n"
	headerRunes := runeCount(header)
	if headerRunes > budget {
		return ""
	}

	bodyBudget := budget - headerRunes
	content := document.Content
	if runeCount(content) > bodyBudget {
		content, _ = truncateRunes(content, bodyBudget)
	}

	var body strings.Builder
	if content != "" {
		body.WriteString("\n")
		body.WriteString(content)
		body.WriteString("\n")
	}
	if document.Truncated {
		perFileNotice := "\n[truncated to fit per-file limit]\n"
		if runeCount(body.String())+runeCount(perFileNotice) <= bodyBudget {
			body.WriteString(perFileNotice)
		}
	}

	bodyRunes := runeCount(body.String())
	if bodyRunes > bodyBudget {
		bodyText, _ := truncateRunes(body.String(), bodyBudget)
		body.Reset()
		body.WriteString(bodyText)
	}

	return header + body.String()
}

// truncateRunes 按 rune（而非字节）截断，避免 UTF-8 多字节字符被破坏。
func truncateRunes(input string, max int) (string, bool) {
	if max <= 0 {
		return "", input != ""
	}
	if runeCount(input) <= max {
		return input, false
	}

	runes := []rune(input)
	return string(runes[:max]), true
}

// runeCount 统一使用 rune 数进行长度预算计算。
func runeCount(input string) int {
	return utf8.RuneCountInString(input)
}
