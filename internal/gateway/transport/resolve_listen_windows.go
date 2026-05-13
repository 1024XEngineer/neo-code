//go:build windows

package transport

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	windowsNamedPipePrefix      = `\\.\pipe\`
	windowsNamedPipeAltPrefix   = `\\?\pipe\`
	gatewayListenAddressHintMsg = "Windows IPC listen address must be a named pipe path like \\\\.\\pipe\\neocode-gateway; use --http-listen for TCP endpoints"
)

// validateListenAddress 校验 Windows 下的网关 IPC 监听地址格式，避免误把 TCP 地址传给 Named Pipe 监听。
func validateListenAddress(address string) error {
	normalized := strings.TrimSpace(address)
	if strings.Contains(normalized, "://") {
		return nil
	}
	lowerAddress := strings.ToLower(normalized)
	if strings.HasPrefix(lowerAddress, windowsNamedPipePrefix) || strings.HasPrefix(lowerAddress, windowsNamedPipeAltPrefix) {
		return nil
	}
	if looksLikeTCPListenAddress(normalized) {
		return fmt.Errorf(gatewayListenAddressHintMsg)
	}
	return nil
}

// looksLikeTCPListenAddress 判断地址是否近似 host:port 形态，用于识别误传给 IPC 的 TCP 监听参数。
func looksLikeTCPListenAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		lastColon := strings.LastIndex(address, ":")
		if lastColon <= 0 || lastColon >= len(address)-1 {
			return false
		}
		host = strings.TrimSpace(address[:lastColon])
		port = strings.TrimSpace(address[lastColon+1:])
	}
	if host == "" || port == "" {
		return false
	}
	_, parseErr := strconv.Atoi(port)
	return parseErr == nil
}
