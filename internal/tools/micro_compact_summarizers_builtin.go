package tools

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type builtinSummarizerRegistration struct {
	toolName   string
	summarizer ContentSummarizer
}

var builtinSummarizers = []builtinSummarizerRegistration{
	{toolName: ToolNameBash, summarizer: bashSummarizer},
	{toolName: ToolNameFilesystemReadFile, summarizer: readFileSummarizer},
	{toolName: ToolNameFilesystemWriteFile, summarizer: writeFileSummarizer},
	{toolName: ToolNameFilesystemEdit, summarizer: editSummarizer},
	{toolName: ToolNameFilesystemGrep, summarizer: grepSummarizer},
	{toolName: ToolNameFilesystemGlob, summarizer: globSummarizer},
	{toolName: ToolNameWebFetch, summarizer: webfetchSummarizer},
}

// RegisterBuiltinSummarizers 将所有内置工具的内容摘要器注册到 Registry。
// 建议在启动装配阶段调用；可重复调用并覆盖同名摘要器。
func RegisterBuiltinSummarizers(registry *Registry) {
	if registry == nil {
		return
	}
	for _, item := range builtinSummarizers {
		registry.RegisterSummarizer(item.toolName, item.summarizer)
	}
}

const summaryMaxRunes = 200
const metadataTokenMaxRunes = 120

// bashSummarizer 仅保留结构化执行元信息，避免把原始输出内容重新注入上下文。
func bashSummarizer(content string, metadata map[string]string, isError bool) string {
	var parts []string

	if isError {
		parts = append(parts, "[exit=non-zero]")
	} else {
		parts = append(parts, "[exit=0]")
	}

	if workdir := metadataToken(metadata["workdir"]); workdir != "" {
		parts = append(parts, "workdir="+workdir)
	}

	trimmed := strings.TrimSpace(content)
	if trimmed != "" {
		parts = appendTextStats(parts, trimmed)
	}

	return truncateRunes(strings.Join(parts, " "), summaryMaxRunes)
}

// readFileSummarizer 仅保留稳定元信息，避免在摘要中再次暴露文件正文。
func readFileSummarizer(content string, metadata map[string]string, isError bool) string {
	path := metadataToken(metadata["path"])
	if path == "" {
		return ""
	}

	lineCount := stableLineCount(content)

	var parts []string
	parts = append(parts, "[summary]", path, "lines="+strconv.Itoa(lineCount))
	if content != "" {
		parts = append(parts, "chars="+strconv.Itoa(utf8.RuneCountInString(content)))
	}

	return truncateRunes(strings.Join(parts, " "), summaryMaxRunes)
}

// writeFileSummarizer 保留文件路径与写入字节数。
func writeFileSummarizer(content string, metadata map[string]string, isError bool) string {
	path := metadataToken(metadata["path"])
	if path == "" {
		return ""
	}
	bytes := metadata["bytes"]
	return truncateRunes("[summary] wrote "+path+" ("+bytes+" bytes)", summaryMaxRunes)
}

// editSummarizer 保留编辑路径与替换范围。
func editSummarizer(content string, metadata map[string]string, isError bool) string {
	path := metadataToken(metadata["relative_path"])
	if path == "" {
		path = metadataToken(metadata["path"])
	}
	if path == "" {
		return ""
	}
	searchLen := metadata["search_length"]
	replaceLen := metadata["replacement_length"]
	return truncateRunes(
		"[summary] edited "+path+" (search="+searchLen+" chars, replace="+replaceLen+" chars)",
		summaryMaxRunes,
	)
}

// grepSummarizer 保留搜索根目录、匹配计数与前若干文件名。
func grepSummarizer(content string, metadata map[string]string, isError bool) string {
	var parts []string
	parts = append(parts, "[summary] grep")

	if root := metadataToken(metadata["root"]); root != "" {
		parts = append(parts, "root="+root)
	}

	if matchedFiles := metadata["matched_files"]; matchedFiles != "" {
		parts = append(parts, "files="+matchedFiles)
	}
	if matchedLines := metadata["matched_lines"]; matchedLines != "" {
		parts = append(parts, "lines="+matchedLines)
	}

	// 从 content 中提取前几个不重复文件名，避免对整段输出做全量切分。
	fileNames := extractUniqueMatchFiles(content, 3)
	if len(fileNames) > 0 {
		parts = append(parts, "matches="+strings.Join(fileNames, ", "))
	}

	return truncateRunes(strings.Join(parts, " "), summaryMaxRunes)
}

// globSummarizer 保留匹配计数与前若干文件名。
func globSummarizer(content string, metadata map[string]string, isError bool) string {
	count := metadata["count"]
	if count == "" {
		count = "?"
	}

	preview := collectPreviewLines(content, 3)

	var parts []string
	parts = append(parts, "[summary] glob", count+" files")
	if len(preview) > 0 {
		parts = append(parts, strings.Join(preview, ", "))
	}

	return truncateRunes(strings.Join(parts, " "), summaryMaxRunes)
}

// webfetchSummarizer 保留可稳定持久化的 webfetch 结果标记。
func webfetchSummarizer(content string, metadata map[string]string, isError bool) string {
	var parts []string
	parts = append(parts, "[summary] webfetch")

	if truncated := metadata["truncated"]; truncated == "true" {
		parts = append(parts, "truncated=true")
	}

	return truncateRunes(strings.Join(parts, " "), summaryMaxRunes)
}

// truncateRunes 按 rune 数量截断字符串，超出时追加 "..."。
func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 || text == "" {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes]) + "..."
}

// stableLineCount 统计文本行数；空文本返回 0，末尾换行不会产生额外空行计数。
func stableLineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		count--
	}
	if count < 0 {
		return 0
	}
	return count
}

// appendTextStats 为摘要补充文本统计字段，保持统一的结构化输出格式。
func appendTextStats(parts []string, text string) []string {
	return append(parts,
		"lines="+strconv.Itoa(stableLineCount(text)),
		"chars="+strconv.Itoa(utf8.RuneCountInString(text)),
	)
}

// extractUniqueMatchFiles 按行扫描 grep 输出，提取前若干个去重后的文件名摘要。
func extractUniqueMatchFiles(content string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	seen := make(map[string]struct{}, limit)
	result := make([]string, 0, limit)
	remaining := content
	for len(remaining) > 0 && len(result) < limit {
		line, rest := nextLine(remaining)
		remaining = rest

		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}

		file := sanitizeSummaryToken(line[:colon], 80)
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		result = append(result, file)
	}
	return result
}

// collectPreviewLines 按行扫描输出并提取前若干个非空预览，避免全量 Split 带来的额外分配。
func collectPreviewLines(content string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	result := make([]string, 0, limit)
	remaining := content
	for len(remaining) > 0 && len(result) < limit {
		line, rest := nextLine(remaining)
		remaining = rest

		clean := sanitizeSummaryToken(line, 100)
		if clean == "" {
			continue
		}
		result = append(result, clean)
	}
	return result
}

// nextLine 返回 text 的首行及余下文本，兼容存在或不存在换行符的输入。
func nextLine(text string) (line string, rest string) {
	idx := strings.IndexByte(text, '\n')
	if idx < 0 {
		return text, ""
	}
	return text[:idx], text[idx+1:]
}

// sanitizeSummaryToken 清理不可见控制字符并裁剪长度，降低摘要注入风险。
func sanitizeSummaryToken(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.TrimSpace(b.String())
	if clean == "" {
		return ""
	}
	return truncateRunes(clean, maxRunes)
}

// metadataToken 统一清理 metadata 中可回灌到摘要的文本字段。
func metadataToken(text string) string {
	return sanitizeSummaryToken(text, metadataTokenMaxRunes)
}
