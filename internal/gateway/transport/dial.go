package transport

import "net"

// Dial 连接到本地网关 IPC 地址，按平台选择 UDS 或 Named Pipe。
func Dial(address string) (net.Conn, error) {
	return dial(address)
}
