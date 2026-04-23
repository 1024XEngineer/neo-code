package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestProviderGenerate(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"Hello\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"filesystem_read_file\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":6}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverAnthropic,
		BaseURL:        server.URL,
		DefaultModel:   "claude-3-7-sonnet",
		APIKeyEnv:      "ANTHROPIC_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 16)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if capturedPath != "/v1/messages" {
		t.Fatalf("unexpected request path: %q", capturedPath)
	}
	if capturedKey != "test-key" {
		t.Fatalf("expected x-api-key header, got %q", capturedKey)
	}

	drained := drainEvents(events)
	if len(drained) == 0 {
		t.Fatal("expected stream events")
	}

	var foundText, foundToolStart, foundToolDelta, foundDone bool
	for _, event := range drained {
		switch event.Type {
		case providertypes.StreamEventTextDelta:
			foundText = true
		case providertypes.StreamEventToolCallStart:
			foundToolStart = true
		case providertypes.StreamEventToolCallDelta:
			foundToolDelta = true
		case providertypes.StreamEventMessageDone:
			foundDone = true
			payload, payloadErr := event.MessageDoneValue()
			if payloadErr != nil {
				t.Fatalf("MessageDoneValue() error = %v", payloadErr)
			}
			if payload.FinishReason != "tool_use" {
				t.Fatalf("expected finish reason tool_use, got %q", payload.FinishReason)
			}
			if payload.Usage == nil || payload.Usage.TotalTokens != 10 {
				t.Fatalf("expected usage total tokens 10, got %+v", payload.Usage)
			}
		}
	}
	if !foundText || !foundToolStart || !foundToolDelta || !foundDone {
		t.Fatalf("expected text/tool_start/tool_delta/done events, got %+v", drained)
	}
}

func TestNewAcceptsCustomChatEndpointPath(t *testing.T) {
	t.Parallel()

	p, err := New(provider.RuntimeConfig{
		Driver:           provider.DriverAnthropic,
		BaseURL:          "https://api.anthropic.com/v1",
		DefaultModel:     "claude-3-7-sonnet",
		APIKeyEnv:        "ANTHROPIC_TEST_KEY",
		APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
		ChatEndpointPath: "/custom/messages",
	})
	if err != nil {
		t.Fatalf("expected custom chat endpoint path to be accepted, got %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildRequestRejectsToolResultWithoutToolCallID(t *testing.T) {
	t.Parallel()

	_, err := BuildRequest(context.Background(), provider.RuntimeConfig{DefaultModel: "claude", Driver: provider.DriverAnthropic}, providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleTool,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("result")},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "tool_call_id") {
		t.Fatalf("expected tool_call_id validation error, got %v", err)
	}
}

func TestBuildRequestSupportsImageParts(t *testing.T) {
	t.Parallel()

	params, err := BuildRequest(context.Background(), provider.RuntimeConfig{
		DefaultModel: "claude-3-7-sonnet",
		Driver:       provider.DriverAnthropic,
	}, providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("look"),
					providertypes.NewRemoteImagePart("https://example.com/cat.png"),
				},
			},
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
				},
			},
		},
		SessionAssetReader: &stubSessionAssetReader{
			assets: map[string]stubSessionAsset{
				"asset-1": {data: []byte("image-bytes"), mime: "image/png"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", params.Messages)
	}
	firstContent := params.Messages[0].Content
	if len(firstContent) != 2 ||
		firstContent[1].OfImage == nil ||
		firstContent[1].OfImage.Source.OfURL == nil ||
		firstContent[1].OfImage.Source.OfURL.URL != "https://example.com/cat.png" {
		t.Fatalf("unexpected remote image conversion: %+v", firstContent)
	}
	secondContent := params.Messages[1].Content
	if len(secondContent) != 1 || secondContent[0].OfImage == nil || secondContent[0].OfImage.Source.OfBase64 == nil ||
		!strings.HasPrefix(secondContent[0].OfImage.Source.OfBase64.Data, "aW1hZ2Ut") {
		t.Fatalf("unexpected session asset conversion: %+v", secondContent)
	}
}

func TestBuildRequestRejectsSessionAssetWithoutReader(t *testing.T) {
	t.Parallel()

	_, err := BuildRequest(context.Background(), provider.RuntimeConfig{
		DefaultModel: "claude-3-7-sonnet",
		Driver:       provider.DriverAnthropic,
	}, providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-1", "image/png")},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "session_asset reader is not configured") {
		t.Fatalf("expected missing session_asset reader error, got %v", err)
	}
}

func TestEstimateInputTokensReturnsAdvisoryLocalEstimate(t *testing.T) {
	t.Parallel()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverAnthropic,
		BaseURL:        "https://api.anthropic.com/v1",
		DefaultModel:   "claude-3-7-sonnet",
		APIKeyEnv:      "ANTHROPIC_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	estimate, err := p.EstimateInputTokens(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	})
	if err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}
	if estimate.EstimateSource != provider.EstimateSourceLocal {
		t.Fatalf("estimate source = %q, want %q", estimate.EstimateSource, provider.EstimateSourceLocal)
	}
	if estimate.GatePolicy != provider.EstimateGateAdvisory {
		t.Fatalf("gate policy = %q, want %q", estimate.GatePolicy, provider.EstimateGateAdvisory)
	}
	if estimate.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive estimate tokens, got %d", estimate.EstimatedInputTokens)
	}
}

func TestEstimateThenGenerateReusesPreparedRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverAnthropic,
		BaseURL:        server.URL,
		DefaultModel:   "claude-3-7-sonnet",
		APIKeyEnv:      "ANTHROPIC_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	reader := &stubSessionAssetReader{
		maxOpen: 1,
		assets: map[string]stubSessionAsset{
			"asset-1": {data: []byte("image-bytes"), mime: "image/png"},
		},
	}
	request := providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-1", "image/png")},
		}},
		SessionAssetReader: reader,
	}
	if _, err := p.EstimateInputTokens(context.Background(), request); err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), request, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if reader.openCount != 1 {
		t.Fatalf("expected session asset to be opened once, got %d", reader.openCount)
	}
}

func drainEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	var drained []providertypes.StreamEvent
	for {
		select {
		case event := <-events:
			drained = append(drained, event)
		default:
			return drained
		}
	}
}

type stubSessionAsset struct {
	data []byte
	mime string
	err  error
}

type stubSessionAssetReader struct {
	assets    map[string]stubSessionAsset
	openCount int
	maxOpen   int
}

func (r *stubSessionAssetReader) Open(_ context.Context, assetID string) (io.ReadCloser, string, error) {
	if r.maxOpen > 0 && r.openCount >= r.maxOpen {
		return nil, "", fmt.Errorf("open limit exceeded for asset: %s", assetID)
	}
	r.openCount++
	asset, ok := r.assets[assetID]
	if !ok {
		return nil, "", fmt.Errorf("asset not found: %s", assetID)
	}
	if asset.err != nil {
		return nil, "", asset.err
	}
	return io.NopCloser(strings.NewReader(string(asset.data))), asset.mime, nil
}
