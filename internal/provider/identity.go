package provider

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// ProviderIdentity 标识 discovery、缓存与去重使用的 provider 连接身份。
type ProviderIdentity struct {
	Driver                string `json:"driver"`
	BaseURL               string `json:"base_url"`
	ChatAPIMode           string `json:"chat_api_mode,omitempty"`
	ChatEndpointPath      string `json:"chat_endpoint_path,omitempty"`
	DiscoveryEndpointPath string `json:"discovery_endpoint_path,omitempty"`
}

// Key 返回稳定的 provider 身份键，用于缓存命名与去重。
func (i ProviderIdentity) Key() string {
	parts := []string{i.Driver, i.BaseURL}
	if strings.TrimSpace(i.ChatAPIMode) != "" {
		parts = append(parts, i.ChatAPIMode)
	}
	if strings.TrimSpace(i.ChatEndpointPath) != "" {
		parts = append(parts, i.ChatEndpointPath)
	}
	if strings.TrimSpace(i.DiscoveryEndpointPath) != "" {
		parts = append(parts, i.DiscoveryEndpointPath)
	}
	return strings.Join(parts, "|")
}

// String 返回可读身份字符串，保持与 Key 一致。
func (i ProviderIdentity) String() string {
	return i.Key()
}

// NormalizeKey 统一执行大小写折叠与空白裁剪，保证跨层比较稳定。
func NormalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// NormalizeProviderDriver 规范化 driver 名称，避免大小写与空白导致身份漂移。
func NormalizeProviderDriver(driver string) string {
	return NormalizeKey(driver)
}

// NormalizeProviderChatEndpointPath 规范化聊天端点路径，沿用 discovery 路径安全规则。
func NormalizeProviderChatEndpointPath(endpointPath string) (string, error) {
	return NormalizeProviderDiscoveryEndpointPath(endpointPath)
}

// NormalizeProviderDiscoveryEndpointPath 规范化模型发现端点路径，只允许相对路径。
func NormalizeProviderDiscoveryEndpointPath(endpointPath string) (string, error) {
	value := strings.TrimSpace(endpointPath)
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("provider discovery endpoint path %q is invalid", endpointPath)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("provider discovery endpoint path %q is invalid: %w", endpointPath, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(value, "//") {
		return "", fmt.Errorf("provider discovery endpoint path %q must be a relative path", endpointPath)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("provider discovery endpoint path %q must not contain query or fragment", endpointPath)
	}

	for _, segment := range strings.Split(value, "/") {
		if strings.TrimSpace(segment) == ".." {
			return "", fmt.Errorf("provider discovery endpoint path %q must not contain '..'", endpointPath)
		}
	}

	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	cleaned := path.Clean(value)
	if !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("provider discovery endpoint path %q is invalid", endpointPath)
	}
	if cleaned != "/" {
		cleaned = strings.TrimRight(cleaned, "/")
	}
	return cleaned, nil
}

// NormalizeProviderDiscoverySettings 根据 driver 规范化 discovery 设置，并在受支持场景补齐默认值。
func NormalizeProviderDiscoverySettings(driver string, endpointPath string) (string, error) {
	candidateEndpointPath := strings.TrimSpace(endpointPath)

	if candidateEndpointPath == "" {
		candidateEndpointPath = DiscoveryEndpointPathModels
	}
	_ = NormalizeProviderDriver(driver)

	normalizedEndpointPath, err := NormalizeProviderDiscoveryEndpointPath(candidateEndpointPath)
	if err != nil {
		return "", err
	}
	return normalizedEndpointPath, nil
}

// NormalizeProviderBaseURL 将 provider 接入地址规范为可比较的稳定形式。
func NormalizeProviderBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("provider base_url is empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("provider base_url %q is invalid: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("provider base_url %q must include scheme and host", raw)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("provider base_url %q must not include userinfo", raw)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil

	if cleaned := path.Clean(strings.TrimSpace(parsed.Path)); cleaned == "." {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimRight(cleaned, "/")
	}
	return parsed.String(), nil
}

// NewProviderIdentity 根据 driver 与 base_url 构造基础 provider 身份。
func NewProviderIdentity(driver string, baseURL string) (ProviderIdentity, error) {
	normalizedDriver := NormalizeProviderDriver(driver)
	if normalizedDriver == "" {
		return ProviderIdentity{}, fmt.Errorf("provider driver is empty")
	}

	normalizedBaseURL, err := NormalizeProviderBaseURL(baseURL)
	if err != nil {
		return ProviderIdentity{}, err
	}

	return ProviderIdentity{
		Driver:  normalizedDriver,
		BaseURL: normalizedBaseURL,
	}, nil
}

// NormalizeProviderIdentity 根据 driver 规则归一化连接身份，保留缓存去重所需字段。
func NormalizeProviderIdentity(identity ProviderIdentity) (ProviderIdentity, error) {
	normalizedDriver := NormalizeProviderDriver(identity.Driver)
	if normalizedDriver == "" {
		return ProviderIdentity{}, fmt.Errorf("provider driver is empty")
	}

	normalizedBaseURL, err := NormalizeProviderBaseURL(identity.BaseURL)
	if err != nil {
		return ProviderIdentity{}, err
	}

	switch normalizedDriver {
	case DriverOpenAICompat:
		chatAPIMode, err := NormalizeProviderChatAPIMode(identity.ChatAPIMode)
		if err != nil {
			return ProviderIdentity{}, err
		}
		chatEndpointPath, err := NormalizeProviderChatEndpointPath(identity.ChatEndpointPath)
		if err != nil {
			return ProviderIdentity{}, err
		}
		discoveryEndpointPath, err := NormalizeProviderDiscoverySettings(identity.Driver, identity.DiscoveryEndpointPath)
		if err != nil {
			return ProviderIdentity{}, err
		}
		return ProviderIdentity{
			Driver:                normalizedDriver,
			BaseURL:               normalizedBaseURL,
			ChatAPIMode:           chatAPIMode,
			ChatEndpointPath:      chatEndpointPath,
			DiscoveryEndpointPath: discoveryEndpointPath,
		}, nil
	case DriverGemini, DriverAnthropic:
		return ProviderIdentity{
			Driver:  normalizedDriver,
			BaseURL: normalizedBaseURL,
		}, nil
	default:
		discoveryEndpointPath, err := NormalizeProviderDiscoverySettings(identity.Driver, identity.DiscoveryEndpointPath)
		if err != nil {
			return ProviderIdentity{}, err
		}
		return ProviderIdentity{
			Driver:                normalizedDriver,
			BaseURL:               normalizedBaseURL,
			DiscoveryEndpointPath: discoveryEndpointPath,
		}, nil
	}
}
