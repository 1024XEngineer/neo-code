//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
)

var (
	lookupPathForTerminalWindows  = exec.LookPath
	execCommandForTerminalWindows = exec.Command
)

// launchTerminal 在 Windows 上优先使用 Windows Terminal，失败时回退 cmd /c start。
func launchTerminal(command string) error {
	if _, err := lookupPathForTerminalWindows("wt.exe"); err == nil {
		wtCommand := execCommandForTerminalWindows("wt.exe", "new-tab", "cmd", "/k", command)
		if runErr := wtCommand.Run(); runErr == nil {
			return nil
		}
	}
	cmd := execCommandForTerminalWindows("cmd", "/c", "start", "", "cmd", "/k", command)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch terminal on windows: %w", err)
	}
	return nil
}
