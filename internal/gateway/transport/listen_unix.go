//go:build !windows

package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

const (
	// unixSocketDirPerm 定义 Unix socket 父目录权限（仅当前用户可访问）。
	unixSocketDirPerm os.FileMode = 0o700
	// unixSocketFilePerm 定义 Unix socket 文件权限（仅当前用户可读写）。
	unixSocketFilePerm os.FileMode = 0o600
)

// Listen 在 Unix 系统上启动 UDS 监听并在关闭时清理 socket 文件。
func Listen(address string) (net.Listener, error) {
	socketDir := filepath.Dir(address)
	created, err := ensureSocketDir(socketDir)
	if err != nil {
		return nil, err
	}
	if created {
		if err := os.Chmod(socketDir, unixSocketDirPerm); err != nil {
			return nil, fmt.Errorf("gateway: set socket dir permission: %w", err)
		}
	}

	if err := removeStaleUnixSocket(address); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", address)
	if err != nil {
		return nil, fmt.Errorf("gateway: listen unix socket: %w", err)
	}
	if err := os.Chmod(address, unixSocketFilePerm); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("gateway: set socket file permission: %w", err)
	}

	return newCleanupListener(listener, func() error {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("gateway: remove unix socket: %w", err)
		}
		return nil
	}), nil
}

// ensureSocketDir 确保 socket 父目录可用，并返回该目录是否由当前流程创建。
func ensureSocketDir(socketDir string) (bool, error) {
	info, err := os.Stat(socketDir)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("gateway: socket dir path exists and is not directory: %s", socketDir)
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("gateway: stat socket dir: %w", err)
	}

	if err := os.MkdirAll(socketDir, unixSocketDirPerm); err != nil {
		return false, fmt.Errorf("gateway: create socket dir: %w", err)
	}
	return true, nil
}

// removeStaleUnixSocket 清理历史残留的 socket 文件，避免监听失败。
func removeStaleUnixSocket(address string) error {
	info, err := os.Lstat(address)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("gateway: stat unix socket path: %w", err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("gateway: unix socket path exists and is not socket: %s", address)
	}

	if err := os.Remove(address); err != nil {
		return fmt.Errorf("gateway: remove stale unix socket: %w", err)
	}

	return nil
}
