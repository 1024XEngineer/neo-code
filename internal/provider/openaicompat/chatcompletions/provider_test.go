package chatcompletions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestNewAndBuildRequest(t *testing.T) {
	t.Parallel()

	t.Run("new validation", func(t *testing.T) {
		t.Parallel()

		if _, err := New(testCfg("", "gpt-4.1", "test-key"), &http.Client{}); err == nil || !strings.Contains(err.Error(), "base url is empty") {
			t.Fatalf("expected base url error, got %v", err)
		}
		if _, err := New(testCfg("https://api.example.com/v1", "gpt-4.1", ""), &http.Client{}); err == nil || !strings.Contains(err.Error(), "api key is empty") {
			t.Fatalf("expected api key error, got %v", err)
		}
		if _, err := New(testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), nil); err == nil || !strings.Contains(err.Error(), "client is nil") {
			t.Fatalf("expected nil client error, got %v", err)
		}
	})

	t.Run("build request variants", func(t *testing.T) {
		t.Parallel()

		if _, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "", "test-key"), providertypes.GenerateRequest{}); err == nil || !strings.Contains(err.Error(), "model is empty") {
			t.Fatalf("expected model error, got %v", err)
		}
		if _, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{
					Role:  providertypes.RoleUser,
					Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-without-reader", "image/png")},
				},
			},
		}); err == nil || !strings.Contains(err.Error(), "session_asset reader is not configured") {
			t.Fatalf("expected BuildRequest conversion error, got %v", err)
		}

		payload, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Model:        "gpt-5.4",
			SystemPrompt: "system",
			Messages: []providertypes.Message{
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
				{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call_1", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`}}},
				{Role: providertypes.RoleTool, ToolCallID: "call_1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
			},
			Tools: []providertypes.ToolSpec{{Name: "filesystem_edit", Description: "Edit file", Schema: map[string]any{"type": "object"}}},
		})
		if err != nil {
			t.Fatalf("BuildRequest() error = %v", err)
		}
		if payload.Model != "gpt-5.4" || !payload.Stream || payload.ToolChoice != "auto" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		if len(payload.Messages) != 4 || payload.Messages[0].Role != providertypes.RoleSystem {
			t.Fatalf("unexpected messages: %+v", payload.Messages)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "filesystem_edit" {
			t.Fatalf("unexpected tools: %+v", payload.Tools)
		}

		toolSchemaWithTopLevelCombinator := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string"},
			},
			"oneOf": []any{
				map[string]any{"required": []string{"action"}},
			},
		}
		sanitizedPayload, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			},
			Tools: []providertypes.ToolSpec{{
				Name:        "todo_write",
				Description: "write todos",
				Schema:      toolSchemaWithTopLevelCombinator,
			}},
		})
		if err != nil {
			t.Fatalf("BuildRequest() sanitize schema error = %v", err)
		}
		gotSchema := sanitizedPayload.Tools[0].Function.Parameters
		if gotSchema["type"] != "object" {
			t.Fatalf("expected sanitized schema type object, got %+v", gotSchema["type"])
		}
		if _, ok := gotSchema["oneOf"]; !ok {
			t.Fatalf("expected top-level oneOf to be preserved, got %+v", gotSchema)
		}
		if _, ok := toolSchemaWithTopLevelCombinator["oneOf"]; !ok {
			t.Fatalf("expected original schema not to be mutated")
		}

		downgradedPayload, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			},
			Tools: []providertypes.ToolSpec{{
				Name:        "non_object_schema",
				Description: "schema root is string",
				Schema: map[string]any{
					"type": "string",
				},
			}},
		})
		if err != nil {
			t.Fatalf("BuildRequest() downgrade schema error = %v", err)
		}
		downgradedSchema := downgradedPayload.Tools[0].Function.Parameters
		if downgradedSchema["type"] != "object" {
			t.Fatalf("expected downgraded schema type object, got %+v", downgradedSchema["type"])
		}
		if downgradedSchema["x-neocode-schema-downgraded"] != true {
			t.Fatalf("expected downgrade marker, got %+v", downgradedSchema)
		}

		withSessionAsset, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{
					Role: providertypes.RoleUser,
					Parts: []providertypes.ContentPart{
						providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
					},
				},
			},
			SessionAssetReader: stubSessionAssetReader{
				assets: map[string]stubSessionAsset{
					"asset-1": {data: []byte("img"), mime: "image/png"},
				},
			},
		})
		if err != nil {
			t.Fatalf("BuildRequest() session asset error = %v", err)
		}
		converted, ok := withSessionAsset.Messages[0].Content.([]MessageContentPart)
		if !ok || len(converted) != 1 || !strings.HasPrefix(converted[0].ImageURL.URL, "data:image/png;base64,") {
			t.Fatalf("unexpected session asset conversion: %+v", withSessionAsset.Messages[0].Content)
		}

		halfBudget := providertypes.MaxSessionAssetsTotalBytes / 2
		overBudgetSize := int(halfBudget + 1)
		if _, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			Messages: []providertypes.Message{
				{
					Role: providertypes.RoleUser,
					Parts: []providertypes.ContentPart{
						providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
						providertypes.NewSessionAssetImagePart("asset-2", "image/png"),
					},
				},
			},
			SessionAssetReader: stubSessionAssetReader{
				assets: map[string]stubSessionAsset{
					"asset-1": {data: make([]byte, overBudgetSize), mime: "image/png"},
					"asset-2": {data: make([]byte, overBudgetSize), mime: "image/png"},
				},
			},
		}); err == nil || !strings.Contains(err.Error(), "session_asset total exceeds") {
			t.Fatalf("expected session_asset total budget error, got %v", err)
		}

		fallback, err := BuildRequest(context.Background(), testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), providertypes.GenerateRequest{
			SystemPrompt: "   ",
			Messages:     []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
			Tools:        []providertypes.ToolSpec{},
		})
		if err != nil {
			t.Fatalf("BuildRequest() fallback error = %v", err)
		}
		if fallback.Model != "gpt-4.1" || len(fallback.Messages) != 1 || len(fallback.Tools) != 0 || fallback.ToolChoice != "" {
			t.Fatalf("unexpected fallback payload: %+v", fallback)
		}
	})

	t.Run("message conversion", func(t *testing.T) {
		t.Parallel()

		user, err := ToOpenAIMessage(
			context.Background(),
			providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			nil,
		)
		if err != nil {
			t.Fatalf("ToOpenAIMessage() user error = %v", err)
		}
		if user.Role != providertypes.RoleUser || user.Content != "hello" || len(user.ToolCalls) != 0 {
			t.Fatalf("unexpected user message: %+v", user)
		}

		multiText, err := ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewTextPart("hello "),
				providertypes.NewTextPart("world"),
			},
		}, nil)
		if err != nil {
			t.Fatalf("ToOpenAIMessage() multiText error = %v", err)
		}
		if multiText.Content != "hello world" {
			t.Fatalf("unexpected multiText content: %+v", multiText.Content)
		}

		assistant, err := ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
			},
		}, nil)
		if err != nil {
			t.Fatalf("ToOpenAIMessage() assistant error = %v", err)
		}
		if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Type != "function" || assistant.ToolCalls[0].Function.Name != "read_file" {
			t.Fatalf("unexpected assistant message: %+v", assistant)
		}

		tool, err := ToOpenAIMessage(
			context.Background(),
			providertypes.Message{Role: providertypes.RoleTool, ToolCallID: "call_1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("result")}},
			nil,
		)
		if err != nil {
			t.Fatalf("ToOpenAIMessage() tool error = %v", err)
		}
		if tool.Role != providertypes.RoleTool || tool.ToolCallID != "call_1" {
			t.Fatalf("unexpected tool message: %+v", tool)
		}

		multiModal, err := ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewTextPart("look"),
				providertypes.NewRemoteImagePart("https://example.com/img.png"),
			},
		}, nil)
		if err != nil {
			t.Fatalf("ToOpenAIMessage() multiModal error = %v", err)
		}
		parts, ok := multiModal.Content.([]MessageContentPart)
		if !ok || len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" || parts[1].ImageURL.URL != "https://example.com/img.png" {
			t.Fatalf("unexpected multiModal message: %+v", multiModal)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "session_asset reader is not configured") {
			t.Fatalf("expected missing reader error, got %v", err)
		}
		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("   ", "image/png"),
			},
		}, stubSessionAssetReader{})
		if err == nil || !strings.Contains(err.Error(), "missing asset id") {
			t.Fatalf("expected missing asset id error, got %v", err)
		}

		sessionAssetMsg, err := ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
			},
		}, stubSessionAssetReader{
			assets: map[string]stubSessionAsset{
				"asset-1": {
					data: []byte("image-bytes"),
					mime: "image/png",
				},
			},
		})
		if err != nil {
			t.Fatalf("ToOpenAIMessage() session asset error = %v", err)
		}
		sessionParts, ok := sessionAssetMsg.Content.([]MessageContentPart)
		if !ok || len(sessionParts) != 1 || sessionParts[0].Type != "image_url" ||
			!strings.HasPrefix(sessionParts[0].ImageURL.URL, "data:image/png;base64,") {
			t.Fatalf("unexpected session asset message: %+v", sessionAssetMsg)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-unsupported", "image/png"),
			},
		}, stubSessionAssetReader{
			assets: map[string]stubSessionAsset{
				"asset-unsupported": {
					data: []byte("data"),
					mime: "application/octet-stream",
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported mime type") {
			t.Fatalf("expected unsupported mime type error, got %v", err)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-missing-mime", ""),
			},
		}, stubSessionAssetReader{
			assets: map[string]stubSessionAsset{
				"asset-missing-mime": {
					data: []byte("data"),
					mime: "   ",
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "missing mime type") {
			t.Fatalf("expected missing mime type error, got %v", err)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-big", "image/png"),
			},
		}, stubSessionAssetReader{
			assets: map[string]stubSessionAsset{
				"asset-big": {
					data: make([]byte, providertypes.MaxSessionAssetBytes+1),
					mime: "image/png",
				},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected oversized session asset error, got %v", err)
		}

		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = ToOpenAIMessage(canceledCtx, providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewSessionAssetImagePart("asset-cancel", "image/png"),
			},
		}, stubSessionAssetReader{
			openFunc: func(ctx context.Context, assetID string) (io.ReadCloser, string, error) {
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatalf("expected canceled ctx in reader, asset=%s err=%v", assetID, ctx.Err())
				}
				return nil, "", ctx.Err()
			},
		})
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected context canceled error, got %v", err)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				{Kind: providertypes.ContentPartImage, Image: &providertypes.ImagePart{SourceType: "unknown"}},
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "unsupported source type") {
			t.Fatalf("expected unsupported image error, got %v", err)
		}

		_, err = ToOpenAIMessage(context.Background(), providertypes.Message{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewRemoteImagePart(""),
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid message parts") {
			t.Fatalf("expected invalid parts error, got %v", err)
		}
	})
}

func TestResolveSessionAssetDataURLVariants(t *testing.T) {
	t.Parallel()

	if _, _, err := resolveSessionAssetDataURL(context.Background(), stubSessionAssetReader{
		assets: map[string]stubSessionAsset{
			"asset-no-budget": {data: []byte("x"), mime: "image/png"},
		},
	}, &providertypes.AssetRef{ID: "asset-no-budget", MimeType: "image/png"}, 0); err == nil ||
		!strings.Contains(err.Error(), "session_asset total exceeds") {
		t.Fatalf("expected no budget error, got %v", err)
	}

	if _, _, err := resolveSessionAssetDataURL(nil, stubSessionAssetReader{
		assets: map[string]stubSessionAsset{
			"asset-nil-ctx": {data: []byte("x"), mime: "image/png"},
		},
	}, &providertypes.AssetRef{ID: "asset-nil-ctx", MimeType: "image/png"}, maxSessionAssetsTotalBytes); err != nil {
		t.Fatalf("expected nil context to fallback to background, got %v", err)
	}

	if _, _, err := resolveSessionAssetDataURL(context.Background(), stubSessionAssetReader{
		openFunc: func(ctx context.Context, assetID string) (io.ReadCloser, string, error) {
			_ = ctx
			_ = assetID
			return nil, "", errors.New("open failed")
		},
	}, &providertypes.AssetRef{ID: "asset-open-error", MimeType: "image/png"}, maxSessionAssetsTotalBytes); err == nil || !strings.Contains(err.Error(), "open session_asset") {
		t.Fatalf("expected wrapped open error, got %v", err)
	}

	if _, _, err := resolveSessionAssetDataURL(context.Background(), stubSessionAssetReader{
		openFunc: func(ctx context.Context, assetID string) (io.ReadCloser, string, error) {
			_ = ctx
			_ = assetID
			return &failingReadCloser{err: errors.New("read failed")}, "image/png", nil
		},
	}, &providertypes.AssetRef{ID: "asset-read-error", MimeType: "image/png"}, maxSessionAssetsTotalBytes); err == nil || !strings.Contains(err.Error(), "read session_asset") {
		t.Fatalf("expected wrapped read error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if _, _, err := resolveSessionAssetDataURL(ctx, stubSessionAssetReader{
		openFunc: func(openCtx context.Context, assetID string) (io.ReadCloser, string, error) {
			_ = openCtx
			_ = assetID
			return &cancelOnReadCloser{cancel: cancel}, "image/png", nil
		},
	}, &providertypes.AssetRef{ID: "asset-cancel-after-read", MimeType: "image/png"}, maxSessionAssetsTotalBytes); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected canceled context after read, got %v", err)
	}

	if _, _, err := resolveSessionAssetDataURL(context.Background(), stubSessionAssetReader{
		assets: map[string]stubSessionAsset{
			"asset-empty": {data: []byte{}, mime: "image/png"},
		},
	}, &providertypes.AssetRef{ID: "asset-empty", MimeType: "image/png"}, maxSessionAssetsTotalBytes); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty asset error, got %v", err)
	}

	if _, _, err := resolveSessionAssetDataURL(context.Background(), stubSessionAssetReader{
		assets: map[string]stubSessionAsset{
			"asset-over-total": {data: []byte("12345"), mime: "image/png"},
		},
	}, &providertypes.AssetRef{ID: "asset-over-total", MimeType: "image/png"}, 4); err == nil ||
		!strings.Contains(err.Error(), "session_asset total exceeds") {
		t.Fatalf("expected total budget error, got %v", err)
	}

	if _, _, err := toOpenAIMessageWithBudget(context.Background(), providertypes.Message{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewSessionAssetImagePart("asset-negative-budget", "image/png"),
		},
	}, stubSessionAssetReader{
		assets: map[string]stubSessionAsset{
			"asset-negative-budget": {data: []byte("x"), mime: "image/png"},
		},
	}, -1); err == nil || !strings.Contains(err.Error(), "session_asset total exceeds") {
		t.Fatalf("expected negative budget to fail as no budget, got %v", err)
	}
}

func TestParseErrorVariants(t *testing.T) {
	t.Parallel()

	if err := ParseError(&http.Response{
		Status:     "400 Bad Request",
		StatusCode: http.StatusBadRequest,
		Body:       &failingReadCloser{err: errors.New("broken body")},
	}); err == nil || !strings.Contains(err.Error(), "read error response") {
		t.Fatalf("expected body read error, got %v", err)
	}

	if err := ParseError(&http.Response{
		Status:     "401 Unauthorized",
		StatusCode: http.StatusUnauthorized,
		Body:       ioNopCloser(`{"error":{"message":"invalid api key"}}`),
	}); err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected json error payload, got %v", err)
	}

	if err := ParseError(&http.Response{
		Status:     "502 Bad Gateway",
		StatusCode: http.StatusBadGateway,
		Body:       ioNopCloser("gateway timeout"),
	}); err == nil || !strings.Contains(err.Error(), "gateway timeout") {
		t.Fatalf("expected plain text fallback, got %v", err)
	}

	if err := ParseError(&http.Response{
		Status:     "500 Internal Server Error",
		StatusCode: http.StatusInternalServerError,
		Body:       ioNopCloser("   "),
	}); err == nil || !strings.Contains(err.Error(), "500 Internal Server Error") {
		t.Fatalf("expected status fallback, got %v", err)
	}

	contextErr := ParseError(&http.Response{
		Status:     "400 Bad Request",
		StatusCode: http.StatusBadRequest,
		Body: ioNopCloser(
			`{"error":{"message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 140000 tokens."}}`,
		),
	})
	if contextErr == nil || !provider.IsContextTooLong(contextErr) {
		t.Fatalf("expected context-too-long classification, got %v", contextErr)
	}
}

func TestEmitFlushMergeAndUsage(t *testing.T) {
	t.Parallel()

	if err := EmitTextDelta(context.Background(), nil, "hello"); err != nil {
		t.Fatalf("nil channel guard failed: %v", err)
	}

	nilCtxEvents := make(chan providertypes.StreamEvent, 4)
	if err := EmitTextDelta(nil, nilCtxEvents, "nil-ctx-text"); err != nil {
		t.Fatalf("nil context text emit failed: %v", err)
	}
	if got := mustText(t, <-nilCtxEvents); got.Text != "nil-ctx-text" {
		t.Fatalf("unexpected nil context text payload: %+v", got)
	}
	if err := EmitToolCallStart(nil, nilCtxEvents, 0, "call_nil", "tool_nil"); err != nil {
		t.Fatalf("nil context start emit failed: %v", err)
	}
	if got := mustStart(t, <-nilCtxEvents); got.ID != "call_nil" || got.Name != "tool_nil" {
		t.Fatalf("unexpected nil context start payload: %+v", got)
	}
	if err := EmitToolCallDelta(nil, nilCtxEvents, 0, "call_nil", "{\"x\":1}"); err != nil {
		t.Fatalf("nil context delta emit failed: %v", err)
	}
	if got := mustDelta(t, <-nilCtxEvents); got.ID != "call_nil" || got.ArgumentsDelta != "{\"x\":1}" {
		t.Fatalf("unexpected nil context delta payload: %+v", got)
	}
	if err := EmitMessageDone(nil, nilCtxEvents, "stop", &providertypes.Usage{TotalTokens: 3}); err != nil {
		t.Fatalf("nil context done emit failed: %v", err)
	}
	if got := mustDone(t, <-nilCtxEvents); got.FinishReason != "stop" || got.Usage == nil || got.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected nil context done payload: %+v", got)
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := EmitTextDelta(context.Background(), events, ""); err != nil {
		t.Fatalf("empty text guard failed: %v", err)
	}
	select {
	case evt := <-events:
		t.Fatalf("expected no event for empty text, got %+v", evt)
	default:
	}

	if err := EmitTextDelta(context.Background(), events, "chunk"); err != nil {
		t.Fatalf("EmitTextDelta() error = %v", err)
	}
	if got := mustText(t, <-events); got.Text != "chunk" {
		t.Fatalf("unexpected text payload: %+v", got)
	}

	if err := EmitToolCallStart(context.Background(), events, 1, "call_1", "filesystem_edit"); err != nil {
		t.Fatalf("EmitToolCallStart() error = %v", err)
	}
	if got := mustStart(t, <-events); got.Index != 1 || got.ID != "call_1" || got.Name != "filesystem_edit" {
		t.Fatalf("unexpected start payload: %+v", got)
	}

	if err := EmitToolCallDelta(context.Background(), events, 1, "call_1", "{}"); err != nil {
		t.Fatalf("EmitToolCallDelta() error = %v", err)
	}
	if got := mustDelta(t, <-events); got.ID != "call_1" || got.ArgumentsDelta != "{}" {
		t.Fatalf("unexpected delta payload: %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EmitTextDelta(ctx, make(chan providertypes.StreamEvent), "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled text emit, got %v", err)
	}
	if err := EmitToolCallStart(ctx, make(chan providertypes.StreamEvent), 0, "call_1", "tool"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled start emit, got %v", err)
	}
	if err := EmitToolCallDelta(ctx, make(chan providertypes.StreamEvent), 0, "call_1", "{}"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled delta emit, got %v", err)
	}

	doneEvents := make(chan providertypes.StreamEvent, 1)
	if err := EmitMessageDone(ctx, doneEvents, "stop", &providertypes.Usage{TotalTokens: 10}); err != nil {
		t.Fatalf("expected canceled message_done fallback, got %v", err)
	}
	if got := mustDone(t, <-doneEvents); got.FinishReason != "stop" || got.Usage == nil || got.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected message_done payload: %+v", got)
	}
	if err := EmitMessageDone(ctx, make(chan providertypes.StreamEvent), "stop", &providertypes.Usage{}); err != nil {
		t.Fatalf("expected non-blocking drop on canceled context, got %v", err)
	}

	var flushed []string
	if err := FlushDataLines([]string{"a", "b"}, func(line string) error {
		flushed = append(flushed, line)
		return nil
	}); err != nil || strings.Join(flushed, ",") != "a,b" {
		t.Fatalf("unexpected flush result: lines=%v err=%v", flushed, err)
	}
	flushErr := errors.New("flush failed")
	if err := FlushDataLines([]string{"x"}, func(string) error { return flushErr }); !errors.Is(err, flushErr) {
		t.Fatalf("expected flush error, got %v", err)
	}

	toolCalls := map[int]*providertypes.ToolCall{}
	if err := MergeToolCallDelta(context.Background(), events, toolCalls, ToolCallDelta{Index: 0, ID: "call_0"}); err != nil {
		t.Fatalf("MergeToolCallDelta() id-only error = %v", err)
	}
	select {
	case evt := <-events:
		t.Fatalf("expected no event for id-only delta, got %+v", evt)
	default:
	}
	if err := MergeToolCallDelta(context.Background(), events, toolCalls, ToolCallDelta{
		Index:    0,
		Function: FunctionCall{Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() name error = %v", err)
	}
	if err := MergeToolCallDelta(context.Background(), events, toolCalls, ToolCallDelta{
		Index:    0,
		Function: FunctionCall{Arguments: `,"replace":"new"}`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() args error = %v", err)
	}
	if mustStart(t, <-events).Name != "filesystem_edit" {
		t.Fatal("expected tool_call_start after name arrival")
	}
	if mustDelta(t, <-events).ArgumentsDelta != `{"path":"main.go"}` {
		t.Fatal("expected first tool call delta")
	}
	if mustDelta(t, <-events).ArgumentsDelta != `,"replace":"new"}` {
		t.Fatal("expected second tool call delta")
	}
	if toolCalls[0].Arguments != `{"path":"main.go"},"replace":"new"}` {
		t.Fatalf("unexpected accumulated tool call: %+v", toolCalls[0])
	}

	if err := MergeToolCallDelta(ctx, make(chan providertypes.StreamEvent), map[int]*providertypes.ToolCall{}, ToolCallDelta{
		Index:    1,
		Function: FunctionCall{Name: "filesystem_edit"},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled merge, got %v", err)
	}

	var usage providertypes.Usage
	ExtractStreamUsage(&usage, nil)
	ExtractStreamUsage(&usage, &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	if usage.InputTokens != 10 || usage.OutputTokens != 5 || usage.TotalTokens != 15 {
		t.Fatalf("unexpected usage mapping: %+v", usage)
	}
}

func testCfg(baseURL, model, apiKey string) provider.RuntimeConfig {
	return provider.RuntimeConfig{
		Name:         "openaicompat",
		Driver:       "openaicompat",
		BaseURL:      baseURL,
		DefaultModel: model,
		APIKey:       apiKey,
	}
}

func userTextGenerateRequest(text string) providertypes.GenerateRequest {
	return providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(text)}},
		},
	}
}

func mustProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"), &http.Client{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
}

func mustText(t *testing.T, evt providertypes.StreamEvent) providertypes.TextDeltaPayload {
	t.Helper()
	payload, err := evt.TextDeltaValue()
	if err != nil {
		t.Fatalf("TextDeltaValue() error = %v", err)
	}
	return payload
}

func mustStart(t *testing.T, evt providertypes.StreamEvent) providertypes.ToolCallStartPayload {
	t.Helper()
	payload, err := evt.ToolCallStartValue()
	if err != nil {
		t.Fatalf("ToolCallStartValue() error = %v", err)
	}
	return payload
}

func mustDelta(t *testing.T, evt providertypes.StreamEvent) providertypes.ToolCallDeltaPayload {
	t.Helper()
	payload, err := evt.ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("ToolCallDeltaValue() error = %v", err)
	}
	return payload
}

func mustDone(t *testing.T, evt providertypes.StreamEvent) providertypes.MessageDonePayload {
	t.Helper()
	payload, err := evt.MessageDoneValue()
	if err != nil {
		t.Fatalf("MessageDoneValue() error = %v", err)
	}
	return payload
}

func ioNopCloser(body string) io.ReadCloser { return io.NopCloser(strings.NewReader(body)) }

type failingReadCloser struct{ err error }

func (f *failingReadCloser) Read(_ []byte) (int, error) { return 0, f.err }
func (f *failingReadCloser) Close() error               { return f.err }

func TestConsumeStreamVariants(t *testing.T) {
	t.Parallel()

	t.Run("comment and done", func(t *testing.T) {
		t.Parallel()

		events := make(chan providertypes.StreamEvent, 4)
		err := mustProvider(t).ConsumeStream(context.Background(), strings.NewReader(
			": heartbeat\n"+
				"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n"+
				"data: [DONE]\n\n",
		), events)
		if err != nil {
			t.Fatalf("ConsumeStream() error = %v", err)
		}
		drained := drain(events)
		if len(drained) != 2 || drained[0].Type != providertypes.StreamEventTextDelta || drained[1].Type != providertypes.StreamEventMessageDone {
			t.Fatalf("unexpected events: %+v", drained)
		}
	})

	t.Run("payload and decode errors", func(t *testing.T) {
		t.Parallel()

		if err := mustProvider(t).ConsumeStream(context.Background(), strings.NewReader("data: {\"error\":{\"message\":\"rate limit exceeded\"}}\n"), make(chan providertypes.StreamEvent, 1)); err == nil || !strings.Contains(err.Error(), "rate limit exceeded") {
			t.Fatalf("expected payload error, got %v", err)
		}
		if err := mustProvider(t).ConsumeStream(context.Background(), strings.NewReader("data: {not-json}\n\n"), make(chan providertypes.StreamEvent, 1)); err == nil || !strings.Contains(err.Error(), "decode stream chunk") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})

	t.Run("eof without done flushes and interrupts", func(t *testing.T) {
		t.Parallel()

		events := make(chan providertypes.StreamEvent, 4)
		err := mustProvider(t).ConsumeStream(context.Background(), strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n"), events)
		if !errors.Is(err, provider.ErrStreamInterrupted) {
			t.Fatalf("expected ErrStreamInterrupted, got %v", err)
		}
		drained := drain(events)
		if len(drained) == 0 || drained[0].Type != providertypes.StreamEventTextDelta {
			t.Fatalf("expected pending text delta, got %+v", drained)
		}
	})

	t.Run("cancellation beats interruption", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := mustProvider(t).ConsumeStream(ctx, strings.NewReader("data: {}\n\n"), make(chan providertypes.StreamEvent, 1)); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

		ctx2, cancel2 := context.WithCancel(context.Background())
		err := mustProvider(t).ConsumeStream(ctx2, &cancelThenErrorReader{cancel: cancel2, err: io.ErrClosedPipe}, make(chan providertypes.StreamEvent, 1))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected read cancellation, got %v", err)
		}
	})

	t.Run("done then read error still finishes", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		events := make(chan providertypes.StreamEvent, 2)
		err := mustProvider(t).ConsumeStream(ctx, &cancelAfterDoneReader{
			payload: []byte("data: [DONE]\n"),
			cancel:  cancel,
			err:     io.ErrClosedPipe,
		}, events)
		if err != nil {
			t.Fatalf("expected completed stream, got %v", err)
		}
		drained := drain(events)
		if len(drained) != 1 || drained[0].Type != providertypes.StreamEventMessageDone {
			t.Fatalf("unexpected completion events: %+v", drained)
		}
	})

	t.Run("flush pending data on non eof error", func(t *testing.T) {
		t.Parallel()

		events := make(chan providertypes.StreamEvent, 4)
		body := io.MultiReader(
			strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n"),
			&errReader{err: io.ErrClosedPipe},
		)
		err := mustProvider(t).ConsumeStream(context.Background(), body, events)
		if !errors.Is(err, provider.ErrStreamInterrupted) {
			t.Fatalf("expected interrupted error, got %v", err)
		}
		drained := drain(events)
		if len(drained) == 0 || drained[0].Type != providertypes.StreamEventTextDelta {
			t.Fatalf("expected flushed text delta, got %+v", drained)
		}
	})

	t.Run("tool call stream records usage", func(t *testing.T) {
		t.Parallel()

		events := make(chan providertypes.StreamEvent, 8)
		err := mustProvider(t).ConsumeStream(context.Background(), strings.NewReader(
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello \"}}]}\n"+
				"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_edit\",\"arguments\":\"{\\\"path\\\":\\\"main.go\\\"}\"}}]}}]}\n"+
				"data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n"+
				"data: [DONE]\n\n",
		), events)
		if err != nil {
			t.Fatalf("ConsumeStream() error = %v", err)
		}
		drained := drain(events)
		if len(drained) != 4 || drained[0].Type != providertypes.StreamEventTextDelta || drained[1].Type != providertypes.StreamEventToolCallStart || drained[2].Type != providertypes.StreamEventToolCallDelta {
			t.Fatalf("unexpected stream events: %+v", drained)
		}
		done := mustDone(t, drained[3])
		if done.FinishReason != "tool_calls" || done.Usage == nil || done.Usage.TotalTokens != 15 {
			t.Fatalf("unexpected done payload: %+v", done)
		}
	})
}

func TestGenerateVariants(t *testing.T) {
	t.Parallel()

	t.Run("build and marshal errors", func(t *testing.T) {
		t.Parallel()

		emptyModel := &Provider{cfg: testCfg("https://api.example.com/v1", "", "test-key"), client: &http.Client{}}
		if err := emptyModel.Generate(context.Background(), providertypes.GenerateRequest{}, nil); err == nil || !strings.Contains(err.Error(), "model is empty") {
			t.Fatalf("expected model error, got %v", err)
		}

		if err := mustProvider(t).Generate(context.Background(), providertypes.GenerateRequest{
			Messages: userTextGenerateRequest("hello").Messages,
			Tools:    []providertypes.ToolSpec{{Name: "bad", Schema: map[string]any{"bad": func() {}}}},
		}, nil); err == nil || !strings.Contains(err.Error(), "marshal request") {
			t.Fatalf("expected marshal error, got %v", err)
		}
	})

	t.Run("request build and send errors", func(t *testing.T) {
		t.Parallel()

		invalidURL := &Provider{cfg: testCfg("://bad", "gpt-4.1", "test-key"), client: &http.Client{}}
		if err := invalidURL.Generate(context.Background(), userTextGenerateRequest("hello"), nil); err == nil || !strings.Contains(err.Error(), "build request") {
			t.Fatalf("expected build request error, got %v", err)
		}

		sendErr := &Provider{
			cfg: testCfg("https://api.example.com/v1", "gpt-4.1", "test-key"),
			client: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return nil, io.ErrClosedPipe
			})},
		}
		if err := sendErr.Generate(context.Background(), userTextGenerateRequest("hello"), nil); err == nil || !strings.Contains(err.Error(), "send request") {
			t.Fatalf("expected send request error, got %v", err)
		}
	})

	t.Run("http error response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		}))
		defer server.Close()

		p, err := New(testCfg(server.URL, "gpt-4.1", "test-key"), server.Client())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := p.Generate(context.Background(), userTextGenerateRequest("hello"), nil); err == nil || !strings.Contains(err.Error(), "invalid api key") {
			t.Fatalf("expected parsed provider error, got %v", err)
		}
	})

	t.Run("does not retry without tools on 400", func(t *testing.T) {
		t.Parallel()

		var reqCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCount++
			var payload Request
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if reqCount == 1 {
				if len(payload.Tools) == 0 {
					t.Fatalf("first request should include tools")
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"schema rejected"}}`))
				return
			}
			t.Fatalf("unexpected retry request without explicit retry policy")
		}))
		defer server.Close()

		p, err := New(testCfg(server.URL, "gpt-4.1", "test-key"), server.Client())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		events := make(chan providertypes.StreamEvent, 4)
		err = p.Generate(context.Background(), providertypes.GenerateRequest{
			Messages: []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
			Tools:    []providertypes.ToolSpec{{Name: "read_file", Description: "read", Schema: map[string]any{"type": "object"}}},
		}, events)
		if err == nil || !strings.Contains(err.Error(), "schema rejected") {
			t.Fatalf("Generate() expected original 400 error, got %v", err)
		}
		if reqCount != 1 {
			t.Fatalf("expected one request without tool-stripping retry, got %d", reqCount)
		}
		if drained := drain(events); len(drained) != 0 {
			t.Fatalf("expected no stream events after 400, got %+v", drained)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Accept") != "text/event-stream" {
				t.Fatalf("unexpected headers: auth=%q accept=%q", r.Header.Get("Authorization"), r.Header.Get("Accept"))
			}

			var payload Request
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload.Model != "gpt-5.4" || !payload.Stream {
				t.Fatalf("unexpected payload: %+v", payload)
			}

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(
				"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n" +
					"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n" +
					"data: [DONE]\n\n",
			))
		}))
		defer server.Close()

		p, err := New(testCfg(server.URL+"/", "gpt-4.1", "test-key"), server.Client())
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		events := make(chan providertypes.StreamEvent, 4)
		err = p.Generate(context.Background(), providertypes.GenerateRequest{
			Model:    "gpt-5.4",
			Messages: []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		}, events)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		drained := drain(events)
		if len(drained) != 2 || drained[0].Type != providertypes.StreamEventTextDelta {
			t.Fatalf("unexpected generate events: %+v", drained)
		}
		done := mustDone(t, drained[1])
		if done.FinishReason != "stop" || done.Usage == nil || done.Usage.TotalTokens != 2 {
			t.Fatalf("unexpected done payload: %+v", done)
		}
	})
}

func drain(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	var drained []providertypes.StreamEvent
	for {
		select {
		case evt := <-events:
			drained = append(drained, evt)
		default:
			return drained
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

type cancelThenErrorReader struct {
	cancel func()
	err    error
}

func (r *cancelThenErrorReader) Read(_ []byte) (int, error) {
	r.cancel()
	return 0, r.err
}

type cancelAfterDoneReader struct {
	payload []byte
	cancel  func()
	err     error
	read    bool
}

type cancelOnReadCloser struct {
	cancel func()
	read   bool
}

func (r *cancelOnReadCloser) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	copy(p, []byte("img"))
	r.cancel()
	return len("img"), nil
}

func (r *cancelOnReadCloser) Close() error { return nil }

func (r *cancelAfterDoneReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, r.payload), nil
	}
	r.cancel()
	return 0, r.err
}

type stubSessionAsset struct {
	data []byte
	mime string
	err  error
}

type stubSessionAssetReader struct {
	assets   map[string]stubSessionAsset
	openFunc func(ctx context.Context, assetID string) (io.ReadCloser, string, error)
}

func (r stubSessionAssetReader) Open(ctx context.Context, assetID string) (io.ReadCloser, string, error) {
	if r.openFunc != nil {
		return r.openFunc(ctx, assetID)
	}
	_ = ctx
	asset, ok := r.assets[assetID]
	if !ok {
		return nil, "", errors.New("asset not found")
	}
	if asset.err != nil {
		return nil, "", asset.err
	}
	return io.NopCloser(strings.NewReader(string(asset.data))), asset.mime, nil
}
