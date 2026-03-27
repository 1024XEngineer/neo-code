package webfetch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dust/neo-code/internal/tools"
)

type Tool struct {
	client    *http.Client
	bodyLimit int64
}

type input struct {
	URL string `json:"url"`
}

func New(timeout time.Duration) *Tool {
	return &Tool{
		client: &http.Client{
			Timeout: timeout,
		},
		bodyLimit: 256 * 1024,
	}
}

func (t *Tool) Name() string {
	return "webfetch"
}

func (t *Tool) Description() string {
	return "Fetch text content from a web page using GET with bounded response size."
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
	var in input
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		return tools.ToolResult{Name: t.Name()}, errors.New("webfetch: url must start with http:// or https://")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.bodyLimit))
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	result := tools.ToolResult{
		Name:    t.Name(),
		Content: string(body),
		IsError: resp.StatusCode >= 400,
		Metadata: map[string]any{
			"url":    in.URL,
			"status": resp.Status,
		},
	}
	if result.IsError {
		return result, errors.New(resp.Status)
	}
	return result, nil
}
