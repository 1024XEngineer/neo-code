package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

type stubAssetReader struct {
	data map[string][]byte
	mime map[string]string
}

func (s *stubAssetReader) Open(_ context.Context, assetID string) (io.ReadCloser, string, error) {
	content, ok := s.data[assetID]
	if !ok {
		return nil, "", io.EOF
	}
	return io.NopCloser(strings.NewReader(string(content))), s.mime[assetID], nil
}

func TestBuildRequestUsesDefaultModelAndMapsMessages(t *testing.T) {
	t.Parallel()

	cfg := provider.RuntimeConfig{DefaultModel: "gpt-default"}
	req := providertypes.GenerateRequest{
		SystemPrompt: "system prompt",
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleSystem,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("ignored-system")},
			},
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("hello"),
					providertypes.NewRemoteImagePart("https://example.com/a.png"),
				},
			},
			{
				Role: providertypes.RoleAssistant,
				ToolCalls: []providertypes.ToolCall{
					{ID: "call_1", Name: "read_file", Arguments: "{\"path\":\"README.md\"}"},
				},
			},
			{
				Role:       providertypes.RoleTool,
				ToolCallID: "call_1",
				Parts:      []providertypes.ContentPart{providertypes.NewTextPart("ok")},
			},
		},
		Tools: []providertypes.ToolSpec{
			{
				Name:        "read_file",
				Description: "read a file",
				Schema: map[string]any{
					"type": "array",
				},
			},
		},
	}

	payload, err := BuildRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if payload.Model != "gpt-default" {
		t.Fatalf("expected default model, got %q", payload.Model)
	}
	if payload.Instructions != "system prompt" {
		t.Fatalf("expected instructions from system prompt, got %q", payload.Instructions)
	}
	if !payload.Stream {
		t.Fatal("expected stream=true")
	}
	if len(payload.Input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(payload.Input))
	}
	if payload.Input[0].Role != providertypes.RoleUser {
		t.Fatalf("expected first input role user, got %q", payload.Input[0].Role)
	}
	if len(payload.Input[0].Content) != 2 {
		t.Fatalf("expected multimodal user content, got %+v", payload.Input[0].Content)
	}
	if payload.Input[1].Type != "function_call" || payload.Input[1].Name != "read_file" {
		t.Fatalf("expected assistant function_call input, got %+v", payload.Input[1])
	}
	if payload.Input[2].Type != "function_call_output" || payload.Input[2].CallID != "call_1" || payload.Input[2].Output != "ok" {
		t.Fatalf("expected tool output input item, got %+v", payload.Input[2])
	}
	if payload.ToolChoice != "auto" || len(payload.Tools) != 1 {
		t.Fatalf("expected one auto tool, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
	if gotType, _ := payload.Tools[0].Parameters["type"].(string); gotType != "object" {
		t.Fatalf("expected normalized schema type object, got %q", gotType)
	}
	if _, ok := payload.Tools[0].Parameters["properties"].(map[string]any); !ok {
		t.Fatalf("expected normalized schema properties map, got %+v", payload.Tools[0].Parameters["properties"])
	}
}

func TestBuildRequestValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing model", func(t *testing.T) {
		t.Parallel()

		_, err := BuildRequest(context.Background(), provider.RuntimeConfig{}, providertypes.GenerateRequest{})
		if err == nil || !strings.Contains(err.Error(), "model is empty") {
			t.Fatalf("expected model is empty error, got %v", err)
		}
	})

	t.Run("tool result requires tool_call_id", func(t *testing.T) {
		t.Parallel()

		_, err := BuildRequest(context.Background(), provider.RuntimeConfig{DefaultModel: "m"}, providertypes.GenerateRequest{
			Messages: []providertypes.Message{{
				Role:  providertypes.RoleTool,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("missing")},
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "tool result message requires tool_call_id") {
			t.Fatalf("expected tool_call_id error, got %v", err)
		}
	})

	t.Run("session asset total budget respects runtime limits", func(t *testing.T) {
		t.Parallel()

		assetReader := &stubAssetReader{
			data: map[string][]byte{
				"asset_1": []byte("PN"),
				"asset_2": []byte("PN"),
			},
			mime: map[string]string{
				"asset_1": "image/png",
				"asset_2": "image/png",
			},
		}

		_, err := BuildRequest(context.Background(), provider.RuntimeConfig{
			DefaultModel: "m",
			SessionAssetLimits: providertypes.SessionAssetLimits{
				MaxSessionAssetBytes:       2,
				MaxSessionAssetsTotalBytes: 3,
			},
		}, providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{
					Role:  providertypes.RoleUser,
					Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset_1", "image/png")},
				},
				{
					Role:  providertypes.RoleUser,
					Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset_2", "image/png")},
				},
			},
			SessionAssetReader: assetReader,
		})
		if err == nil || !strings.Contains(err.Error(), "session_asset total exceeds 3 bytes") {
			t.Fatalf("expected runtime session asset total budget error, got %v", err)
		}
	})
}

func TestToResponsesContentPartsAndRenderToolOutput(t *testing.T) {
	t.Parallel()

	parts, err := toResponsesContentParts([]any{"unsupported"})
	if err == nil || parts != nil {
		t.Fatalf("expected unsupported content type error, got parts=%+v err=%v", parts, err)
	}

	output, err := renderToolOutput([]any{"unsupported"})
	if err == nil || output != "" {
		t.Fatalf("expected unsupported tool output error, got output=%q err=%v", output, err)
	}
}
