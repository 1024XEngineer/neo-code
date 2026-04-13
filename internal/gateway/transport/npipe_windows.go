//go:build windows

package transport

import (
	"fmt"
	"net"
	"strings"

	"github.com/natefinch/npipe"
)

const defaultPipeName = `\\.\pipe\neocode-gateway`

// listenNPipe 在 Windows 平台创建 Named Pipe 监听器。
func listenNPipe(endpoint string) (net.Listener, string, error) {
	pipeAddress := strings.TrimSpace(endpoint)
	if pipeAddress == "" {
		pipeAddress = defaultPipeName
	}
	listener, err := npipe.Listen(pipeAddress)
	if err != nil {
		return nil, "", fmt.Errorf("listen npipe: %w", err)
	}
	return listener, pipeAddress, nil
}

// listenUDS 在 Windows 平台返回不支持错误。
func listenUDS(_ string) (net.Listener, string, error) {
	return nil, "", errUnsupportedTransport
}
