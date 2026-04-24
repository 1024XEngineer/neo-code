package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"neo-code/internal/security"
)

const (
	defaultRetrievalLimit         = 20
	maxRetrievalLimit             = 50
	defaultContextLines           = 3
	maxContextLines               = 8
	maxSnippetLines               = 20
	maxRepositorySnippetFileBytes = 256 * 1024
	binaryProbePrefixSize         = 1024
)

var blockedRepositorySnippetExtensions = map[string]struct{}{
	".p8":  {},
	".key": {},
	".pem": {},
	".p12": {},
	".pfx": {},
	".jks": {},
	".der": {},
	".cer": {},
	".crt": {},
}

var blockedRepositorySnippetBaseNames = map[string]struct{}{
	".envrc":           {},
	".npmrc":           {},
	".pypirc":          {},
	".netrc":           {},
	".git-credentials": {},
	"id_rsa":           {},
	"id_dsa":           {},
	"id_ecdsa":         {},
	"id_ed25519":       {},
	"authorized_keys":  {},
	"known_hosts":      {},
	"credentials":      {},
	".terraformrc":     {},
	"terraform.rc":     {},
}

var blockedRepositorySnippetPathSuffixes = []string{
	"/.aws/credentials",
	"/.aws/config",
	"/.docker/config.json",
	"/.kube/config",
	"/.config/gcloud/application_default_credentials.json",
	"/.config/gcloud/credentials.db",
	"/.config/gcloud/access_tokens.db",
}

var blockedRepositorySnippetConfigExtensions = map[string]struct{}{
	".cfg":  {},
	".conf": {},
	".env":  {},
	".ini":  {},
	".json": {},
	".log":  {},
	".md":   {},
	".toml": {},
	".txt":  {},
	".yaml": {},
	".yml":  {},
}

var blockedRepositorySnippetConfigKeywords = []string{
	"credential",
	"credentials",
	"passwd",
	"password",
	"private",
	"secret",
	"secrets",
	"token",
	"tokens",
}

var errRetrievalLimitReached = errors.New("repository: retrieval limit reached")

// retrieveByPath 按路径读取目标文件的受限片段。
func (s *Service) retrieveByPath(ctx context.Context, root string, query RetrievalQuery) (RetrievalResult, error) {
	if err := ctx.Err(); err != nil {
		return RetrievalResult{}, err
	}
	target, _, allowed, err := resolveRepositorySnippetFileFromRoot(root, query.Value)
	if err != nil {
		return RetrievalResult{}, err
	}
	if !allowed {
		return RetrievalResult{}, nil
	}
	content, err := s.readFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return RetrievalResult{}, nil
		}
		return RetrievalResult{}, err
	}
	if isBinaryContent(content) {
		return RetrievalResult{}, nil
	}

	hit, err := buildRetrievalHit(root, target, RetrievalModePath, query.Value, string(content), 1, query.ContextLines)
	if err != nil {
		return RetrievalResult{}, err
	}
	return RetrievalResult{Hits: []RetrievalHit{hit}}, nil
}

// retrieveByGlob 按 glob 模式在工作区内定位候选文件。
func (s *Service) retrieveByGlob(ctx context.Context, root string, scope string, query RetrievalQuery) (RetrievalResult, error) {
	if err := ctx.Err(); err != nil {
		return RetrievalResult{}, err
	}

	effectiveLimit := query.Limit + 1
	hits := make([]RetrievalHit, 0, effectiveLimit)
	truncated := false
	err := walkWorkspaceFiles(ctx, root, scope, func(path string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(hits) >= effectiveLimit {
			return errRetrievalLimitReached
		}
		match, matchErr := filepath.Match(query.Value, filepath.Base(path))
		if matchErr != nil {
			return matchErr
		}
		if !match {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			match, matchErr = filepath.Match(query.Value, filepath.ToSlash(relative))
			if matchErr != nil {
				return matchErr
			}
		}
		if !match {
			return nil
		}
		content, ok := s.readRetrievalText(root, path)
		if !ok {
			return nil
		}
		hit, hitErr := buildRetrievalHit(root, path, RetrievalModeGlob, query.Value, content, 1, query.ContextLines)
		if hitErr != nil {
			return hitErr
		}
		hits = append(hits, hit)
		if len(hits) >= effectiveLimit {
			return errRetrievalLimitReached
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errRetrievalLimitReached) {
			err = nil
		}
	}
	if err != nil {
		return RetrievalResult{}, err
	}
	if len(hits) > query.Limit {
		hits = hits[:query.Limit]
		truncated = true
	}

	sort.Slice(hits, func(i int, j int) bool {
		return hits[i].Path < hits[j].Path
	})
	return RetrievalResult{Hits: hits, Truncated: truncated}, nil
}

// retrieveByText 扫描工作区文本文件并返回稳定排序的关键字命中。
func (s *Service) retrieveByText(ctx context.Context, root string, scope string, query RetrievalQuery, wholeWord bool) (RetrievalResult, error) {
	if err := ctx.Err(); err != nil {
		return RetrievalResult{}, err
	}

	var matcher *regexp.Regexp
	if wholeWord {
		matcher = regexp.MustCompile(`\b` + regexp.QuoteMeta(query.Value) + `\b`)
	}

	effectiveLimit := query.Limit + 1
	hits := make([]RetrievalHit, 0, effectiveLimit)
	truncated := false
	err := walkWorkspaceFiles(ctx, root, scope, func(path string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(hits) >= effectiveLimit {
			return errRetrievalLimitReached
		}
		content, ok := s.readRetrievalText(root, path)
		if !ok {
			return nil
		}
		lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
		for index, line := range lines {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if len(hits) >= effectiveLimit {
				break
			}
			matched := strings.Contains(line, query.Value)
			if wholeWord {
				matched = matcher.MatchString(line)
			}
			if !matched {
				continue
			}

			hit, hitErr := buildRetrievalHit(root, path, RetrievalModeText, query.Value, content, index+1, query.ContextLines)
			if hitErr != nil {
				return hitErr
			}
			hits = append(hits, hit)
			if len(hits) >= effectiveLimit {
				return errRetrievalLimitReached
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errRetrievalLimitReached) {
			err = nil
		}
	}
	if err != nil {
		return RetrievalResult{}, err
	}
	if len(hits) > query.Limit {
		hits = hits[:query.Limit]
		truncated = true
	}

	sortRetrievalHits(hits)
	return RetrievalResult{Hits: hits, Truncated: truncated}, nil
}

// retrieveBySymbol 先做 Go 定义检索，再在无定义命中时回退到 whole-word 文本检索。
func (s *Service) retrieveBySymbol(ctx context.Context, root string, scope string, query RetrievalQuery) (RetrievalResult, error) {
	if err := ctx.Err(); err != nil {
		return RetrievalResult{}, err
	}

	effectiveLimit := query.Limit + 1
	hits := make([]RetrievalHit, 0, effectiveLimit)
	truncated := false
	err := walkWorkspaceFiles(ctx, root, scope, func(path string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(hits) >= effectiveLimit {
			return errRetrievalLimitReached
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		content, ok := s.readRetrievalText(root, path)
		if !ok {
			return nil
		}
		lineNumbers := findGoSymbolDefinitions(content, query.Value)
		for _, lineNumber := range lineNumbers {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if len(hits) >= effectiveLimit {
				break
			}
			hit, hitErr := buildRetrievalHit(root, path, RetrievalModeSymbol, query.Value, content, lineNumber, query.ContextLines)
			if hitErr != nil {
				return hitErr
			}
			hits = append(hits, hit)
			if len(hits) >= effectiveLimit {
				return errRetrievalLimitReached
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errRetrievalLimitReached) {
			err = nil
		}
	}
	if err != nil {
		return RetrievalResult{}, err
	}
	if len(hits) > query.Limit {
		hits = hits[:query.Limit]
		truncated = true
	}
	if len(hits) > 0 {
		sortRetrievalHits(hits)
		return RetrievalResult{Hits: hits, Truncated: truncated}, nil
	}

	textResult, err := s.retrieveByText(ctx, root, scope, query, true)
	if err != nil {
		return RetrievalResult{}, err
	}
	for index := range textResult.Hits {
		textResult.Hits[index].Kind = string(RetrievalModeSymbol)
	}
	return textResult, nil
}

// findGoSymbolDefinitions 以轻量正则匹配 Go 定义，不尝试跨文件语义解析。
func findGoSymbolDefinitions(content string, symbol string) []int {
	if strings.TrimSpace(symbol) == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	directFunc := regexp.MustCompile(`^\s*func\s+` + regexp.QuoteMeta(symbol) + `\s*\(`)
	methodFunc := regexp.MustCompile(`^\s*func\s*\([^)]*\)\s*` + regexp.QuoteMeta(symbol) + `\s*\(`)
	directType := regexp.MustCompile(`^\s*type\s+` + regexp.QuoteMeta(symbol) + `\b`)
	directConst := regexp.MustCompile(`^\s*const\s+` + regexp.QuoteMeta(symbol) + `\b`)
	directVar := regexp.MustCompile(`^\s*var\s+` + regexp.QuoteMeta(symbol) + `\b`)
	blockSymbol := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(symbol) + `\b`)

	results := make([]int, 0, 4)
	inConstBlock := false
	inVarBlock := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "const ("):
			inConstBlock = true
		case strings.HasPrefix(trimmed, "var ("):
			inVarBlock = true
		case trimmed == ")":
			inConstBlock = false
			inVarBlock = false
		}

		if directFunc.MatchString(line) ||
			methodFunc.MatchString(line) ||
			directType.MatchString(line) ||
			directConst.MatchString(line) ||
			directVar.MatchString(line) ||
			((inConstBlock || inVarBlock) && blockSymbol.MatchString(line)) {
			results = append(results, index+1)
		}
	}
	return results
}

// sortRetrievalHits 统一按 path + line 排序，保证同输入下输出稳定。
func sortRetrievalHits(hits []RetrievalHit) {
	sort.Slice(hits, func(i int, j int) bool {
		if hits[i].Path == hits[j].Path {
			return hits[i].LineHint < hits[j].LineHint
		}
		return hits[i].Path < hits[j].Path
	})
}

// readRetrievalText 读取并过滤检索候选文件，失败时按“无命中”处理。
func (s *Service) readRetrievalText(root string, path string) (string, bool) {
	target, _, allowed, err := resolveRepositorySnippetFileFromRoot(root, path)
	if err != nil || !allowed {
		return "", false
	}
	content, err := s.readFile(target)
	if err != nil || isBinaryContent(content) {
		return "", false
	}
	return string(content), true
}

// buildRetrievalHit 基于命中文件和行号构造统一格式的检索结果。
func buildRetrievalHit(
	root string,
	path string,
	mode RetrievalMode,
	query string,
	content string,
	lineNumber int,
	contextLines int,
) (RetrievalHit, error) {
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return RetrievalHit{}, err
	}
	snippet, lineHint := snippetAroundLine(content, lineNumber, contextLines)
	return RetrievalHit{
		Path:          filepath.Clean(relativePath),
		Kind:          string(mode),
		SymbolOrQuery: query,
		Snippet:       snippet,
		LineHint:      lineHint,
	}, nil
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// allowRepositorySnippetByPath 基于路径检查文件是否允许进入 repository 片段。
func resolveRepositorySnippetFile(workdir string, path string) (string, os.FileInfo, bool, error) {
	root, _, err := security.ResolveWorkspacePath(workdir, ".")
	if err != nil {
		return "", nil, false, err
	}
	return resolveRepositorySnippetFileFromRoot(root, path)
}

func resolveRepositorySnippetFileFromRoot(root string, path string) (string, os.FileInfo, bool, error) {
	target, err := security.ResolveWorkspacePathFromRoot(root, path)
	if err != nil {
		return "", nil, false, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, false, nil
		}
		return "", nil, false, err
	}
	resolvedTarget := target
	if info.Mode()&os.ModeSymlink != 0 {
		resolvedTarget, err = filepath.EvalSymlinks(target)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil, false, nil
			}
			return "", nil, false, err
		}
		resolvedTarget, err = security.ResolveWorkspacePathFromRoot(root, resolvedTarget)
		if err != nil {
			return "", nil, false, err
		}
		info, err = os.Stat(resolvedTarget)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil, false, nil
			}
			return "", nil, false, err
		}
	}
	if info.IsDir() {
		return "", nil, false, nil
	}
	if !allowRepositorySnippetByPathAndSize(resolvedTarget, info.Size()) {
		return resolvedTarget, info, false, nil
	}
	return target, info, true, nil
}

// allowRepositorySnippetByPathAndSize 基于路径与大小过滤敏感文件和高成本文件。
func allowRepositorySnippetByPathAndSize(path string, size int64) bool {
	if size < 0 || size > maxRepositorySnippetFileBytes {
		return false
	}
	if path == "" {
		return false
	}
	normalizedPath := strings.ToLower(filepath.ToSlash(path))
	if normalizedPath == "" {
		return false
	}
	baseName := pathpkg.Base(normalizedPath)
	if baseName == "." || baseName == "" {
		return false
	}
	if baseName == ".env" || strings.HasPrefix(baseName, ".env.") {
		return false
	}
	if _, blocked := blockedRepositorySnippetBaseNames[baseName]; blocked {
		return false
	}
	if _, blocked := blockedRepositorySnippetExtensions[filepath.Ext(baseName)]; blocked {
		return false
	}
	pathWithSentinel := "/" + strings.TrimPrefix(normalizedPath, "/")
	for _, suffix := range blockedRepositorySnippetPathSuffixes {
		if strings.HasSuffix(pathWithSentinel, suffix) {
			return false
		}
	}
	if isSensitiveRepositoryConfigPath(baseName) {
		return false
	}
	return true
}

// isSensitiveRepositoryConfigPath 识别常见明文凭据或 secrets 配置文件命名。
func isSensitiveRepositoryConfigPath(baseName string) bool {
	extension := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, extension)
	if extension == "" {
		for _, keyword := range blockedRepositorySnippetConfigKeywords {
			if strings.Contains(nameWithoutExt, keyword) {
				return true
			}
		}
		return false
	}
	if _, ok := blockedRepositorySnippetConfigExtensions[extension]; !ok {
		return false
	}
	for _, keyword := range blockedRepositorySnippetConfigKeywords {
		if strings.Contains(nameWithoutExt, keyword) {
			return true
		}
	}
	return false
}

// isBinaryContent 通过前缀字节判断文件是否为二进制内容。
func isBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	prefixBytes := content
	if len(prefixBytes) > binaryProbePrefixSize {
		prefixBytes = prefixBytes[:binaryProbePrefixSize]
	}
	if bytes.IndexByte(prefixBytes, 0x00) >= 0 {
		return true
	}
	for _, b := range prefixBytes {
		if b < 0x09 {
			return true
		}
	}
	return false
}
