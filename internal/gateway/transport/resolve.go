package transport

import "strings"

// ResolveListenAddress 解析网关监听地址，优先使用显式传入值，否则回退到平台默认地址。
func ResolveListenAddress(override string) (string, error) {
	normalized := strings.TrimSpace(override)
	if normalized != "" {
		return normalized, nil
	}
	return DefaultListenAddress()
}
