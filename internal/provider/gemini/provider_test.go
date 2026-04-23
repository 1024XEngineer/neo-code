package gemini

import (
	"bytes"
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
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"filesystem_read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverGemini,
		BaseURL:        server.URL,
		DefaultModel:   "gemini-2.5-flash",
		APIKeyEnv:      "GEMINI_TEST_KEY",
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

	if capturedPath != "/models/gemini-2.5-flash:streamGenerateContent" {
		t.Fatalf("unexpected request path: %q", capturedPath)
	}
	if capturedAuth != "test-key" {
		t.Fatalf("expected x-goog-api-key header, got %q", capturedAuth)
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
			if payload.Usage == nil || payload.Usage.TotalTokens != 7 {
				t.Fatalf("expected usage total tokens 7, got %+v", payload.Usage)
			}
			if payload.FinishReason != "stop" {
				t.Fatalf("expected finish reason stop, got %q", payload.FinishReason)
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
		Driver:           provider.DriverGemini,
		BaseURL:          "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel:     "gemini-2.5-flash",
		APIKeyEnv:        "GEMINI_TEST_KEY",
		APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
		ChatEndpointPath: "/custom/models",
	})
	if err != nil {
		t.Fatalf("expected custom chat endpoint path to be accepted, got %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildRequestSupportsImageParts(t *testing.T) {
	t.Parallel()

	cfg := provider.RuntimeConfig{
		Driver:         provider.DriverGemini,
		BaseURL:        "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel:   "gemini-2.5-flash",
		APIKeyEnv:      "GEMINI_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	}
	model, contents, requestConfig, err := BuildRequest(context.Background(), cfg, providertypes.GenerateRequest{
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
	if model != "gemini-2.5-flash" {
		t.Fatalf("unexpected model: %q", model)
	}
	if requestConfig == nil {
		t.Fatal("expected request config")
	}
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %+v", contents)
	}
	firstParts := contents[0].Parts
	if len(firstParts) != 2 || firstParts[1].FileData == nil || firstParts[1].FileData.FileURI != "https://example.com/cat.png" {
		t.Fatalf("unexpected remote image mapping: %+v", firstParts)
	}
	secondParts := contents[1].Parts
	if len(secondParts) != 1 || secondParts[0].InlineData == nil || !bytes.HasPrefix(secondParts[0].InlineData.Data, []byte("image-")) {
		t.Fatalf("unexpected session_asset mapping: %+v", secondParts)
	}
}

func TestBuildRequestRejectsSessionAssetWithoutReader(t *testing.T) {
	t.Parallel()

	cfg := provider.RuntimeConfig{
		Driver:         provider.DriverGemini,
		BaseURL:        "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel:   "gemini-2.5-flash",
		APIKeyEnv:      "GEMINI_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	}
	_, _, _, err := BuildRequest(context.Background(), cfg, providertypes.GenerateRequest{
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
		Driver:         provider.DriverGemini,
		BaseURL:        "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel:   "gemini-2.5-flash",
		APIKeyEnv:      "GEMINI_TEST_KEY",
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
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[]}}]}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverGemini,
		BaseURL:        server.URL,
		DefaultModel:   "gemini-2.5-flash",
		APIKeyEnv:      "GEMINI_TEST_KEY",
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
