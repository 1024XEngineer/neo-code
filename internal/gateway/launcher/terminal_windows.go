//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"strings"
)

// launchTerminal 在 Windows 上通过 `cmd /c start` 拉起独立终端窗口执行命令。
func launchTerminal(command string) error {
	args := append([]string{"/c", "start"}, strings.Fields(command)...)
	if len(args) <= 2 {
		return fmt.Errorf("empty terminal command")
	}
	cmd := exec.Command("cmd", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch terminal on windows: %w", err)
	}
	return nil
}
