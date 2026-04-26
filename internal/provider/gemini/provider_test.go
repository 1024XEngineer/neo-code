package gemini

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

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
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: 1,
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
			if !payload.Usage.InputObserved || !payload.Usage.OutputObserved {
				t.Fatalf("expected usage observed flags true, got %+v", payload.Usage)
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

func TestProviderGenerateOmitsUsageWhenProviderDidNotReturnUsage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[{\"text\":\"done\"}]}}]}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: defaultGenerateRetryMax,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	drained := drainEvents(events)
	var done *providertypes.MessageDonePayload
	for i := range drained {
		if drained[i].Type != providertypes.StreamEventMessageDone {
			continue
		}
		payload, payloadErr := drained[i].MessageDoneValue()
		if payloadErr != nil {
			t.Fatalf("MessageDoneValue() error = %v", payloadErr)
		}
		done = &payload
		break
	}
	if done == nil {
		t.Fatalf("expected message_done event, got %+v", drained)
	}
	if done.Usage != nil {
		t.Fatalf("expected nil usage when provider does not report usage, got %+v", done.Usage)
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
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: 1,
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

func TestProviderGenerateRetriesRetryableErrorBeforeStreamStarts(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"retry ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[]}}]}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.retryBackoff = func(attempt int) time.Duration {
		_ = attempt
		return 0
	}
	p.retryWait = func(ctx context.Context, wait time.Duration) error {
		_ = ctx
		_ = wait
		return nil
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests after retry, got %d", requests)
	}
	if len(drainEvents(events)) == 0 {
		t.Fatal("expected stream events after retry recovery")
	}
}

func TestProviderGenerateReturnsRetryableErrorAfterRetryExhausted(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: defaultGenerateRetryMax,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.retryBackoff = func(attempt int) time.Duration {
		_ = attempt
		return 0
	}
	p.retryWait = func(ctx context.Context, wait time.Duration) error {
		_ = ctx
		_ = wait
		return nil
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected retryable error after exhausting retries")
	}
	if requests != defaultGenerateRetryMax+1 {
		t.Fatalf("expected %d requests, got %d", defaultGenerateRetryMax+1, requests)
	}

	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Retryable {
		t.Fatalf("expected retryable provider error, got %v", err)
	}
}

func TestProviderGenerateRetryStateResetsAfterSuccess(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[]}}]}\n\n")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.retryBackoff = func(attempt int) time.Duration {
		_ = attempt
		return 0
	}
	p.retryWait = func(ctx context.Context, wait time.Duration) error {
		_ = ctx
		_ = wait
		return nil
	}

	for i := 0; i < 2; i++ {
		events := make(chan providertypes.StreamEvent, 8)
		if err := p.Generate(context.Background(), providertypes.GenerateRequest{
			Messages: []providertypes.Message{{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
			}},
		}, events); err != nil {
			t.Fatalf("Generate() call %d error = %v", i+1, err)
		}
	}
	if requests != 3 {
		t.Fatalf("expected second request to start from a fresh retry window, got %d total requests", requests)
	}
}

func TestNormalizeGenerateErrorMapsNetworkTimeouts(t *testing.T) {
	t.Parallel()

	timeoutErr := timeoutNetError{message: "i/o timeout"}
	err := normalizeGenerateError(timeoutErr)
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCodeTimeout {
		t.Fatalf("expected timeout provider error, got %v", err)
	}

	err = normalizeGenerateError(net.UnknownNetworkError("dns failure"))
	if !errors.As(err, &providerErr) || providerErr.Code != provider.ErrorCodeNetwork {
		t.Fatalf("expected network provider error, got %v", err)
	}
}

func TestProviderGenerateReturnsRetryWaitError(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:             provider.DriverGemini,
		BaseURL:            server.URL,
		DefaultModel:       "gemini-2.5-flash",
		APIKeyEnv:          "GEMINI_TEST_KEY",
		APIKeyResolver:     provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sentinel := errors.New("retry wait failed")
	p.retryBackoff = func(attempt int) time.Duration {
		_ = attempt
		return 0
	}
	p.retryWait = func(ctx context.Context, wait time.Duration) error {
		_ = ctx
		_ = wait
		return sentinel
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected retry wait error, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one request before retry wait failure, got %d", requests)
	}
}

func TestProviderGenerateReturnsEmptyModelForPreparedRequest(t *testing.T) {
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

	req := providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}
	signature := provider.BuildGenerateRequestSignature(req)
	p.storePreparedRequest(signature, "   ", nil, nil)

	events := make(chan providertypes.StreamEvent, 4)
	err = p.Generate(context.Background(), req, events)
	if err == nil || !strings.Contains(err.Error(), "model is empty") {
		t.Fatalf("expected model empty error, got %v", err)
	}
}

func TestGenerateHelpers(t *testing.T) {
	t.Parallel()

	t.Run("normalize_model", func(t *testing.T) {
		t.Parallel()
		if got := normalizeGeminiModelName(" models/gemini-2.5-pro "); got != "gemini-2.5-pro" {
			t.Fatalf("normalizeGeminiModelName() = %q", got)
		}
		if got := normalizeGeminiModelName("  "); got != "" {
			t.Fatalf("normalizeGeminiModelName() = %q, want empty", got)
		}
	})

	t.Run("encode_arguments", func(t *testing.T) {
		t.Parallel()
		encoded, err := encodeArguments(nil)
		if err != nil || encoded != "{}" {
			t.Fatalf("encodeArguments(nil) = %q, %v", encoded, err)
		}
		_, err = encodeArguments(map[string]any{"bad": make(chan int)})
		if err == nil || !strings.Contains(err.Error(), "encode function args") {
			t.Fatalf("expected encode error, got %v", err)
		}
	})

	t.Run("timeout_error", func(t *testing.T) {
		t.Parallel()
		if !isTimeoutGenerateError(context.DeadlineExceeded) {
			t.Fatal("context deadline should be timeout")
		}
		if isTimeoutGenerateError(errors.New("plain")) {
			t.Fatal("plain error should not be timeout")
		}
	})
}

func TestMapGeminiSDKError(t *testing.T) {
	t.Parallel()

	if err := mapGeminiSDKError(errors.New("plain")); err != nil {
		t.Fatalf("plain error should not map, got %v", err)
	}

	cases := []struct {
		name       string
		err        error
		wantCode   provider.ProviderErrorCode
		wantSubstr string
	}{
		{
			name:       "status from name unauthenticated",
			err:        genai.APIError{Status: "UNAUTHENTICATED", Message: "bad token"},
			wantCode:   provider.ErrorCodeAuthFailed,
			wantSubstr: "bad token",
		},
		{
			name:       "bad request api key heuristic",
			err:        genai.APIError{Code: http.StatusBadRequest, Message: "x-goog-api-key invalid"},
			wantCode:   provider.ErrorCodeAuthFailed,
			wantSubstr: "x-goog-api-key invalid",
		},
		{
			name:       "bad request quota heuristic",
			err:        &genai.APIError{Code: http.StatusBadRequest, Message: "RESOURCE_EXHAUSTED quota"},
			wantCode:   provider.ErrorCodeRateLimit,
			wantSubstr: "RESOURCE_EXHAUSTED quota",
		},
		{
			name:       "permission denied",
			err:        genai.APIError{Status: "PERMISSION_DENIED", Message: "forbidden"},
			wantCode:   provider.ErrorCodeForbidden,
			wantSubstr: "forbidden",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := mapGeminiSDKError(tc.err)
			if err == nil {
				t.Fatal("expected mapped error")
			}
			var providerErr *provider.ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("expected provider error, got %T %v", err, err)
			}
			if providerErr.Code != tc.wantCode {
				t.Fatalf("provider code = %q, want %q", providerErr.Code, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("mapped error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
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

type timeoutNetError struct {
	message string
}

func (e timeoutNetError) Error() string {
	return e.message
}

func (e timeoutNetError) Timeout() bool {
	return true
}

func (e timeoutNetError) Temporary() bool {
	return true
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
