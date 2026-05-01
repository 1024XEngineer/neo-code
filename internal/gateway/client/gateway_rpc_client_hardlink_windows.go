//go:build windows

package client

import "os"

// isUnsafeGatewayAutoSpawnLogHardLink 在 Windows 平台暂不执行硬链接计数检测，仅保留软链接拦截。
func isUnsafeGatewayAutoSpawnLogHardLink(_ os.FileInfo) bool {
	return false
}
