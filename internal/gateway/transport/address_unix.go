//go:build !windows

package transport

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultUnixSocketRelativePath = ".neocode/run/gateway.sock"

// DefaultListenAddress 返回 Unix 系统默认监听地址。
func DefaultListenAddress() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("gateway: resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, defaultUnixSocketRelativePath), nil
}
