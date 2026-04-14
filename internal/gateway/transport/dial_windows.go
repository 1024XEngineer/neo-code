//go:build windows

package transport

import (
	"net"

	"github.com/Microsoft/go-winio"
)

var dialPipeFn = winio.DialPipe

// dial 在 Windows 系统上通过 Named Pipe 连接网关。
func dial(address string) (net.Conn, error) {
	return dialPipeFn(address, nil)
}
