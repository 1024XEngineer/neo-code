package checkpoint

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	bashWriteRedirectRE = regexp.MustCompile(`(^|[^&\d])>{1,2}\s*[^\s&>]`)
	bashSedInplaceRE    = regexp.MustCompile(`\bsed\b[^|;&]*?\s(-i|-i\.[^\s]+|--in-place)`)
	bashAwkInplaceRE    = regexp.MustCompile(`\bawk\b[^|;&]*?-i\b`)
	bashGitWriteRE      = regexp.MustCompile(`\bgit\s+(checkout|restore|reset|apply|pull|merge|rebase|am|cherry-pick|revert|commit|add|rm|mv|stash|clean)\b`)
	bashPkgManagerRE    = regexp.MustCompile(`\b(npm|yarn|pnpm|bower)\s+(install|i|add|remove|uninstall|i)\b`)
	bashPipInstallRE    = regexp.MustCompile(`\bpip\s*\d*\s+(install|uninstall)\b`)
	bashGoInstallRE     = regexp.MustCompile(`\bgo\s+(get|install|mod\s+(download|tidy|vendor)|generate)\b`)
	bashCargoRE         = regexp.MustCompile(`\bcargo\s+(install|add|remove|update|build|fetch|generate)\b`)
	bashArchiveRE       = regexp.MustCompile(`\b(unzip|gunzip|bunzip2|tar)\b`)
	bashFindDeleteRE    = regexp.MustCompile(`\bfind\b[^|;&]*?(-delete|-exec\s+rm)`)
	bashTeeRE           = regexp.MustCompile(`\btee\b`)
	bashShellSplitRE    = regexp.MustCompile(`[;&|<>()\s{}` + "`" + `]+`)
)

// bashWriteCommands lists single-word commands that mutate files when invoked.
var bashWriteCommands = []string{
	"mv", "cp", "rm", "touch", "mkdir", "rmdir", "ln", "chmod", "chown",
	"dd", "patch", "install", "rsync", "shred", "truncate", "trash",
}

var bashWriteCommandRE = regexp.MustCompile(`\b(` + strings.Join(bashWriteCommands, "|") + `)\b`)

// BashLikelyWritesFiles 基于启发式判断 bash 命令是否可能写文件。
// 设计偏保守：宁可多 capture（返回 true），也不漏（false 时由 fingerprint 兜底）。
// 仅在能明确判定为只读时返回 false。
func BashLikelyWritesFiles(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}
	sanitized := stripHarmlessRedirects(cmd)
	if bashWriteRedirectRE.MatchString(sanitized) {
		return true
	}
	lower := strings.ToLower(sanitized)
	if bashWriteCommandRE.MatchString(lower) {
		return true
	}
	if bashSedInplaceRE.MatchString(lower) {
		return true
	}
	if bashAwkInplaceRE.MatchString(lower) {
		return true
	}
	if bashGitWriteRE.MatchString(lower) {
		return true
	}
	if bashPkgManagerRE.MatchString(lower) {
		return true
	}
	if bashPipInstallRE.MatchString(lower) {
		return true
	}
	if bashGoInstallRE.MatchString(lower) {
		return true
	}
	if bashCargoRE.MatchString(lower) {
		return true
	}
	if bashTeeRE.MatchString(lower) {
		return true
	}
	if bashFindDeleteRE.MatchString(lower) {
		return true
	}
	if bashArchiveRE.MatchString(lower) && bashHasArchiveExtractFlag(lower) {
		return true
	}
	return false
}

// SourceFilesInWorkdir 从命令中尝试提取 workdir 内的文件路径（保守估计）。
// 仅匹配看起来像源代码/配置/文本的扩展名，返回的路径可能不准确（启发式），由 fingerprint 兜底。
func SourceFilesInWorkdir(command, workdir string) []string {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	tokens := tokenizeBashArgs(command)
	seen := make(map[string]struct{})
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.Trim(tok, `"'`)
		if tok == "" {
			continue
		}
		if !hasRecognizedSourceExt(tok) {
			continue
		}
		abs := resolvePathAgainstWorkdir(tok, workdir)
		if abs == "" {
			continue
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokenizeBashArgs(cmd string) []string {
	parts := bashShellSplitRE.Split(cmd, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func resolvePathAgainstWorkdir(p, workdir string) string {
	if strings.ContainsAny(p, "*?[") {
		return ""
	}
	workdirClean := filepath.Clean(strings.TrimSpace(workdir))
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		if workdirClean == "" || workdirClean == "." {
			return ""
		}
		abs = filepath.Clean(filepath.Join(workdirClean, p))
	}
	if workdirClean == "" || workdirClean == "." {
		return abs
	}
	rel, err := filepath.Rel(workdirClean, abs)
	if err != nil {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return abs
}

func stripHarmlessRedirects(cmd string) string {
	r := strings.NewReplacer(
		"2>&1", "",
		"1>&2", "",
		">&2", "",
		">&-", "",
		"<&-", "",
		"&>&-", "",
	)
	return r.Replace(cmd)
}

func bashHasArchiveExtractFlag(lower string) bool {
	if strings.Contains(lower, "unzip") || strings.Contains(lower, "gunzip") || strings.Contains(lower, "bunzip2") {
		return true
	}
	if !strings.Contains(lower, "tar") {
		return false
	}
	for _, marker := range []string{" -x", " --extract", "tar x", "-xf", "-xv", "-xz", "-xj", "-xJ", "xvf"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

var bashSourceExts = map[string]struct{}{
	".go": {}, ".rs": {}, ".py": {}, ".js": {}, ".jsx": {}, ".ts": {}, ".tsx": {},
	".java": {}, ".c": {}, ".cpp": {}, ".cc": {}, ".cxx": {}, ".h": {}, ".hpp": {}, ".hxx": {},
	".rb": {}, ".php": {}, ".swift": {}, ".kt": {}, ".scala": {}, ".groovy": {},
	".md": {}, ".rst": {}, ".txt": {},
	".json": {}, ".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {}, ".conf": {}, ".cfg": {}, ".properties": {},
	".html": {}, ".htm": {}, ".xml": {}, ".css": {}, ".scss": {}, ".sass": {}, ".less": {},
	".vue": {}, ".svelte": {}, ".astro": {},
	".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {}, ".ps1": {},
	".sql": {}, ".graphql": {}, ".gql": {}, ".proto": {},
	".csv": {}, ".tsv": {}, ".log": {},
	".env": {}, ".lock": {},
}

func hasRecognizedSourceExt(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	if ext == "" {
		base := strings.ToLower(filepath.Base(p))
		switch base {
		case "dockerfile", "makefile", ".gitignore", ".dockerignore", ".env":
			return true
		}
		return false
	}
	_, ok := bashSourceExts[ext]
	return ok
}
