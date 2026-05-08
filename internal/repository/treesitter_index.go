package repository

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// tsIndexEntry 是倒排索引中的单条符号位置。
type tsIndexEntry struct {
	Name      string // 符号名称（原始大小写，用于输出）
	Path      string
	LineHint  int
	Kind      string
	Signature string
}

// tsFileMeta 记录已索引文件的 mtime 和大小，用于增量更新检测。
type tsFileMeta struct {
	modTime time.Time
	size    int64
}

// TreeSitterIndexer 管理内存中的跨语言符号倒排索引。
// 线程安全，支持惰性初始化 + 增量更新。
type TreeSitterIndexer struct {
	mu       sync.RWMutex
	built    bool
	root     string
	symbols  map[string][]tsIndexEntry // lower(symbol) → entries
	fileMeta map[string]tsFileMeta     // path → meta
}

// NewTreeSitterIndexer 返回一个惰性初始化的 Tree-sitter 索引器。
func NewTreeSitterIndexer() *TreeSitterIndexer {
	return &TreeSitterIndexer{
		symbols:  make(map[string][]tsIndexEntry),
		fileMeta: make(map[string]tsFileMeta),
	}
}

// Search 从索引中查询符号，返回精确匹配和前缀匹配的结果。
// 返回的 hits 按 path + line_hint 排序。
func (idx *TreeSitterIndexer) Search(name string, limit int) []SymbolSearchHit {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if !idx.built {
		return nil
	}

	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil
	}

	// 精确匹配
	exact := idx.symbols[key]
	// 前缀匹配（如 "Hello" 匹配 "HelloWorld"）
	var prefix []tsIndexEntry
	for k, entries := range idx.symbols {
		if k != key && strings.HasPrefix(k, key) {
			prefix = append(prefix, entries...)
		}
	}

	total := append(exact, prefix...)
	if len(total) == 0 {
		return nil
	}
	if limit > 0 && len(total) > limit {
		total = total[:limit]
	}

	hits := make([]SymbolSearchHit, len(total))
	for i, e := range total {
		hits[i] = SymbolSearchHit{
			Path:      e.Path,
			LineHint:  e.LineHint,
			Kind:      e.Kind,
			Signature: e.Signature,
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Path == hits[j].Path {
			return hits[i].LineHint < hits[j].LineHint
		}
		return hits[i].Path < hits[j].Path
	})
	return hits
}

// EnsureBuilt 惰性初始化索引：扫描工作区，解析所有非 Go 文件，构建倒排索引。
func (idx *TreeSitterIndexer) EnsureBuilt(ctx context.Context, root string, scope string, readFile FileReader) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.built && idx.root == root {
		return nil
	}

	idx.root = root
	idx.symbols = make(map[string][]tsIndexEntry)
	idx.fileMeta = make(map[string]tsFileMeta)

	walkErr := walkWorkspaceFiles(ctx, root, scope, func(absPath string) error {
		return idx.indexFile(ctx, root, absPath, readFile)
	})
	idx.built = true
	if walkErr != nil {
		return walkErr
	}
	return nil
}

// Refresh 使用文件 mtime 检测变更并增量更新索引。
func (idx *TreeSitterIndexer) Refresh(ctx context.Context, root string, scope string, readFile FileReader) error {
	if !idx.isBuilt() || idx.getRoot() != root {
		return idx.EnsureBuilt(ctx, root, scope, readFile)
	}

	currentMeta := make(map[string]tsFileMeta)
	var changedFiles []string
	var deletedFiles []string
	var mu sync.Mutex

	walkErr := walkWorkspaceFiles(ctx, root, scope, func(absPath string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		rel, relErr := filepath.Rel(root, absPath)
		if relErr != nil {
			return nil
		}
		cleanRel := filepath.ToSlash(rel)

		info, statErr := os.Stat(absPath)
		if statErr != nil {
			return nil
		}
		meta := tsFileMeta{modTime: info.ModTime(), size: info.Size()}

		mu.Lock()
		currentMeta[cleanRel] = meta
		mu.Unlock()

		idx.mu.RLock()
		oldMeta, exists := idx.fileMeta[cleanRel]
		idx.mu.RUnlock()

		if !exists || !meta.modTime.Equal(oldMeta.modTime) || meta.size != oldMeta.size {
			mu.Lock()
			changedFiles = append(changedFiles, absPath)
			mu.Unlock()
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	idx.mu.RLock()
	for path := range idx.fileMeta {
		if _, exists := currentMeta[path]; !exists {
			deletedFiles = append(deletedFiles, path)
		}
	}
	idx.mu.RUnlock()

	// 重新索引变更文件
	for _, absPath := range changedFiles {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		idx.replaceFile(ctx, root, absPath, readFile)
	}

	// 删除已移除文件的索引条目
	idx.mu.Lock()
	for _, cleanRel := range deletedFiles {
		idx.deleteFileEntries(cleanRel)
	}
	idx.mu.Unlock()

	return nil
}

// Close 释放索引持有的内存。
func (idx *TreeSitterIndexer) Close() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.symbols = make(map[string][]tsIndexEntry)
	idx.fileMeta = make(map[string]tsFileMeta)
	idx.built = false
}

func (idx *TreeSitterIndexer) isBuilt() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.built
}

func (idx *TreeSitterIndexer) getRoot() string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.root
}

// indexFile 解析单个文件的符号并添加到索引。
func (idx *TreeSitterIndexer) indexFile(ctx context.Context, root string, absPath string, readFile FileReader) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if filepath.Ext(absPath) == ".go" {
		return nil
	}

	rel, relErr := filepath.Rel(root, absPath)
	if relErr != nil {
		return nil
	}
	cleanRel := filepath.ToSlash(rel)

	entries, meta, ok := idx.parseFile(root, absPath, readFile)
	if !ok {
		return nil
	}

	idx.fileMeta[cleanRel] = meta
	for _, e := range entries {
		key := strings.ToLower(strings.TrimSpace(e.Name))
		idx.symbols[key] = append(idx.symbols[key], e)
	}
	return nil
}

// replaceFile 替换已有文件的索引条目（先删后加）。
func (idx *TreeSitterIndexer) replaceFile(ctx context.Context, root string, absPath string, readFile FileReader) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	rel, relErr := filepath.Rel(root, absPath)
	if relErr != nil {
		return nil
	}
	cleanRel := filepath.ToSlash(rel)

	if filepath.Ext(absPath) == ".go" {
		idx.mu.Lock()
		idx.deleteFileEntries(cleanRel)
		idx.mu.Unlock()
		return nil
	}

	entries, meta, ok := idx.parseFile(root, absPath, readFile)
	idx.mu.Lock()
	idx.deleteFileEntries(cleanRel)
	if ok {
		idx.fileMeta[cleanRel] = meta
		for _, e := range entries {
			key := strings.ToLower(strings.TrimSpace(e.Name))
			idx.symbols[key] = append(idx.symbols[key], e)
		}
	}
	idx.mu.Unlock()
	return nil
}

// deleteFileEntries 从索引中删除指定文件的全部条目。
func (idx *TreeSitterIndexer) deleteFileEntries(cleanRel string) {
	delete(idx.fileMeta, cleanRel)
	for key, entries := range idx.symbols {
		filtered := entries[:0]
		for _, e := range entries {
			if e.Path != cleanRel {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(idx.symbols, key)
		} else {
			idx.symbols[key] = filtered
		}
	}
}

// parseFile 使用 Tree-sitter 解析一个文件，返回所有符号条目 + 文件元信息。
func (idx *TreeSitterIndexer) parseFile(root string, absPath string, readFile FileReader) ([]tsIndexEntry, tsFileMeta, bool) {
	content, ok := readRetrievalTextWithReader(root, absPath, readFile)
	if !ok {
		return nil, tsFileMeta{}, false
	}

	info, statErr := os.Stat(absPath)
	if statErr != nil {
		return nil, tsFileMeta{}, false
	}
	meta := tsFileMeta{modTime: info.ModTime(), size: info.Size()}

	entry := grammars.DetectLanguage(filepath.Base(absPath))
	if entry == nil {
		return nil, meta, false
	}
	lang := entry.Language()
	tagsQuery := grammars.ResolveTagsQuery(*entry)
	if tagsQuery == "" {
		return nil, meta, false
	}

	parser := gotreesitter.NewParser(lang)
	tree, parseErr := parser.Parse([]byte(content))
	if parseErr != nil {
		return nil, meta, false
	}
	rootNode := tree.RootNode()

	query, queryErr := gotreesitter.NewQuery(tagsQuery, lang)
	if queryErr != nil {
		return nil, meta, false
	}

	cursor := query.Exec(rootNode, lang, []byte(content))
	src := []byte(content)
	var entries []tsIndexEntry

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		var defNode *gotreesitter.Node
		var defKind string
		var nameText string
		var nameLine int

		for _, cap := range match.Captures {
			capName := cap.Name
			if capName == "name" {
				nameText = strings.TrimSpace(cap.Text(src))
				nameLine = int(cap.Node.StartPoint().Row) + 1
			}
			if strings.HasPrefix(capName, "definition.") {
				defNode = cap.Node
				defKind = captureNameToKind(capName)
			}
		}

		if nameText == "" {
			continue
		}

		sig := ""
		if defNode != nil {
			sig = extractTreeSitterSignature(src, defNode)
		}
		if sig == "" && nameLine > 0 {
			sig = extractLineSignature(content, nameLine)
		}

		rel, _ := filepath.Rel(root, absPath)
		entries = append(entries, tsIndexEntry{
			Name:      nameText,
			Path:      filepath.Clean(rel),
			LineHint:  nameLine,
			Kind:      defKind,
			Signature: sig,
		})
	}

	return entries, meta, true
}
