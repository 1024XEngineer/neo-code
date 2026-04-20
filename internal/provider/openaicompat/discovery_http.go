package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const maxDiscoveryResponseBodyBytes int64 = 2 * 1024 * 1024
const maxHTTPErrorSummaryBytes int64 = 4 * 1024

var (
	bearerTokenPattern       = regexp.MustCompile(`(?i)\bbearer\s+([a-z0-9\-._~+/]+=*)`)
	headerSecretPattern      = regexp.MustCompile(`(?i)\b(x-?api-?key|api[_-]?key|authorization)\b\s*[:=]\s*([^\s,;]+)`)
	jsonSecretPattern        = regexp.MustCompile(`(?i)"(x-?api-?key|api[_-]?key|authorization)"\s*:\s*"[^"]*"`)
	providerKeyLikeIDPattern = regexp.MustCompile(`\bsk-[a-zA-Z0-9]{8,}\b`)
)

// RequestConfig 描述通用 HTTP discovery 请求所需参数。
type RequestConfig struct {
	Driver       string
	BaseURL      string
	EndpointPath string
	APIKey       string
}

// RequestConfigFromRuntime 基于运行时配置生成 discovery/http 请求参数。
func RequestConfigFromRuntime(cfg provider.RuntimeConfig) (RequestConfig, error) {
	normalizedEndpointPath, err := provider.NormalizeProviderDiscoveryEndpointPath(cfg.DiscoveryEndpointPath)
	if err != nil {
		return RequestConfig{}, provider.NewDiscoveryConfigError(err.Error())
	}

	discoveryEndpointPath, err := provider.NormalizeProviderDiscoverySettings(cfg.Driver, normalizedEndpointPath)
	if err != nil {
		return RequestConfig{}, provider.NewDiscoveryConfigError(err.Error())
	}

	return RequestConfig{
		Driver:       cfg.Driver,
		BaseURL:      cfg.BaseURL,
		EndpointPath: discoveryEndpointPath,
		APIKey:       cfg.APIKey,
	}, nil
}

// DiscoverRawModels 通过通用 HTTP discovery 拉取并提取原始模型数组。
func DiscoverRawModels(ctx context.Context, client *http.Client, cfg RequestConfig) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("provider discovery: http client is nil")
	}

	endpointPath, err := provider.NormalizeProviderDiscoveryEndpointPath(cfg.EndpointPath)
	if err != nil {
		return nil, provider.NewDiscoveryConfigError(err.Error())
	}
	if endpointPath == "" {
		return nil, provider.NewDiscoveryConfigError(
			"provider discovery endpoint path is empty; set discovery_endpoint_path or switch to model_source=manual",
		)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if endpointPath != "" {
		endpoint += endpointPath
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: build models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	applyAuthHeaders(req.Header, cfg.APIKey)

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

	rawModels, err := ExtractRawModels(payload, discoveryResponseProfileGeneric)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: decode models response: %w", err)
	}
	return rawModels, nil
}

// DiscoverModelDescriptors 通过通用 discovery 流程返回标准化模型描述。
func DiscoverModelDescriptors(
	ctx context.Context,
	client *http.Client,
	cfg RequestConfig,
) ([]providertypes.ModelDescriptor, error) {
	rawModels, err := DiscoverRawModels(ctx, client, cfg)
	if err != nil {
		return nil, err
	}

	descriptors := make([]providertypes.ModelDescriptor, 0, len(rawModels))
	for _, raw := range rawModels {
		descriptor, ok := providertypes.DescriptorFromRawModel(raw)
		if !ok {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return providertypes.MergeModelDescriptors(descriptors), nil
}

// parseHTTPError 将 discovery HTTP 错误映射为可分类 ProviderError。
func parseHTTPError(resp *http.Response) error {
	message := httpErrorMessage(resp.StatusCode)
	if summary := readHTTPErrorSummary(resp.Body, maxHTTPErrorSummaryBytes); summary != "" {
		message += "; upstream body: " + summary
	}
	return provider.NewProviderErrorFromStatus(resp.StatusCode, message)
}

// httpErrorMessage 生成 discovery 请求对应的可操作错误文案。
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

// wrapTransportError 统一归类 discovery 传输错误。
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

// readHTTPErrorSummary 读取并清洗受限长度的响应体摘要。
func readHTTPErrorSummary(body io.Reader, limit int64) string {
	if body == nil || limit <= 0 {
		return ""
	}
	payload, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return ""
	}
	truncated := int64(len(payload)) > limit
	if truncated {
		payload = payload[:limit]
	}
	summary := sanitizePrintableText(payload)
	if summary == "" {
		return ""
	}
	summary = redactSensitiveSummary(summary)
	if truncated {
		return summary + " ...(truncated)"
	}
	return summary
}

// sanitizePrintableText 清理不可打印字符并压缩空白。
func sanitizePrintableText(payload []byte) string {
	var b strings.Builder
	lastWasSpace := false

	for _, r := range string(payload) {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if lastWasSpace {
				continue
			}
			b.WriteByte(' ')
			lastWasSpace = true
			continue
		}
		if !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
		lastWasSpace = false
	}
	return strings.TrimSpace(b.String())
}

// redactSensitiveSummary 脱敏错误摘要中的密钥与令牌。
func redactSensitiveSummary(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}

	redacted := bearerTokenPattern.ReplaceAllString(summary, "Bearer [REDACTED]")
	redacted = headerSecretPattern.ReplaceAllString(redacted, "$1: [REDACTED]")
	redacted = jsonSecretPattern.ReplaceAllStringFunc(redacted, redactJSONSecretField)
	redacted = providerKeyLikeIDPattern.ReplaceAllString(redacted, "sk-[REDACTED]")
	return redacted
}

// redactJSONSecretField 保留 JSON 字段名并清空敏感值。
func redactJSONSecretField(matched string) string {
	idx := strings.Index(matched, ":")
	if idx < 0 {
		return matched
	}
	return matched[:idx+1] + ` "[REDACTED]"`
}

// isTimeoutTransportError 判断传输错误是否由超时触发。
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

// applyAuthHeaders 为 openai-compatible 模型发现请求注入鉴权头。
func applyAuthHeaders(header http.Header, apiKey string) {
	if header == nil {
		return
	}
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return
	}
	header.Set("Authorization", "Bearer "+trimmed)
}
