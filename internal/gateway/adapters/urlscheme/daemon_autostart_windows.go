//go:build windows

package urlscheme

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsDaemonRunRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsDaemonRunValueName    = "NeoCodeDaemon"
)

// installDaemonAutostart 在 Windows 用户注册表中写入 daemon 自启动命令。
func installDaemonAutostart(executablePath, listenAddress string) (string, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsDaemonRunRegistryPath, registry.SET_VALUE)
	if err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("open daemon autorun key failed: %v", err))
	}
	defer func() { _ = key.Close() }()

	command := fmt.Sprintf(`"%s" daemon serve --listen %s`, executablePath, strings.TrimSpace(listenAddress))
	if err := key.SetStringValue(windowsDaemonRunValueName, command); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("write daemon autorun value failed: %v", err))
	}
	return daemonAutostartModeWindowsRun, nil
}

// uninstallDaemonAutostart 删除 Windows 用户注册表中的 daemon 自启动命令。
func uninstallDaemonAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsDaemonRunRegistryPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("open daemon autorun key failed: %v", err))
	}
	defer func() { _ = key.Close() }()

	if err := key.DeleteValue(windowsDaemonRunValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("delete daemon autorun value failed: %v", err))
	}
	return nil
}

// daemonAutostartStatus 返回 Windows 用户注册表中的 daemon 自启动状态。
func daemonAutostartStatus() (daemonAutostartState, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsDaemonRunRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return daemonAutostartState{}, nil
		}
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("open daemon autorun key failed: %v", err))
	}
	defer func() { _ = key.Close() }()

	value, _, err := key.GetStringValue(windowsDaemonRunValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return daemonAutostartState{}, nil
		}
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("read daemon autorun value failed: %v", err))
	}
	if strings.TrimSpace(value) == "" {
		return daemonAutostartState{}, nil
	}
	return daemonAutostartState{Configured: true, Mode: daemonAutostartModeWindowsRun}, nil
}
