//go:build windows

package urlscheme

import "fmt"

// RegisterURLScheme 在 Windows 上注册 neocode:// 协议入口（当前为最小占位实现）。
func RegisterURLScheme(executablePath string) error {
	return newDispatchError(
		ErrorCodeNotSupported,
		fmt.Sprintf("url scheme registry is not implemented yet: %s", executablePath),
	)
}
