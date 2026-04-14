//go:build !windows

package transport

import "net"

// dial 在 Unix 系统上通过 UDS 连接网关。
func dial(address string) (net.Conn, error) {
	return net.Dial("unix", address)
}
