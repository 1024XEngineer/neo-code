package chatcompletions

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

type errReadCloser struct{}

func (errReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReadCloser) Close() error {
	return nil
}

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

func TestBuildRequestThinkingConfigAndContinuity(t *testing.T) {
	t.Parallel()

	payload, err := BuildRequest(context.Background(), provider.RuntimeConfig{DefaultModel: "gpt-default"}, providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{
				Role:             providertypes.RoleAssistant,
				Parts:            []providertypes.ContentPart{providertypes.NewTextPart("hello")},
				ThinkingMetadata: []byte(`{"reasoning":"step by step"}`),
			},
		},
		ThinkingConfig: &providertypes.ThinkingConfig{
			Enabled: true,
			Effort:  "high",
		},
	})
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if payload.ReasoningEffort != "high" {
		t.Fatalf("expected reasoning effort to be preserved, got %q", payload.ReasoningEffort)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].ReasoningContent != "step by step" {
		t.Fatalf("expected reasoning continuity content, got %+v", payload.Messages)
	}

	disabled, err := BuildRequest(context.Background(), provider.RuntimeConfig{DefaultModel: "gpt-default"}, providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
		},
		ThinkingConfig: &providertypes.ThinkingConfig{Enabled: false},
	})
	if err != nil {
		t.Fatalf("BuildRequest() disabled error = %v", err)
	}
	if disabled.ReasoningEffort != "none" {
		t.Fatalf("expected disabled reasoning effort marker, got %q", disabled.ReasoningEffort)
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

	t.Run("invalid message parts", func(t *testing.T) {
		t.Parallel()

		_, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{{
				Kind: "invalid",
			}},
		}, nil, 1024, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
		if err == nil || !strings.Contains(err.Error(), "invalid message parts") {
			t.Fatalf("expected invalid parts error, got %v", err)
		}
	})

	t.Run("session asset missing id", func(t *testing.T) {
		t.Parallel()

		_, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: providertypes.ImageSourceSessionAsset,
					Asset:      &providertypes.AssetRef{},
				},
			}},
		}, &stubAssetReader{}, 1024, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
		if err == nil || !strings.Contains(err.Error(), "invalid message parts") {
			t.Fatalf("expected invalid parts error, got %v", err)
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

func TestToOpenAIMessageWithBudgetRemoteImageAndNegativeBudget(t *testing.T) {
	t.Parallel()

	msg, used, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("caption"),
			providertypes.NewRemoteImagePart("https://example.com/demo.png"),
		},
	}, nil, -1, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
	if err != nil {
		t.Fatalf("toOpenAIMessageWithBudget() error = %v", err)
	}
	if used != 0 {
		t.Fatalf("expected used bytes = 0 for remote image, got %d", used)
	}
	parts, ok := msg.Content.([]MessageContentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected 2 multimodal parts, got %+v", msg.Content)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://example.com/demo.png" {
		t.Fatalf("expected remote image url passthrough, got %+v", parts[1].ImageURL)
	}
}

func TestToOpenAIMessageWithBudgetSessionAssetReadError(t *testing.T) {
	t.Parallel()

	_, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewSessionAssetImagePart("missing", "image/png"),
		},
	}, &stubAssetReader{}, 1024, session.MaxSessionAssetBytes, provider.DefaultRequestAssetBudget())
	if err == nil || !strings.Contains(err.Error(), "open session_asset") {
		t.Fatalf("expected read asset failure, got %v", err)
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

func TestToOpenAIMessageWithBudgetDelegates(t *testing.T) {
	t.Parallel()

	msg, used, err := ToOpenAIMessageWithBudget(
		context.Background(),
		providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		},
		nil,
		1024,
		session.MaxSessionAssetBytes,
		provider.DefaultRequestAssetBudget(),
	)
	if err != nil {
		t.Fatalf("ToOpenAIMessageWithBudget() error = %v", err)
	}
	if used != 0 {
		t.Fatalf("expected used bytes = 0, got %d", used)
	}
	if msg.Content != "hello" {
		t.Fatalf("expected collapsed content, got %#v", msg.Content)
	}
}

func TestParseError(t *testing.T) {
	t.Parallel()

	t.Run("nil response", func(t *testing.T) {
		t.Parallel()

		err := ParseError(nil)
		if err == nil || !strings.Contains(err.Error(), "empty http response") {
			t.Fatalf("expected empty response error, got %v", err)
		}
	})

	t.Run("read body failure", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       errReadCloser{},
		})
		if err == nil || !strings.Contains(err.Error(), "read error response") {
			t.Fatalf("expected read error response, got %v", err)
		}
	})

	t.Run("json error payload", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid token"}}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		})
		if err == nil || !strings.Contains(err.Error(), "invalid token") {
			t.Fatalf("expected parsed json error message, got %v", err)
		}
	})

	t.Run("empty body fallback to status", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("   ")),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		})
		if err == nil || !strings.Contains(err.Error(), "403 Forbidden") {
			t.Fatalf("expected status fallback, got %v", err)
		}
	})

	t.Run("html payload by header", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body: io.NopCloser(strings.NewReader(
				`<!doctype html><html><body><h1>Gateway Error</h1><p>backend exploded</p></body></html>`,
			)),
			Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		})
		if err == nil {
			t.Fatal("expected provider error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "upstream returned html error payload") {
			t.Fatalf("expected html normalization marker, got %q", msg)
		}
		if strings.Contains(strings.ToLower(msg), "<html") {
			t.Fatalf("expected html tags stripped from message, got %q", msg)
		}
	})

	t.Run("html payload by body signature", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader("<html><body>Oops</body></html>")),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		})
		if err == nil || !strings.Contains(err.Error(), "upstream returned html error payload") {
			t.Fatalf("expected html payload normalization, got %v", err)
		}
	})

	t.Run("plain text payload", func(t *testing.T) {
		t.Parallel()

		err := ParseError(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found detail")),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		})
		if err == nil || !strings.Contains(err.Error(), "not found detail") {
			t.Fatalf("expected plain text body in provider error, got %v", err)
		}
	})
}

func TestErrorPayloadHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeErrorContentType(" text/html; charset=utf-8 "); got != "text/html" {
		t.Fatalf("unexpected normalized content type: %q", got)
	}
	if got := normalizeErrorContentType(""); got != "" {
		t.Fatalf("expected empty content type, got %q", got)
	}

	if !isLikelyHTMLError("application/xhtml+xml", "plain") {
		t.Fatal("expected xhtml content type recognized as html")
	}
	if !isLikelyHTMLError("", "<!doctype html><html></html>") {
		t.Fatal("expected doctype signature recognized as html")
	}
	if isLikelyHTMLError("text/plain", "plain text only") {
		t.Fatal("did not expect plain text body to be recognized as html")
	}

	msg := formatHTMLErrorMessage("", "", "<html><body>hello</body></html>")
	if !strings.Contains(msg, "status: unknown") {
		t.Fatalf("expected unknown status fallback, got %q", msg)
	}
	if !strings.Contains(msg, "content_type: text/html") {
		t.Fatalf("expected default content type fallback, got %q", msg)
	}
	if !strings.Contains(msg, "snippet: hello") {
		t.Fatalf("expected stripped snippet, got %q", msg)
	}

	longBody := "<p>" + strings.Repeat("a", htmlErrorSnippetMaxRunes+20) + "</p>"
	snippet := extractErrorSnippet(longBody, htmlErrorSnippetMaxRunes)
	if !strings.HasSuffix(snippet, "...") {
		t.Fatalf("expected truncated snippet suffix, got %q", snippet)
	}
	if got := extractErrorSnippet("x", 0); got != "" {
		t.Fatalf("expected empty snippet when budget <= 0, got %q", got)
	}
	if got := extractErrorSnippet("<div></div>", 10); !strings.HasPrefix(got, "<div></div") {
		t.Fatalf("expected raw-body fallback snippet, got %q", got)
	}

	if got := stripHTMLTags(""); got != "" {
		t.Fatalf("expected empty string passthrough, got %q", got)
	}
	if got := stripHTMLTags("<div>alpha</div><span>beta</span>"); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected html tags stripped with text kept, got %q", got)
	}
}
