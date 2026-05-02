package rules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const rulesFilePermission = 0o644

// GlobalRulePath 返回全局规则文件 AGENTS.md 的固定路径。
func GlobalRulePath(baseDir string) string {
	resolvedBaseDir := resolveBaseDir(baseDir)
	if strings.TrimSpace(resolvedBaseDir) == "" {
		return ""
	}
	return filepath.Join(resolvedBaseDir, agentsFileName)
}

// ProjectRulePath 返回项目根规则文件 AGENTS.md 的固定路径。
func ProjectRulePath(projectRoot string) string {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return ""
	}
	if !filepath.IsAbs(root) {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return ""
		}
		root = absRoot
	}

	info, err := os.Stat(root)
	if err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(root), agentsFileName)
}

// ReadGlobalRule 读取全局规则文件内容。
func ReadGlobalRule(ctx context.Context, baseDir string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	return readRuleDocument(GlobalRulePath(baseDir))
}

// ReadProjectRule 读取项目根规则文件内容。
func ReadProjectRule(ctx context.Context, projectRoot string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	return readRuleDocument(ProjectRulePath(projectRoot))
}

// WriteGlobalRule 覆写全局规则文件内容，并返回目标路径。
func WriteGlobalRule(ctx context.Context, baseDir string, content string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	target := GlobalRulePath(baseDir)
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("rules: global rule path is empty")
	}
	if err := writeRuleFile(target, content); err != nil {
		return "", err
	}
	return target, nil
}

// WriteProjectRule 覆写项目根规则文件内容，并返回目标路径。
func WriteProjectRule(ctx context.Context, projectRoot string, content string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	target := ProjectRulePath(projectRoot)
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("rules: project rule path is empty")
	}
	if err := writeRuleFile(target, content); err != nil {
		return "", err
	}
	return target, nil
}

// readRuleDocument 读取规则文件并应用统一裁剪语义。
func readRuleDocument(path string) (Document, error) {
	if strings.TrimSpace(path) == "" {
		return Document{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Document{}, nil
		}
		return Document{}, fmt.Errorf("rules: read %s: %w", path, err)
	}

	content, truncated := truncateRunes(strings.TrimSpace(string(data)), documentRuneLimit)
	return Document{
		Path:      path,
		Content:   content,
		Truncated: truncated,
	}, nil
}

// writeRuleFile 以 UTF-8 安全方式原子写入规则文件。
func writeRuleFile(path string, content string) error {
	if !utf8.ValidString(content) {
		return fmt.Errorf("rules: content must be valid UTF-8")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("rules: create rule dir %s: %w", dir, err)
	}

	tempFile, err := os.CreateTemp(dir, "agents-*.tmp")
	if err != nil {
		return fmt.Errorf("rules: create temp file for %s: %w", path, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.WriteString(content); err != nil {
		return fmt.Errorf("rules: write temp file for %s: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("rules: close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tempPath, rulesFilePermission); err != nil {
		return fmt.Errorf("rules: chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rules: commit file %s: %w", path, err)
	}
	return nil
}
