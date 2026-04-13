//go:build !windows

package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const defaultSocketFile = "neocode-gateway.sock"

// listenUDS 在 Unix 平台创建 UDS 监听器。
func listenUDS(endpoint string) (net.Listener, string, error) {
	socketPath := strings.TrimSpace(endpoint)
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), defaultSocketFile)
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, "", fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("cleanup stale socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", fmt.Errorf("listen uds: %w", err)
	}
	return &udsListener{Listener: listener, socketPath: socketPath}, socketPath, nil
}

// listenNPipe 在非 Windows 平台返回不支持错误。
func listenNPipe(_ string) (net.Listener, string, error) {
	return nil, "", errUnsupportedTransport
}

// udsListener 在关闭时同步清理 socket 文件。
type udsListener struct {
	net.Listener
	socketPath string
}

// Close 关闭监听器并删除 socket 文件。
func (l *udsListener) Close() error {
	err := l.Listener.Close()
	if removeErr := os.Remove(l.socketPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
		err = removeErr
	}
	return err
}
