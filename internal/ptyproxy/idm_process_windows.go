//go:build windows

package ptyproxy

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isProcessAliveByPID 在 Windows 平台通过 GetExitCodeProcess 探测进程是否存活。
func isProcessAliveByPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == windowsProcessStillActive
}
