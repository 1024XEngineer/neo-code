package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/tools"
)

const (
	toolName                   = "webfetch"
	htmlContentType            = "text/html"
	xhtmlContentType           = "application/xhtml+xml"
	reasonInvalidArguments     = "invalid arguments"
	reasonInvalidURL           = "invalid URL"
	reasonRequestFailed        = "request failed"
	reasonReadResponseFailed   = "read response failed"
	reasonUnsupportedType      = "unsupported content type"
	reasonEmptyContent         = "content is empty after extraction"
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

// New creates a webfetch tool with bounded responses and content-type filtering.
func New(cfg Config) *Tool {
	normalized := normalizeConfig(cfg)
	return &Tool{
		client: &http.Client{
			Timeout: normalized.Timeout,
		},
		cfg:           normalized,
		supportedText: newContentTypeSet(normalized.SupportedContentTypes),
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

	content, title, err := t.extractContent(data.ContentType, body)
	if err != nil {
		return t.newErrorResult(data, reasonUnsupportedType, ""), fmt.Errorf("webfetch: extract content: %w", err)
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
	lines := append([]string{"webfetch error"}, formatCommonLines(data)...)
	lines = append(lines, "reason: "+reason)
	return joinMessage(lines, details)
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
