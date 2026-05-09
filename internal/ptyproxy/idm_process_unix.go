//go:build !windows

package ptyproxy

import (
	"errors"
	"syscall"
)

// isProcessAliveByPID 在 Unix 平台通过 signal 0 探测进程是否存活。
func isProcessAliveByPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
