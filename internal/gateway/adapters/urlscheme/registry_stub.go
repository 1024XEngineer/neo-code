//go:build !windows

package urlscheme

import "fmt"

// RegisterURLScheme 在非 Windows 平台返回 not_supported，保证编译与行为稳定。
func RegisterURLScheme(executablePath string) error {
	return newDispatchError(
		ErrorCodeNotSupported,
		fmt.Sprintf("url scheme registry is not supported on this platform: %s", executablePath),
	)
}
