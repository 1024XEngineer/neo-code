package transport

import (
	"errors"
	"net"
)

// cleanupListener 在关闭底层监听器后执行额外清理逻辑。
type cleanupListener struct {
	net.Listener
	cleanup func() error
}

// newCleanupListener 包装监听器并注入清理钩子。
func newCleanupListener(listener net.Listener, cleanup func() error) net.Listener {
	if cleanup == nil {
		return listener
	}
	return &cleanupListener{
		Listener: listener,
		cleanup:  cleanup,
	}
}

// Close 关闭监听器并执行额外清理。
func (l *cleanupListener) Close() error {
	return errors.Join(l.Listener.Close(), l.cleanup())
}
