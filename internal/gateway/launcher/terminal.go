package launcher

import (
	"errors"
	"fmt"
	"strings"
)

// ErrTerminalUnsupported 表示当前平台尚未实现终端拉起能力。
var ErrTerminalUnsupported = errors.New("terminal launch is not supported on this platform")

// LaunchTerminal 负责按平台策略拉起独立终端并执行指定命令。
func LaunchTerminal(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("empty terminal command")
	}
	return launchTerminal(command)
}
