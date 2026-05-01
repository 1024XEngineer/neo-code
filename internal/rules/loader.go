package rules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	agentsFileName    = "AGENTS.md"
	documentRuneLimit = 4000
	defaultRulesDir   = ".neocode"
)

// DefaultTruncationNotice 是规则文件超出注入预算时附加的统一提示。
const DefaultTruncationNotice = "\n[truncated to fit rule file limit]\n"

// Document 表示单个规则文件的已加载内容快照。
type Document struct {
	Path      string
	Content   string
	Truncated bool
}

// Snapshot 表示当前轮可见的全局与项目规则快照。
type Snapshot struct {
	GlobalAGENTS  Document
	ProjectAGENTS Document
}

// Loader 定义规则快照的最小加载能力。
type Loader interface {
	Load(ctx context.Context, projectRoot string) (Snapshot, error)
}

type fileLoader struct {
	baseDir string
}

// NewLoader 创建基于本地文件系统的规则加载器。
func NewLoader(baseDir string) Loader {
	return &fileLoader{
		baseDir: strings.TrimSpace(baseDir),
	}
}

// Load 读取项目根与全局 AGENTS.md，并返回统一快照。
func (l *fileLoader) Load(ctx context.Context, projectRoot string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	projectDoc, err := l.loadProjectDocument(ctx, projectRoot)
	if err != nil {
		return Snapshot{}, err
	}
	globalDoc, err := l.loadGlobalDocument(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		GlobalAGENTS:  globalDoc,
		ProjectAGENTS: projectDoc,
	}, nil
}

// loadProjectDocument 读取项目根下的 AGENTS.md 作为项目规则。
func (l *fileLoader) loadProjectDocument(ctx context.Context, projectRoot string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	target := ProjectRulePath(projectRoot)
	if strings.TrimSpace(target) == "" {
		return Document{}, nil
	}
	if !filepath.IsAbs(target) {
		return Document{}, fmt.Errorf("rules: project rule path %s is not absolute", target)
	}
	return readRuleDocument(target)
}

// loadGlobalDocument 读取全局 AGENTS.md 作为跨项目默认规则。
func (l *fileLoader) loadGlobalDocument(ctx context.Context) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	target := GlobalRulePath(l.baseDir)
	if strings.TrimSpace(target) == "" {
		return Document{}, nil
	}
	return readRuleDocument(target)
}

// resolveBaseDir 解析全局规则目录，默认回落到 ~/.neocode。
func resolveBaseDir(baseDir string) string {
	trimmed := strings.TrimSpace(baseDir)
	if trimmed != "" {
		return filepath.Clean(trimmed)
	}

	home := strings.TrimSpace(os.Getenv("HOME"))
	if !filepath.IsAbs(home) {
		var err error
		home, err = os.UserHomeDir()
		if err != nil || !filepath.IsAbs(strings.TrimSpace(home)) {
			return ""
		}
	}
	return filepath.Join(home, defaultRulesDir)
}

// truncateRunes 按 rune 数量裁剪文本，避免破坏 UTF-8 多字节字符。
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
