//go:build windows

package transport

const defaultWindowsNamedPipePath = `\\.\pipe\neocode-gateway`

// DefaultListenAddress 返回 Windows 系统默认监听地址。
func DefaultListenAddress() (string, error) {
	return defaultWindowsNamedPipePath, nil
}
