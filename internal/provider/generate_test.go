package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

type stubTextGenProvider struct {
	requests []providertypes.GenerateRequest
	generate func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error
}

func (s *stubTextGenProvider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	_ = ctx
	return providertypes.BudgetEstimate{
		EstimatedInputTokens: provider.EstimateTextTokens(req.SystemPrompt + renderEstimateMessages(req.Messages)),
		EstimateSource:       provider.EstimateSourceLocal,
		GatePolicy:           provider.EstimateGateGateable,
	}, nil
}

func (s *stubTextGenProvider) Generate(
	ctx context.Context,
	req providertypes.GenerateRequest,
	events chan<- providertypes.StreamEvent,
) error {
	s.requests = append(s.requests, req)
	if s.generate != nil {
		return s.generate(ctx, req, events)
	}
	return nil
}

func renderEstimateMessages(messages []providertypes.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(provider.RenderMessageText(message.Parts))
	}
	return builder.String()
}

func TestGenerateTextSuccess(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("hello ")
			events <- providertypes.NewTextDeltaStreamEvent("world")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	req := providertypes.GenerateRequest{
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("test message")}},
		},
	}

	text, err := provider.GenerateText(context.Background(), providerStub, req)
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q, want %q", text, "hello world")
	}
	if len(providerStub.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(providerStub.requests))
	}
}

func TestGenerateTextIgnoresThinkingDelta(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewThinkingDeltaStreamEvent("plan")
			events <- providertypes.NewTextDeltaStreamEvent("[")
			events <- providertypes.NewThinkingDeltaStreamEvent("more plan")
			events <- providertypes.NewTextDeltaStreamEvent("]")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	text, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if text != "[]" {
		t.Fatalf("text = %q, want %q", text, "[]")
	}
}

func TestGenerateTextProviderError(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			return errors.New("provider error")
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "provider error") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextPrefersDirectProviderErrorBeforeStreaming(t *testing.T) {
	providerErr := provider.NewProviderErrorFromStatus(400, "invalid_request_error")
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			return providerErr
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if strings.Contains(err.Error(), "message_done") {
		t.Fatalf("unexpected message_done wrapper: %v", err)
	}
}

func TestGenerateTextPrefersProviderErrorAfterPartialStream(t *testing.T) {
	providerErr := provider.NewProviderErrorFromStatus(500, "upstream failed")
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("partial")
			return providerErr
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if strings.Contains(err.Error(), "message_done") {
		t.Fatalf("unexpected message_done wrapper: %v", err)
	}
}

func TestGenerateTextReturnsEmptyTextWhenProviderErrorsAfterStreaming(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("partial")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return errors.New("provider error")
		},
	}

	text, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "provider error") {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty text on provider error", text)
	}
}

func TestGenerateTextRequiresMessageDone(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("partial")
			return nil
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "message_done") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextRejectsUnexpectedEvent(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: "unexpected"}
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "unexpected provider stream event") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextRejectsTextDeltaWithNilPayload(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: providertypes.StreamEventTextDelta}
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "text_delta event payload is nil") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextRejectsThinkingDeltaWithNilPayload(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: providertypes.StreamEventThinkingDelta}
			events <- providertypes.NewTextDeltaStreamEvent("partial")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "thinking_delta event payload is nil") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextRejectsMessageDoneWithNilPayload(t *testing.T) {
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("partial")
			events <- providertypes.StreamEvent{Type: providertypes.StreamEventMessageDone}
			return nil
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if err == nil || !strings.Contains(err.Error(), "message_done event payload is nil") {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateTextCombinesProviderAndStreamErrors(t *testing.T) {
	providerErr := errors.New("provider error")
	providerStub := &stubTextGenProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: "unexpected"}
			return providerErr
		},
	}

	_, err := provider.GenerateText(context.Background(), providerStub, providertypes.GenerateRequest{})
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected wrapped provider error, got %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected provider stream event") {
		t.Fatalf("expected stream error context to be preserved, got %v", err)
	}
}
