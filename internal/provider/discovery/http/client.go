package httpdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/discovery"
)

const maxDiscoveryResponseBodyBytes int64 = 2 * 1024 * 1024

// RequestConfig 描述通用 HTTP discovery 请求的必要输入。
type RequestConfig struct {
	BaseURL           string
	EndpointPath      string
	DiscoveryProtocol string
	ResponseProfile   string
	AuthStrategy      string
	APIKey            string
	APIVersion        string
}

// DiscoverRawModels 通过通用 HTTP discovery 协议拉取模型列表并输出标准化的原始模型对象切片。
func DiscoverRawModels(ctx context.Context, client *http.Client, cfg RequestConfig) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("provider discovery: http client is nil")
	}

	discoveryProtocol := provider.NormalizeProviderDiscoveryProtocol(cfg.DiscoveryProtocol)
	if discoveryProtocol == "" {
		discoveryProtocol = provider.DiscoveryProtocolCustomHTTPJSON
	}

	endpointPath, err := provider.NormalizeProviderDiscoveryEndpointPath(cfg.EndpointPath)
	if err != nil {
		return nil, provider.NewDiscoveryConfigError(err.Error())
	}
	if endpointPath == "" {
		endpointPath = provider.DiscoveryEndpointPathModels
	}

	responseProfile, err := resolveResponseProfile(discoveryProtocol, cfg.ResponseProfile)
	if err != nil {
		return nil, provider.NewDiscoveryConfigError(err.Error())
	}

	endpoint := discovery.ResolveEndpoint(cfg.BaseURL, endpointPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: build models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	provider.ApplyAuthHeaders(req.Header, cfg.AuthStrategy, cfg.APIKey, cfg.APIVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, wrapTransportError(err)
	}
	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, parseHTTPError(resp)
	}

	limitedBody := io.LimitReader(resp.Body, maxDiscoveryResponseBodyBytes+1)
	rawPayload, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: read models response: %w", err)
	}
	if int64(len(rawPayload)) > maxDiscoveryResponseBodyBytes {
		return nil, provider.NewProviderErrorFromStatus(
			http.StatusRequestEntityTooLarge,
			fmt.Sprintf("provider discovery: models response body too large (limit=%d bytes)", maxDiscoveryResponseBodyBytes),
		)
	}

	var payload any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, fmt.Errorf("provider discovery: decode models response: %w", err)
	}

	rawModels, err := discovery.ExtractRawModels(payload, responseProfile)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: decode models response: %w", err)
	}
	return rawModels, nil
}

// resolveResponseProfile 根据 discovery protocol 与显式配置决定响应提取策略。
func resolveResponseProfile(discoveryProtocol string, profile string) (string, error) {
	normalizedProfile, err := provider.NormalizeProviderDiscoveryResponseProfile(profile)
	if err != nil {
		return "", err
	}
	if normalizedProfile != "" {
		return normalizedProfile, nil
	}

	switch discoveryProtocol {
	case provider.DiscoveryProtocolGeminiModels:
		return provider.DiscoveryResponseProfileGemini, nil
	case provider.DiscoveryProtocolOpenAIModels:
		return provider.DiscoveryResponseProfileOpenAI, nil
	default:
		return provider.DiscoveryResponseProfileGeneric, nil
	}
}

// parseHTTPError 将 discovery HTTP 错误映射为可分类 ProviderError。
func parseHTTPError(resp *http.Response) error {
	_, _ = io.Copy(io.Discard, resp.Body)
	message := httpErrorMessage(resp.StatusCode)
	return provider.NewProviderErrorFromStatus(resp.StatusCode, message)
}

// httpErrorMessage 为 discovery 请求生成可操作错误文案，便于用户区分端点、鉴权与上游异常。
func httpErrorMessage(statusCode int) string {
	switch statusCode {
	case http.StatusNotFound:
		return "provider discovery: models endpoint not found (404); check discovery endpoint path"
	case http.StatusUnauthorized:
		return "provider discovery: unauthorized (401); check api key, auth strategy, and environment variable"
	case http.StatusForbidden:
		return "provider discovery: forbidden (403); check provider permissions"
	}
	if statusCode >= http.StatusInternalServerError {
		return fmt.Sprintf("provider discovery: models endpoint request failed (status=%d); upstream server error", statusCode)
	}
	return fmt.Sprintf("provider discovery: models endpoint request failed (status=%d)", statusCode)
}

// wrapTransportError 统一归类 discovery 传输层失败，区分 timeout 与 network 错误。
func wrapTransportError(err error) error {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown transport error"
	}
	if isTimeoutTransportError(err) {
		return provider.NewTimeoutProviderError("provider discovery: send models request timeout: " + message)
	}
	return provider.NewNetworkProviderError("provider discovery: send models request: " + message)
}

// isTimeoutTransportError 判断网络错误是否由超时触发。
func isTimeoutTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
