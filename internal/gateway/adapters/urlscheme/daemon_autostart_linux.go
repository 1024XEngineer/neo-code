//go:build linux

package urlscheme

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	linuxDaemonSystemdServiceName = "neocode-daemon.service"
	linuxDaemonDesktopFileName    = "neocode-daemon.desktop"
)

// installDaemonAutostart 在 Linux 上优先安装 systemd user service，不可用时回落 desktop autostart。
func installDaemonAutostart(executablePath, listenAddress string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}

	if isLinuxSystemdUserAvailable() {
		if mode, err := installLinuxSystemdAutostart(homeDir, executablePath, listenAddress); err == nil {
			return mode, nil
		}
	}
	return installLinuxDesktopAutostart(homeDir, executablePath, listenAddress)
}

// uninstallDaemonAutostart 删除 Linux systemd/desktop 两类自启动配置。
func uninstallDaemonAutostart() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}

	systemdServicePath := filepath.Join(homeDir, ".config", "systemd", "user", linuxDaemonSystemdServiceName)
	desktopPath := filepath.Join(homeDir, ".config", "autostart", linuxDaemonDesktopFileName)

	if isLinuxSystemdUserAvailable() {
		_ = exec.Command("systemctl", "--user", "disable", "--now", linuxDaemonSystemdServiceName).Run()
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	if err := os.Remove(systemdServicePath); err != nil && !os.IsNotExist(err) {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("remove systemd service failed: %v", err))
	}
	if err := os.Remove(desktopPath); err != nil && !os.IsNotExist(err) {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("remove desktop autostart failed: %v", err))
	}
	return nil
}

// daemonAutostartStatus 返回 Linux 自启动配置状态（systemd 优先）。
func daemonAutostartStatus() (daemonAutostartState, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}
	systemdServicePath := filepath.Join(homeDir, ".config", "systemd", "user", linuxDaemonSystemdServiceName)
	if _, err := os.Stat(systemdServicePath); err == nil {
		return daemonAutostartState{Configured: true, Mode: daemonAutostartModeSystemdUser}, nil
	} else if !os.IsNotExist(err) {
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("stat systemd service failed: %v", err))
	}

	desktopPath := filepath.Join(homeDir, ".config", "autostart", linuxDaemonDesktopFileName)
	if _, err := os.Stat(desktopPath); err == nil {
		return daemonAutostartState{Configured: true, Mode: daemonAutostartModeDesktop}, nil
	} else if !os.IsNotExist(err) {
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("stat desktop autostart failed: %v", err))
	}
	return daemonAutostartState{}, nil
}

// isLinuxSystemdUserAvailable 判断 systemd user manager 是否可用。
func isLinuxSystemdUserAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	if err := exec.Command("systemctl", "--user", "show-environment").Run(); err != nil {
		return false
	}
	return true
}

// installLinuxSystemdAutostart 写入并启用 systemd user service。
func installLinuxSystemdAutostart(homeDir, executablePath, listenAddress string) (string, error) {
	serviceDir := filepath.Join(homeDir, ".config", "systemd", "user")
	servicePath := filepath.Join(serviceDir, linuxDaemonSystemdServiceName)
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("prepare systemd user directory failed: %v", err))
	}
	if err := os.WriteFile(servicePath, []byte(buildLinuxSystemdService(executablePath, listenAddress)), 0o644); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("write systemd service failed: %v", err))
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return "", mapURLSchemeCommandError("systemctl", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", linuxDaemonSystemdServiceName).Run(); err != nil {
		return "", mapURLSchemeCommandError("systemctl", err)
	}
	return daemonAutostartModeSystemdUser, nil
}

// installLinuxDesktopAutostart 写入 XDG desktop autostart 配置。
func installLinuxDesktopAutostart(homeDir, executablePath, listenAddress string) (string, error) {
	autostartDir := filepath.Join(homeDir, ".config", "autostart")
	desktopPath := filepath.Join(autostartDir, linuxDaemonDesktopFileName)
	if err := os.MkdirAll(autostartDir, 0o755); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("prepare desktop autostart directory failed: %v", err))
	}
	if err := os.WriteFile(desktopPath, []byte(buildLinuxDaemonDesktopEntry(executablePath, listenAddress)), 0o644); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("write desktop autostart failed: %v", err))
	}
	return daemonAutostartModeDesktop, nil
}

// buildLinuxSystemdService 生成 daemon systemd user service 内容。
func buildLinuxSystemdService(executablePath, listenAddress string) string {
	escapedExecutable := strings.ReplaceAll(executablePath, `"`, `\"`)
	escapedListen := strings.ReplaceAll(strings.TrimSpace(listenAddress), `"`, `\"`)
	return `[Unit]
Description=NeoCode HTTP Daemon
After=default.target

[Service]
Type=simple
ExecStart="` + escapedExecutable + `" daemon serve --listen "` + escapedListen + `"
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
`
}

// buildLinuxDaemonDesktopEntry 生成 daemon desktop autostart 条目。
func buildLinuxDaemonDesktopEntry(executablePath, listenAddress string) string {
	escapedExecutable := strings.ReplaceAll(executablePath, `"`, `\"`)
	escapedListen := strings.ReplaceAll(strings.TrimSpace(listenAddress), `"`, `\"`)
	return `[Desktop Entry]
Type=Application
Name=NeoCode Daemon
Exec="` + escapedExecutable + `" daemon serve --listen "` + escapedListen + `"
X-GNOME-Autostart-enabled=true
NoDisplay=true
`
}
