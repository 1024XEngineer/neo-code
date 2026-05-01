//go:build !windows

package client

import (
	"os"
	"syscall"
)

// isUnsafeGatewayAutoSpawnLogHardLink 在 Unix 平台识别多硬链接文件，避免日志路径被旁路复用。
func isUnsafeGatewayAutoSpawnLogHardLink(fileInfo os.FileInfo) bool {
	if fileInfo == nil {
		return false
	}
	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return false
	}
	return stat.Nlink > 1
}
