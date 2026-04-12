package tools

import (
	"errors"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestApplyOutputLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		result            ToolResult
		limit             int
		wantContent       string
		wantTruncated     bool
		wantMetadataNil   bool
		wantPreserveValue any
	}{
		{
			name: "no limit keeps content",
			result: ToolResult{
				Content: "hello",
			},
			limit:           0,
			wantContent:     "hello",
			wantTruncated:   false,
			wantMetadataNil: true,
		},
		{
			name: "content within limit keeps metadata",
			result: ToolResult{
				Content:  "hello",
				Metadata: map[string]any{"path": "a.txt"},
			},
			limit:             10,
			wantContent:       "hello",
			wantTruncated:     false,
			wantMetadataNil:   false,
			wantPreserveValue: "a.txt",
		},
		{
			name: "content over limit truncates and marks metadata",
			result: ToolResult{
				Content: "hello world",
			},
			limit:           5,
			wantContent:     "hello" + truncatedSuffix,
			wantTruncated:   true,
			wantMetadataNil: false,
		},
		{
			name: "existing truncated true is preserved",
			result: ToolResult{
				Content:  "hello world",
				Metadata: map[string]any{"truncated": true},
			},
			limit:           5,
			wantContent:     "hello" + truncatedSuffix,
			wantTruncated:   true,
			wantMetadataNil: false,
		},
		{
			name: "existing truncated false is overwritten",
			result: ToolResult{
				Content:  "hello world",
				Metadata: map[string]any{"truncated": false},
			},
			limit:           5,
			wantContent:     "hello" + truncatedSuffix,
			wantTruncated:   true,
			wantMetadataNil: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ApplyOutputLimit(tt.result, tt.limit)
			if got.Content != tt.wantContent {
				t.Fatalf("expected content %q, got %q", tt.wantContent, got.Content)
			}

			if tt.wantMetadataNil {
				if got.Metadata != nil {
					t.Fatalf("expected nil metadata, got %#v", got.Metadata)
				}
				return
			}

			if got.Metadata == nil {
				t.Fatal("expected metadata to be initialized")
			}
			if truncated, _ := got.Metadata["truncated"].(bool); truncated != tt.wantTruncated {
				t.Fatalf("expected truncated=%v, got %#v", tt.wantTruncated, got.Metadata["truncated"])
			}
			if tt.wantPreserveValue != nil && got.Metadata["path"] != tt.wantPreserveValue {
				t.Fatalf("expected path metadata %v, got %v", tt.wantPreserveValue, got.Metadata["path"])
			}
		})
	}
}

func TestFormatHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		toolName   string
		reason     string
		details    string
		err        error
		wantReason string
		wantBody   []string
	}{
		{
			name:       "format trims fields",
			toolName:   " bash ",
			reason:     " failed ",
			details:    " bad input ",
			err:        errors.New("bash: failed"),
			wantReason: "failed",
			wantBody:   []string{"tool error", "tool: bash", "reason: failed", "details: bad input"},
		},
		{
			name:       "normalize without tool prefix keeps message",
			toolName:   "webfetch",
			reason:     "unsupported content type",
			details:    "",
			err:        errors.New("network unavailable"),
			wantReason: "network unavailable",
			wantBody:   []string{"tool error", "tool: webfetch", "reason: unsupported content type"},
		},
		{
			name:       "empty fields collapse cleanly",
			toolName:   "",
			reason:     "",
			details:    "",
			err:        nil,
			wantReason: "",
			wantBody:   []string{"tool error"},
		},
		{
			name:       "tool name empty keeps raw reason",
			toolName:   "",
			reason:     "boom",
			details:    "",
			err:        errors.New("bash: failed"),
			wantReason: "bash: failed",
			wantBody:   []string{"tool error", "reason: boom"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeErrorReason(tt.toolName, tt.err); got != tt.wantReason {
				t.Fatalf("expected reason %q, got %q", tt.wantReason, got)
			}

			body := FormatError(tt.toolName, tt.reason, tt.details)
			for _, fragment := range tt.wantBody {
				if !strings.Contains(body, fragment) {
					t.Fatalf("expected body to contain %q, got %q", fragment, body)
				}
			}

			result := NewErrorResult(tt.toolName, tt.reason, tt.details, map[string]any{"k": "v"})
			if !result.IsError {
				t.Fatal("expected error result")
			}
			if result.Name != tt.toolName {
				t.Fatalf("expected name %q, got %q", tt.toolName, result.Name)
			}
			if result.Metadata["k"] != "v" {
				t.Fatalf("expected metadata to be preserved, got %#v", result.Metadata)
			}
			if result.Content != body {
				t.Fatalf("expected content %q, got %q", body, result.Content)
			}
		})
	}
}

func TestSanitizeToolMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		input  map[string]any
		assert func(t *testing.T, got map[string]string)
	}{
		{
			name: "keeps allowlisted scalar keys and tool name",
			tool: "filesystem_edit",
			input: map[string]any{
				"path":          "internal/context/prompt.go",
				"search_length": 12,
				"raw_result":    strings.Repeat("x", 200),
				"complex":       map[string]any{"a": 1},
				"truncated":     true,
			},
			assert: func(t *testing.T, got map[string]string) {
				t.Helper()
				if got["tool_name"] != "filesystem_edit" {
					t.Fatalf("expected tool_name to be kept, got %#v", got)
				}
				if got["path"] != "internal/context/prompt.go" || got["search_length"] != "12" {
					t.Fatalf("expected allowlisted fields to be preserved, got %#v", got)
				}
				if got["raw_result"] != "" {
					t.Fatalf("expected raw_result to be dropped, got %#v", got)
				}
				if got["complex"] != "" {
					t.Fatalf("expected complex values to be dropped, got %#v", got)
				}
				if got["truncated"] != "true" {
					t.Fatalf("expected truncated to be preserved, got %#v", got)
				}
			},
		},
		{
			name: "caps retained keys and truncates long values",
			tool: "bash",
			input: map[string]any{
				"path":               strings.Repeat("a", 300),
				"relative_path":      "rel",
				"workdir":            "work",
				"root":               "root",
				"bytes":              1,
				"emitted_bytes":      2,
				"matched_files":      3,
				"replacement_length": 4,
			},
			assert: func(t *testing.T, got map[string]string) {
				t.Helper()
				if len(got) > maxProjectedToolMetadataKeys {
					t.Fatalf("expected metadata keys to be capped, got %#v", got)
				}
				if len(got["path"]) <= maxProjectedToolMetadataValueLen {
					t.Fatalf("expected long value to be truncated, got %q", got["path"])
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := SanitizeToolMetadata(tt.tool, tt.input)
			tt.assert(t, got)
		})
	}
}

func TestFormatToolMessageForModel(t *testing.T) {
	t.Parallel()

	message := providertypes.Message{
		Role:       providertypes.RoleTool,
		Content:    "ok",
		ToolCallID: "call-1",
		ToolMetadata: map[string]string{
			"tool_name":     "filesystem_edit",
			"path":          "internal/context/prompt.go",
			"search_length": "12",
			"truncated":     "true",
		},
	}

	got := FormatToolMessageForModel(message)
	fragments := []string{
		"tool result",
		"tool: filesystem_edit",
		"status: ok",
		"tool_call_id: call-1",
		"truncated: true",
		"meta.path: internal/context/prompt.go",
		"meta.search_length: 12",
		"\ncontent:\nok",
	}
	for _, fragment := range fragments {
		if !strings.Contains(got, fragment) {
			t.Fatalf("expected formatted result to contain %q, got %q", fragment, got)
		}
	}
}
