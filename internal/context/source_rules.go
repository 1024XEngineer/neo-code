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
	projectRuleFileName                = "AGENTS.md"
	projectRulePerFileRuneLimit        = 4000
	projectRuleTotalRuneLimit          = 12000
	projectRulePerFileTruncationNotice = "\n[truncated to fit per-file limit]\n"
	projectRuleTotalTruncationNotice   = "\n[additional project rules truncated to fit total limit]\n"
)

type ruleDocument struct {
	Path      string
	Content   string
	Truncated bool
}

type ruleFileFinder func(string) (string, error)

// Sections 按当前工作目录向上发现 AGENTS.md，并优先复用未失效的缓存结果。
func (s *projectRulesSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	rules, err := s.loadCachedProjectRules(ctx, input.Metadata.Workdir)
	if err != nil {
		return nil, err
	}

	section := renderProjectRulesSection(rules)
	if renderPromptSection(section) == "" {
		return nil, nil
	}
	return []promptSection{section}, nil
}

// loadCachedProjectRules 加载项目规则，并在路径和 mtime 未变化时复用缓存内容。
func (s *projectRulesSource) loadCachedProjectRules(ctx context.Context, workdir string) ([]ruleDocument, error) {
	key := normalizeRuleCacheKey(workdir)
	if key == "" {
		return nil, nil
	}

	s.mu.Lock()
	entry, ok := s.cache[key]
	s.mu.Unlock()

	if ok {
		valid, err := s.isRuleCacheEntryValid(entry)
		if err != nil {
			return nil, err
		}
		if valid {
			return cloneRuleDocuments(entry.documents), nil
		}
	}

	documents, err := s.ruleLoader()(ctx, workdir)
	if err != nil {
		return nil, err
	}

	snapshots, err := snapshotRuleFiles(ruleDocumentPaths(documents), s.ruleStatFile())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]cachedRuleDocuments)
	}
	s.cache[key] = cachedRuleDocuments{
		documents: cloneRuleDocuments(documents),
		snapshots: snapshots,
	}
	s.mu.Unlock()

	return documents, nil
}

// isRuleCacheEntryValid 根据规则文件路径与 mtime 判断缓存是否仍可复用。
func (s *projectRulesSource) isRuleCacheEntryValid(entry cachedRuleDocuments) (bool, error) {
	statFile := s.ruleStatFile()
	for _, snapshot := range entry.snapshots {
		info, err := statFile(snapshot.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("context: stat %s: %w", snapshot.Path, err)
		}
		if info.Size() != snapshot.Size || !info.ModTime().Equal(snapshot.ModTime) {
			return false, nil
		}
	}
	return true, nil
}

// ruleLoader 返回 project rules 的实际加载函数，便于测试注入。
func (s *projectRulesSource) ruleLoader() projectRulesLoader {
	if s != nil && s.loadRules != nil {
		return s.loadRules
	}
	return loadProjectRules
}

// ruleStatFile 返回规则文件 stat 函数，便于测试控制缓存失效条件。
func (s *projectRulesSource) ruleStatFile() ruleFileStat {
	if s != nil && s.statFile != nil {
		return s.statFile
	}
	return os.Stat
}

// normalizeRuleCacheKey 统一清洗 workdir，避免缓存键受路径噪音影响。
func normalizeRuleCacheKey(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	return filepath.Clean(workdir)
}

// cloneRuleDocuments 深拷贝规则文档切片，避免缓存与调用方共享底层数据。
func cloneRuleDocuments(documents []ruleDocument) []ruleDocument {
	return append([]ruleDocument(nil), documents...)
}

// ruleDocumentPaths 提取规则文档路径，用于缓存签名计算。
func ruleDocumentPaths(documents []ruleDocument) []string {
	paths := make([]string, 0, len(documents))
	for _, document := range documents {
		paths = append(paths, document.Path)
	}
	return paths
}

// snapshotRuleFiles 采集规则文件的路径、mtime 与 size，供缓存命中判断使用。
func snapshotRuleFiles(paths []string, statFile ruleFileStat) ([]ruleFileSnapshot, error) {
	snapshots := make([]ruleFileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := statFile(path)
		if err != nil {
			return nil, fmt.Errorf("context: stat %s: %w", path, err)
		}
		snapshots = append(snapshots, ruleFileSnapshot{
			Path:    path,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}
	return snapshots, nil
}

// loadProjectRules 发现并读取当前工作目录可见的规则文件。
func loadProjectRules(ctx context.Context, workdir string) ([]ruleDocument, error) {
	paths, err := discoverRuleFiles(ctx, workdir)
	if err != nil {
		return nil, err
	}

	return loadRuleDocuments(ctx, paths, os.ReadFile)
}

// loadRuleDocuments 按顺序读取规则文件并应用单文件裁剪预算。
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

		content, truncated := truncateRunes(strings.TrimSpace(string(data)), projectRulePerFileRuneLimit)
		documents = append(documents, ruleDocument{
			Path:      path,
			Content:   content,
			Truncated: truncated,
		})
	}

	return documents, nil
}

// discoverRuleFiles 自底向上发现当前工作目录可见的 AGENTS.md 文件。
func discoverRuleFiles(ctx context.Context, workdir string) ([]string, error) {
	return discoverRuleFilesWithFinder(ctx, workdir, findExactRuleFile)
}

// discoverRuleFilesWithFinder 允许注入 finder 以便测试不同的规则发现行为。
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
			return nil, fmt.Errorf("context: discover rule file in %s: %w", dir, err)
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

// findExactRuleFile 只匹配大小写完全一致的 AGENTS.md，避免误读同名变体。
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
		if entry.Name() == projectRuleFileName {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", nil
}

// renderProjectRulesSection 将规则文档渲染为统一 prompt section，并应用总预算裁剪。
func renderProjectRulesSection(documents []ruleDocument) promptSection {
	if len(documents) == 0 {
		return promptSection{}
	}

	var builder strings.Builder

	remaining := projectRuleTotalRuneLimit
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
		if noticeRunes := runeCount(projectRuleTotalTruncationNotice); noticeRunes < chunkBudget {
			chunkBudget -= noticeRunes
		}
		chunk := renderRuleDocumentChunkWithinBudget(document, chunkBudget)
		builder.WriteString(chunk)
		remaining -= runeCount(chunk)
		break
	}

	if totalBudgetTruncated {
		if runeCount(projectRuleTotalTruncationNotice) <= remaining {
			builder.WriteString(projectRuleTotalTruncationNotice)
		}
	}

	return promptSection{
		Title:   "Project Rules",
		Content: strings.TrimSpace(builder.String()),
	}
}

// renderRuleDocumentChunk 渲染单个规则文档块。
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
		builder.WriteString(projectRulePerFileTruncationNotice)
	}

	return builder.String()
}

// renderRuleDocumentChunkWithinBudget 在剩余预算内渲染单个规则文档块。
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
		if runeCount(body.String())+runeCount(projectRulePerFileTruncationNotice) <= bodyBudget {
			body.WriteString(projectRulePerFileTruncationNotice)
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

// truncateRunes 按 rune 数量裁剪文本，避免截断多字节字符。
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

// runeCount 统一按 rune 数量统计文本体积。
func runeCount(input string) int {
	return utf8.RuneCountInString(input)
}
