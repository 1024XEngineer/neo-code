//go:build !windows && !darwin && !linux

package urlscheme

import "fmt"

// installDaemonAutostart 在未适配平台上返回 not_supported。
func installDaemonAutostart(_ string, _ string) (string, error) {
	return "", newDispatchError(ErrorCodeNotSupported, "http daemon autostart is not supported on this platform")
}

// uninstallDaemonAutostart 在未适配平台上返回 not_supported。
func uninstallDaemonAutostart() error {
	return newDispatchError(ErrorCodeNotSupported, "http daemon autostart is not supported on this platform")
}

// daemonAutostartStatus 在未适配平台上返回 not_supported。
func daemonAutostartStatus() (daemonAutostartState, error) {
	return daemonAutostartState{}, newDispatchError(
		ErrorCodeNotSupported,
		fmt.Sprintf("http daemon autostart status is not supported on this platform: %s", DaemonHostsAlias),
	)
}
