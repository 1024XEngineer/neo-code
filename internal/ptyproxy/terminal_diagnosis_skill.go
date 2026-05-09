package ptyproxy

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	terminalDiagnosisSkillID           = "terminal-diagnosis"
	terminalDiagnosisSkillRelativePath = ".neocode/skills/terminal-diagnosis/SKILL.md"
)

//go:embed skills/terminal-diagnosis/SKILL.md
var terminalDiagnosisSkillMarkdown string

// EnsureTerminalDiagnosisSkillFile 确保 terminal-diagnosis Skill 文件存在于用户目录。
func EnsureTerminalDiagnosisSkillFile() error {
	skillPath, err := ResolveTerminalDiagnosisSkillPath()
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(skillPath); statErr == nil && !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	content := strings.TrimSpace(terminalDiagnosisSkillMarkdown)
	if content == "" {
		return fmt.Errorf("terminal diagnosis skill template is empty")
	}
	if err := os.WriteFile(skillPath, []byte(content+"\n"), 0o600); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	return nil
}

// ResolveTerminalDiagnosisSkillPath 返回用户目录中 terminal-diagnosis Skill 的目标路径。
func ResolveTerminalDiagnosisSkillPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, terminalDiagnosisSkillRelativePath), nil
}
