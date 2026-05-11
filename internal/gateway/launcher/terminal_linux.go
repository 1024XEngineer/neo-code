//go:build !windows && !darwin

package launcher

import (
	"fmt"
	"os/exec"
	"runtime"
)

var (
	lookupPathForTerminalLinux  = exec.LookPath
	execCommandForTerminalLinux = exec.Command
)

// launchTerminal 在 Linux 上优先尝试 gnome-terminal，再回退到 x-terminal-emulator。
func launchTerminal(command string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%w: run `%s` manually in your terminal", ErrTerminalUnsupported, command)
	}
	if err := launchWithLinuxTerminal("gnome-terminal", []string{"--", "bash", "-lc", command}); err == nil {
		return nil
	}
	if err := launchWithLinuxTerminal("x-terminal-emulator", []string{"-e", "bash", "-lc", command}); err == nil {
		return nil
	}
	return fmt.Errorf("%w: install gnome-terminal/x-terminal-emulator or run `%s` manually", ErrTerminalUnsupported, command)
}

// launchWithLinuxTerminal 通过指定终端模拟器拉起命令。
func launchWithLinuxTerminal(binary string, args []string) error {
	if _, err := lookupPathForTerminalLinux(binary); err != nil {
		return err
	}
	cmd := execCommandForTerminalLinux(binary, args...)
	return cmd.Run()
}
