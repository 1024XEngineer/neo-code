package transport

import (
	"fmt"
	"strings"
)

// ResolveListenAddress 解析网关监听地址，优先使用显式传入值，否则回退到平台默认地址。
func ResolveListenAddress(override string) (string, error) {
	normalized := strings.TrimSpace(override)
	if normalized != "" {
		if err := validateListenAddress(normalized); err != nil {
			return "", fmt.Errorf("invalid gateway listen address %q: %w", normalized, err)
		}
		return normalized, nil
	}
	return DefaultListenAddress()
}
