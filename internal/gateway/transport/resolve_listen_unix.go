//go:build !windows

package transport

// validateListenAddress 在非 Windows 平台不限制 IPC 地址形态，沿用现有 Unix Socket 地址解析策略。
func validateListenAddress(_ string) error {
	return nil
}
