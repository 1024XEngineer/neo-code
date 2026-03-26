package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	htmlnode "golang.org/x/net/html"

	"neocode/internal/tools"
)

const (
	maxResponseBytes = 256 * 1024
	requestTimeout   = 20 * time.Second
)

// FetchTool fetches text content from a safe subset of URLs.
type FetchTool struct {
	client *http.Client
}

// NewFetchTool constructs the web fetch tool.
func NewFetchTool() *FetchTool {
	return &FetchTool{
		client: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// Name returns the stable tool name.
func (t *FetchTool) Name() string {
	return "web_fetch"
}

// Description describes the tool for the model.
func (t *FetchTool) Description() string {
	return "Fetch a web page over HTTP or HTTPS and return plain text content."
}

// Schema returns the JSON schema for tool arguments.
func (t *FetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The HTTP or HTTPS URL to fetch.",
			},
		},
		"required": []string{"url"},
	}
}

// Execute performs the HTTP request and extracts readable text.
func (t *FetchTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = call

	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return tools.Result{}, fmt.Errorf("url is required")
	}

	parsedURL, err := url.Parse(args.URL)
	if err != nil {
		return tools.Result{}, fmt.Errorf("invalid url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return tools.Result{}, fmt.Errorf("unsupported url scheme %q", parsedURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("User-Agent", "NeoCode/0.1")

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.Result{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return tools.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{}, fmt.Errorf("unexpected status %s", resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	bodyText := string(body)
	if strings.Contains(contentType, "text/html") {
		bodyText = extractHTMLText(bodyText)
	}
	bodyText = truncateText(strings.TrimSpace(bodyText), maxResponseBytes/2)
	if bodyText == "" {
		bodyText = "[empty response body]"
	}

	return tools.Result{
		Content: fmt.Sprintf("url: %s\nstatus: %s\ncontent-type: %s\n\n%s", parsedURL.String(), resp.Status, contentType, bodyText),
		Metadata: map[string]any{
			"url":         parsedURL.String(),
			"status_code": resp.StatusCode,
		},
	}, nil
}

func extractHTMLText(source string) string {
	doc, err := htmlnode.Parse(strings.NewReader(source))
	if err != nil {
		return stripWhitespace(htmlstd.UnescapeString(source))
	}

	var chunks []string
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if node.Type == htmlnode.ElementNode && (node.Data == "script" || node.Data == "style" || node.Data == "noscript") {
			return
		}
		if node.Type == htmlnode.TextNode {
			text := stripWhitespace(htmlstd.UnescapeString(node.Data))
			if text != "" {
				chunks = append(chunks, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return strings.Join(chunks, "\n")
}

func stripWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n\n[truncated]"
}
