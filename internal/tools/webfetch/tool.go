package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/tools"
)

const (
	toolName                   = tools.ToolNameWebFetch
	htmlContentType            = "text/html"
	xhtmlContentType           = "application/xhtml+xml"
	reasonInvalidArguments     = "invalid arguments"
	reasonInvalidURL           = "invalid URL"
	reasonRequestFailed        = "request failed"
	reasonReadResponseFailed   = "read response failed"
	reasonUnsupportedType      = "unsupported content type"
	reasonEmptyContent         = "content is empty after extraction"
	reasonRedirectNotAllowed   = "redirect is not allowed"
	errorMessageUnexpectedHTTP = "unexpected HTTP status %s"
)

// Config controls how webfetch reads and filters remote content.
type Config struct {
	Timeout               time.Duration
	MaxResponseBytes      int64
	SupportedContentTypes []string
}

type Tool struct {
	client        *http.Client
	cfg           Config
	supportedText map[string]struct{}
}

type input struct {
	URL string `json:"url"`
}

type responseData struct {
	URL         string
	Status      string
	ContentType string
	Title       string
	Content     string
	Truncated   bool
}

type validationContextKey string

const bypassTargetValidationKey validationContextKey = "webfetch_bypass_target_validation"

var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// New creates a webfetch tool with bounded responses and content-type filtering.
func New(cfg Config) *Tool {
	normalized := normalizeConfig(cfg)
	return &Tool{
		client:        newHTTPClient(normalized.Timeout),
		cfg:           normalized,
		supportedText: newContentTypeSet(normalized.SupportedContentTypes),
	}
}

// newHTTPClient 创建禁止自动重定向的客户端，避免跨域重定向绕过上层网络权限校验。
func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: timeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialAddresses, err := resolveDialAddresses(ctx, address)
		if err != nil {
			return nil, err
		}
		return dialFirstReachable(ctx, dialer.DialContext, network, dialAddresses)
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (t *Tool) Name() string {
	return toolName
}

func (t *Tool) Description() string {
	return "Fetch readable web content with content-type filtering and bounded response size."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP or HTTPS URL to fetch.",
			},
		},
		"required": []string{"url"},
	}
}

// MicroCompactPolicy 声明 webfetch 工具的历史结果默认参与 micro compact 清理。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	in, err := decodeInput(call.Arguments)
	if err != nil {
		return t.newErrorResult(responseData{}, reasonInvalidArguments, err.Error()), fmt.Errorf("webfetch: parse input: %w", err)
	}

	targetURL, err := validateURL(in.URL)
	if err != nil {
		result := t.newErrorResult(responseData{URL: strings.TrimSpace(in.URL)}, reasonInvalidURL, err.Error())
		return result, fmt.Errorf("webfetch: validate url: %w", err)
	}
	if !bypassTargetValidation(ctx) {
		if err := validateFetchTarget(ctx, targetURL); err != nil {
			result := t.newErrorResult(responseData{URL: targetURL}, reasonInvalidURL, err.Error())
			return result, fmt.Errorf("webfetch: validate target: %w", err)
		}
	}

	resp, err := t.fetch(ctx, targetURL)
	if err != nil {
		result := t.newErrorResult(responseData{URL: targetURL}, reasonRequestFailed, err.Error())
		return result, fmt.Errorf("webfetch: fetch %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	return t.handleResponse(targetURL, resp)
}

func decodeInput(raw []byte) (input, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return input{}, err
	}
	return in, nil
}

func validateURL(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", fmt.Errorf("%s: url is required", toolName)
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s: url must start with http:// or https://", toolName)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("%s: url host is empty", toolName)
	}
	return parsed.String(), nil
}

// validateFetchTarget 校验目标主机是否命中本地或内网地址，防止 SSRF 访问敏感网络。
func validateFetchTarget(ctx context.Context, target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return err
	}

	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return fmt.Errorf("%s: url host is empty", toolName)
	}
	if isLocalHostName(hostname) {
		return fmt.Errorf("%s: target host is blocked", toolName)
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("%s: target host is blocked", toolName)
		}
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	records, err := lookupIPAddr(lookupCtx, hostname)
	if err != nil {
		return fmt.Errorf("%s: resolve host: %w", toolName, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%s: resolve host: empty result", toolName)
	}
	for _, record := range records {
		if isBlockedIP(record.IP) {
			return fmt.Errorf("%s: target host is blocked", toolName)
		}
	}
	return nil
}

// resolveDialAddresses 在真实拨号前校验并收敛可用目标地址集合，避免 DNS 重绑定导致的 TOCTOU 绕过。
func resolveDialAddresses(ctx context.Context, address string) ([]string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid dial address", toolName)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("%s: url host is empty", toolName)
	}
	if bypassTargetValidation(ctx) {
		return []string{address}, nil
	}
	if isLocalHostName(host) {
		return nil, fmt.Errorf("%s: target host is blocked", toolName)
	}

	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("%s: target host is blocked", toolName)
		}
		return []string{net.JoinHostPort(ip.String(), port)}, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	records, err := lookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve host: %w", toolName, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s: resolve host: empty result", toolName)
	}

	allowed := make([]string, 0, len(records))
	for _, record := range records {
		if !isBlockedIP(record.IP) {
			allowed = append(allowed, net.JoinHostPort(record.IP.String(), port))
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%s: target host is blocked", toolName)
	}
	return allowed, nil
}

type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// dialFirstReachable 依次尝试多个候选地址，命中首个可连通地址后立即返回，全部失败时返回最后一次错误。
func dialFirstReachable(ctx context.Context, dialFn dialContextFunc, network string, addresses []string) (net.Conn, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%s: invalid dial address", toolName)
	}
	var lastErr error
	for _, address := range addresses {
		conn, err := dialFn(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%s: invalid dial address", toolName)
}

// bypassTargetValidation 判断当前上下文是否显式跳过目标地址安全校验，仅供包内测试使用。
func bypassTargetValidation(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, ok := ctx.Value(bypassTargetValidationKey).(bool)
	return ok && enabled
}

// isLocalHostName 判断主机名是否属于本地回环域名变体。
func isLocalHostName(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	return normalized == "localhost" || strings.HasSuffix(normalized, ".localhost")
}

// isBlockedIP 判断 IP 是否属于回环、链路本地、私网或其他不应被 webfetch 访问的网段。
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func (t *Tool) fetch(ctx context.Context, targetURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", strings.Join(t.cfg.SupportedContentTypes, ", "))

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *Tool) handleResponse(targetURL string, resp *http.Response) (tools.ToolResult, error) {
	body, truncated, err := readBounded(resp.Body, t.cfg.MaxResponseBytes)
	if err != nil {
		data := responseData{
			URL:       targetURL,
			Status:    resp.Status,
			Truncated: truncated,
		}
		return t.newErrorResult(data, reasonReadResponseFailed, err.Error()), fmt.Errorf("webfetch: read response body: %w", err)
	}

	data := responseData{
		URL:         targetURL,
		Status:      resp.Status,
		ContentType: detectContentType(resp.Header.Get("Content-Type"), body),
		Truncated:   truncated,
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		location := strings.TrimSpace(resp.Header.Get("Location"))
		details := location
		if details == "" {
			details = resp.Status
		}
		return t.newErrorResult(data, reasonRedirectNotAllowed, details), fmt.Errorf("webfetch: redirect blocked: %s", details)
	}

	content, title, err := t.extractContent(data.ContentType, body)
	if err != nil {
		return t.newErrorResult(data, reasonUnsupportedType, err.Error()), fmt.Errorf("webfetch: extract content: %w", err)
	}

	data.Title = title
	data.Content = content

	if resp.StatusCode >= http.StatusBadRequest {
		reason := fmt.Sprintf(errorMessageUnexpectedHTTP, resp.Status)
		return t.newErrorResult(data, reason, content), fmt.Errorf("webfetch: "+errorMessageUnexpectedHTTP, resp.Status)
	}
	if strings.TrimSpace(content) == "" {
		return t.newErrorResult(data, reasonEmptyContent, ""), fmt.Errorf("webfetch: %s", reasonEmptyContent)
	}

	return t.newSuccessResult(data), nil
}

func readBounded(body io.Reader, limit int64) ([]byte, bool, error) {
	limited, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(limited)) > limit {
		return limited[:limit], true, nil
	}
	return limited, false, nil
}

func detectContentType(header string, body []byte) string {
	mediaType := normalizeContentType(header)
	if mediaType != "" {
		return mediaType
	}
	if len(body) == 0 {
		return ""
	}
	return normalizeContentType(http.DetectContentType(body))
}

func (t *Tool) extractContent(contentType string, body []byte) (string, string, error) {
	if !t.supports(contentType) {
		return "", "", fmt.Errorf("%s: %s", toolName, reasonUnsupportedType)
	}
	if isHTMLContentType(contentType) {
		text, title, err := extractHTMLContent(body)
		if err != nil {
			return "", "", fmt.Errorf("parse html: %w", err)
		}
		return text, title, nil
	}
	return normalizePlainText(body), "", nil
}

func (t *Tool) supports(contentType string) bool {
	_, ok := t.supportedText[contentType]
	return ok
}

func (t *Tool) newSuccessResult(data responseData) tools.ToolResult {
	return tools.ToolResult{
		Name:     t.Name(),
		Content:  formatSuccess(data),
		IsError:  false,
		Metadata: metadataFromResponse(data),
	}
}

func (t *Tool) newErrorResult(data responseData, reason string, details string) tools.ToolResult {
	return tools.ToolResult{
		Name:     t.Name(),
		Content:  formatError(data, reason, details),
		IsError:  true,
		Metadata: metadataFromResponse(data),
	}
}

func formatSuccess(data responseData) string {
	lines := formatCommonLines(data)
	if data.Title != "" {
		lines = append(lines, "title: "+data.Title)
	}
	return joinMessage(lines, data.Content)
}

func formatError(data responseData, reason string, details string) string {
	lines := []string{"tool error", "tool: webfetch"}
	if strings.TrimSpace(reason) != "" {
		lines = append(lines, "reason: "+strings.TrimSpace(reason))
	}
	lines = append(lines, formatCommonLines(data)...)
	if strings.TrimSpace(details) != "" {
		lines = append(lines, "details: "+strings.TrimSpace(details))
	}
	return strings.Join(lines, "\n")
}

func formatCommonLines(data responseData) []string {
	lines := make([]string, 0, 5)
	if data.URL != "" {
		lines = append(lines, "url: "+data.URL)
	}
	if data.Status != "" {
		lines = append(lines, "status: "+data.Status)
	}
	if data.ContentType != "" {
		lines = append(lines, "content_type: "+data.ContentType)
	}
	if data.Truncated {
		lines = append(lines, "truncated: true")
	}
	return lines
}

func joinMessage(lines []string, body string) string {
	header := strings.Join(lines, "\n")
	content := strings.TrimSpace(body)
	if content == "" {
		return header
	}
	if header == "" {
		return content
	}
	return header + "\n\n" + content
}

func metadataFromResponse(data responseData) map[string]any {
	metadata := map[string]any{
		"url":          data.URL,
		"status":       data.Status,
		"content_type": data.ContentType,
		"truncated":    data.Truncated,
	}
	if data.Title != "" {
		metadata["title"] = data.Title
	}
	return metadata
}

func normalizeConfig(cfg Config) Config {
	if cfg.Timeout <= 0 {
		cfg.Timeout = time.Duration(config.DefaultToolTimeoutSec) * time.Second
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = config.DefaultWebFetchMaxResponseBytes
	}
	if len(cfg.SupportedContentTypes) == 0 {
		cfg.SupportedContentTypes = config.DefaultWebFetchSupportedContentTypes()
	}

	normalized := make([]string, 0, len(cfg.SupportedContentTypes))
	seen := make(map[string]struct{}, len(cfg.SupportedContentTypes))
	for _, contentType := range cfg.SupportedContentTypes {
		mediaType := normalizeContentType(contentType)
		if mediaType == "" {
			continue
		}
		if _, exists := seen[mediaType]; exists {
			continue
		}
		seen[mediaType] = struct{}{}
		normalized = append(normalized, mediaType)
	}
	cfg.SupportedContentTypes = normalized
	return cfg
}

func newContentTypeSet(contentTypes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(contentTypes))
	for _, contentType := range contentTypes {
		set[contentType] = struct{}{}
	}
	return set
}

func normalizeContentType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err == nil {
		return mediaType
	}
	if index := strings.Index(trimmed, ";"); index >= 0 {
		return strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}

func isHTMLContentType(contentType string) bool {
	return contentType == htmlContentType || contentType == xhtmlContentType
}

func normalizePlainText(body []byte) string {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	return strings.TrimSpace(text)
}
