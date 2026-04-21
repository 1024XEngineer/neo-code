package chatcompletions

import (
	"context"
	"io"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
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

func TestBuildRequestUsesDefaultModelAndNormalizesTools(t *testing.T) {
	t.Parallel()

	cfg := provider.RuntimeConfig{DefaultModel: "gpt-default"}
	payload, err := BuildRequest(context.Background(), cfg, providertypes.GenerateRequest{
		SystemPrompt: "system",
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello"), providertypes.NewTextPart(" world")},
			},
		},
		Tools: []providertypes.ToolSpec{{
			Name:   "run",
			Schema: map[string]any{"type": "array"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if payload.Model != "gpt-default" {
		t.Fatalf("expected default model, got %q", payload.Model)
	}
	if !payload.Stream {
		t.Fatal("expected stream=true")
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("expected system+user messages, got %d", len(payload.Messages))
	}
	if payload.Messages[1].Content != "hello world" {
		t.Fatalf("expected collapsed user content, got %+v", payload.Messages[1].Content)
	}
	if len(payload.Tools) != 1 || payload.ToolChoice != "auto" {
		t.Fatalf("expected one auto tool, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
	if gotType, _ := payload.Tools[0].Function.Parameters["type"].(string); gotType != "object" {
		t.Fatalf("expected normalized object schema, got %q", gotType)
	}
}

func TestBuildRequestAndToOpenAIMessageErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing model", func(t *testing.T) {
		t.Parallel()

		_, err := BuildRequest(context.Background(), provider.RuntimeConfig{}, providertypes.GenerateRequest{})
		if err == nil || !strings.Contains(err.Error(), "model is empty") {
			t.Fatalf("expected model error, got %v", err)
		}
	})

	t.Run("session asset missing reader", func(t *testing.T) {
		t.Parallel()

		_, err := ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset_1", "image/png"),
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "session_asset reader is not configured") {
			t.Fatalf("expected missing reader error, got %v", err)
		}
	})

	t.Run("unsupported image source", func(t *testing.T) {
		t.Parallel()

		_, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: "unsupported",
				},
			}},
		}, nil, 1024, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
		if err == nil || !strings.Contains(err.Error(), "unsupported source type") {
			t.Fatalf("expected unsupported source type error, got %v", err)
		}
	})
}

func TestToOpenAIMessageMapsToolCallsAndSessionAsset(t *testing.T) {
	t.Parallel()

	reader := &stubAssetReader{
		data: map[string][]byte{"asset_1": []byte("PNG")},
		mime: map[string]string{"asset_1": "image/png"},
	}
	msg, used, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("look"),
			providertypes.NewSessionAssetImagePart("asset_1", "image/png"),
		},
		ToolCalls: []providertypes.ToolCall{{
			ID:        "call_1",
			Name:      "read_file",
			Arguments: "{\"path\":\"README.md\"}",
		}},
	}, reader, 1024, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
	if err != nil {
		t.Fatalf("toOpenAIMessageWithBudget() error = %v", err)
	}
	expectedBudgetBytes := provider.EstimateDataURLTransportBytes(int64(len("PNG")), "image/png")
	if used != expectedBudgetBytes {
		t.Fatalf("expected consumed session asset bytes=%d, got %d", expectedBudgetBytes, used)
	}
	parts, ok := msg.Content.([]MessageContentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected 2 multimodal parts, got %+v", msg.Content)
	}
	if parts[1].ImageURL == nil || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("expected encoded data url, got %+v", parts[1].ImageURL)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("expected mapped tool call, got %+v", msg.ToolCalls)
	}
}

func TestToOpenAIMessageWithBudgetRejectsDataURLTransportOverhead(t *testing.T) {
	t.Parallel()

	reader := &stubAssetReader{
		data: map[string][]byte{"asset_1": []byte("PN")},
		mime: map[string]string{"asset_1": "image/png"},
	}
	_, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewSessionAssetImagePart("asset_1", "image/png"),
		},
	}, reader, 4, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
	if err == nil || !strings.Contains(err.Error(), "session_asset total exceeds") {
		t.Fatalf("expected total budget error, got %v", err)
	}
}
