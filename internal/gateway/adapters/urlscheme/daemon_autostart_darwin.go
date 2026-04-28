//go:build darwin

package urlscheme

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	darwinDaemonLaunchAgentName = "io.neocode.daemon"
	darwinDaemonLaunchAgentFile = darwinDaemonLaunchAgentName + ".plist"
)

// installDaemonAutostart 在 macOS 用户目录写入 LaunchAgent 并加载。
func installDaemonAutostart(executablePath, listenAddress string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}
	launchAgentDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentDir, 0o755); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("prepare launchagents directory failed: %v", err))
	}
	plistPath := filepath.Join(launchAgentDir, darwinDaemonLaunchAgentFile)
	if err := os.WriteFile(plistPath, []byte(buildDarwinDaemonLaunchAgent(executablePath, listenAddress)), 0o644); err != nil {
		return "", newDispatchError(ErrorCodeInternal, fmt.Sprintf("write launch agent failed: %v", err))
	}

	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return "", mapURLSchemeCommandError("launchctl", err)
	}
	return daemonAutostartModeLaunchAgent, nil
}

// uninstallDaemonAutostart 卸载并删除 macOS LaunchAgent。
func uninstallDaemonAutostart() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", darwinDaemonLaunchAgentFile)
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return newDispatchError(ErrorCodeInternal, fmt.Sprintf("remove launch agent failed: %v", err))
	}
	return nil
}

// daemonAutostartStatus 返回 macOS LaunchAgent 配置状态。
func daemonAutostartStatus() (daemonAutostartState, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve home directory failed: %v", err))
	}
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", darwinDaemonLaunchAgentFile)
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return daemonAutostartState{}, nil
		}
		return daemonAutostartState{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("stat launch agent failed: %v", err))
	}
	return daemonAutostartState{Configured: true, Mode: daemonAutostartModeLaunchAgent}, nil
}

// buildDarwinDaemonLaunchAgent 生成 LaunchAgent plist 内容。
func buildDarwinDaemonLaunchAgent(executablePath, listenAddress string) string {
	escapedExecutable := escapePlistXML(executablePath)
	escapedListen := escapePlistXML(strings.TrimSpace(listenAddress))
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + darwinDaemonLaunchAgentName + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + escapedExecutable + `</string>
		<string>daemon</string>
		<string>serve</string>
		<string>--listen</string>
		<string>` + escapedListen + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`
}

// escapePlistXML 转义 plist 中的 XML 特殊字符。
func escapePlistXML(value string) string {
	replaced := strings.ReplaceAll(value, "&", "&amp;")
	replaced = strings.ReplaceAll(replaced, "<", "&lt;")
	replaced = strings.ReplaceAll(replaced, ">", "&gt;")
	replaced = strings.ReplaceAll(replaced, `"`, "&quot;")
	return strings.ReplaceAll(replaced, `'`, "&apos;")
}
