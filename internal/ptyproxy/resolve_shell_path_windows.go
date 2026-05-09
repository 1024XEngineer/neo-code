//go:build windows

package ptyproxy

import "strings"

// resolveShellPath 解析诊断元数据中的 shell 路径，Windows 默认与交互 shell 选择保持一致。
func resolveShellPath(shellOption string) string {
	if trimmed := strings.TrimSpace(shellOption); trimmed != "" {
		return trimmed
	}
	return defaultWindowsInteractiveShell()
}
