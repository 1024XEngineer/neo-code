package chatcompletions

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

type failingReadCloser struct{}

func (failingReadCloser) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingReadCloser) Close() error               { return nil }

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
		}, nil, 1024, providertypes.DefaultSessionAssetLimits())
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
	}, reader, 1024, providertypes.DefaultSessionAssetLimits())
	if err != nil {
		t.Fatalf("toOpenAIMessageWithBudget() error = %v", err)
	}
	if used <= 0 {
		t.Fatalf("expected consumed session asset bytes, got %d", used)
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

func TestToOpenAIMessageWithBudgetWrapperAndBudgetClamp(t *testing.T) {
	t.Parallel()

	t.Run("wrapper maps plain text message", func(t *testing.T) {
		t.Parallel()

		msg, used, err := ToOpenAIMessageWithBudget(
			context.Background(),
			providertypes.Message{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
			},
			nil,
			64,
			providertypes.DefaultSessionAssetLimits(),
		)
		if err != nil {
			t.Fatalf("ToOpenAIMessageWithBudget() error = %v", err)
		}
		if msg.Content != "hello" {
			t.Fatalf("expected text content, got %#v", msg.Content)
		}
		if used != 0 {
			t.Fatalf("expected zero asset bytes, got %d", used)
		}
	})

	t.Run("negative budget is clamped to zero", func(t *testing.T) {
		t.Parallel()

		reader := &stubAssetReader{
			data: map[string][]byte{"asset_1": []byte("PNG")},
			mime: map[string]string{"asset_1": "image/png"},
		}
		_, _, err := ToOpenAIMessageWithBudget(
			context.Background(),
			providertypes.Message{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset_1", "image/png")},
			},
			reader,
			-1,
			providertypes.DefaultSessionAssetLimits(),
		)
		if err == nil || !strings.Contains(err.Error(), "session_asset total exceeds") {
			t.Fatalf("expected session asset budget error, got %v", err)
		}
	})
}

func TestParseErrorAndHTMLHelpers(t *testing.T) {
	t.Parallel()

	t.Run("nil response", func(t *testing.T) {
		t.Parallel()

		err := ParseError(nil)
		if err == nil || !strings.Contains(err.Error(), "empty http response") {
			t.Fatalf("expected empty response error, got %v", err)
		}
	})

	t.Run("body read failure", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       failingReadCloser{},
			Header:     http.Header{},
		}
		err := ParseError(resp)
		if err == nil || !strings.Contains(err.Error(), "read error response") {
			t.Fatalf("expected read error branch, got %v", err)
		}
	})

	t.Run("json error message", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limit"}}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
		err := ParseError(resp)
		if err == nil || !strings.Contains(err.Error(), "rate limit") {
			t.Fatalf("expected parsed json message, got %v", err)
		}
	})

	t.Run("empty text body falls back to status", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader("   ")),
			Header:     http.Header{},
		}
		err := ParseError(resp)
		if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
			t.Fatalf("expected status fallback, got %v", err)
		}
	})

	t.Run("html payload is summarized", func(t *testing.T) {
		t.Parallel()

		body := "<html><body><h1>Oops</h1><p>gateway timeout</p></body></html>"
		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		}
		err := ParseError(resp)
		if err == nil {
			t.Fatal("expected provider error")
		}
		got := err.Error()
		if !strings.Contains(got, "upstream returned html error payload") {
			t.Fatalf("expected html summary marker, got %v", err)
		}
		if !strings.Contains(got, "snippet: Oops gateway timeout") {
			t.Fatalf("expected extracted snippet, got %v", err)
		}
	})

	t.Run("html detection without content type", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(bytes.NewBufferString("<!DOCTYPE html><html><body>fatal</body></html>")),
			Header:     http.Header{},
		}
		err := ParseError(resp)
		if err == nil || !strings.Contains(err.Error(), "content_type: text/html") {
			t.Fatalf("expected html summary with default content type, got %v", err)
		}
	})

	t.Run("non html payload returns plain body", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader("raw upstream failure")),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		}
		err := ParseError(resp)
		if err == nil || !strings.Contains(err.Error(), "raw upstream failure") {
			t.Fatalf("expected plain body fallback, got %v", err)
		}
	})
}

func TestErrorFormattingHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeErrorContentType(" Text/HTML ; charset=utf-8 "); got != "text/html" {
		t.Fatalf("normalizeErrorContentType() = %q", got)
	}
	if got := normalizeErrorContentType(""); got != "" {
		t.Fatalf("normalizeErrorContentType(empty) = %q", got)
	}

	if !isLikelyHTMLError("application/xhtml+xml", "ignored") {
		t.Fatal("expected xhtml content-type to be detected as html")
	}
	if !isLikelyHTMLError("", "<html><body>x</body></html>") {
		t.Fatal("expected html body marker to be detected")
	}
	if isLikelyHTMLError("text/plain", "normal error text") {
		t.Fatal("did not expect plain text to be detected as html")
	}

	long := strings.Repeat("x", htmlErrorSnippetMaxRunes+8)
	if got := extractErrorSnippet(long, htmlErrorSnippetMaxRunes); !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated snippet, got %q", got)
	}
	if got := extractErrorSnippet("data", 0); got != "" {
		t.Fatalf("expected empty snippet when maxRunes<=0, got %q", got)
	}

	if got := stripHTMLTags("<div>Hello</div><span>World</span>"); !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Fatalf("stripHTMLTags() unexpected output: %q", got)
	}
	if got := stripHTMLTags(" \n "); got != "" {
		t.Fatalf("stripHTMLTags(blank) = %q", got)
	}

	message := formatHTMLErrorMessage("", "", "<h1>Fail</h1><p>details</p>")
	if !strings.Contains(message, "status: unknown") || !strings.Contains(message, "content_type: text/html") {
		t.Fatalf("unexpected html summary: %q", message)
	}
}
