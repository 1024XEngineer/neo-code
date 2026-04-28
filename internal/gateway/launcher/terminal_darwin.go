//go:build darwin

package launcher

import (
	"fmt"
	"os/exec"
	"strings"
)

// launchTerminal 在 macOS 上通过 AppleScript 拉起 Terminal 并执行命令。
func launchTerminal(command string) error {
	script := fmt.Sprintf(
		`tell app "Terminal" to do script "%s"`,
		escapeAppleScriptString(command),
	)
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch terminal on darwin: %w", err)
	}
	return nil
}

// escapeAppleScriptString 处理 AppleScript 字符串中的转义字符，避免命令被截断。
func escapeAppleScriptString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(escaped, `"`, `\"`)
}
