//go:build !windows && !darwin

package launcher

import "fmt"

// launchTerminal 在 Linux/其他平台先返回未实现错误，后续再接入具体终端适配。
func launchTerminal(command string) error {
	return fmt.Errorf("%w: run `%s` manually in your terminal", ErrTerminalUnsupported, command)
}
