package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestCompactSummaryGeneratorBuildsProviderRequestWithoutTools(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(strings.Join([]string{
				"[compact_summary]",
				"done:",
				"- Completed the historical task and kept the final result.",
				"",
				"in_progress:",
				"- Continue from the retained recent window.",
				"",
				"decisions:",
				"- Keep the existing section layout for compatibility.",
				"",
				"code_changes:",
				"- Updated compact summary generation behavior.",
				"",
				"constraints:",
				"- Preserve only the minimum information needed to continue the work.",
			}, "\n"))},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	generator := newCompactSummaryGenerator(factory, resolvedProvider.ToRuntimeConfig(), "session-model")

	summary, err := generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		ArchivedMessages: []providertypes.Message{
			{Role: providertypes.RoleUser, Content: "legacy request"},
			{
				Role: providertypes.RoleAssistant,
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
				},
			},
		},
		RetainedMessages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Content: "recent answer"},
		},
		ArchivedMessageCount: 2,
		Config:               manager.Get().Context.Compact,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(summary, "[compact_summary]") {
		t.Fatalf("expected compact summary marker, got %q", summary)
	}
	if factory.calls != 1 || scripted.callCount != 1 {
		t.Fatalf("expected one provider call, got factory=%d provider=%d", factory.calls, scripted.callCount)
	}
	if len(factory.configs) != 1 || factory.configs[0].Name != config.OpenAIName {
		t.Fatalf("expected openai provider config, got %+v", factory.configs)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected exactly one recorded request, got %d", len(scripted.requests))
	}
	req := scripted.requests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("expected compact summary request without tools, got %+v", req.Tools)
	}
	if req.Model != "session-model" {
		t.Fatalf("expected request model session-model, got %q", req.Model)
	}
	if !strings.Contains(req.SystemPrompt, "[compact_summary]") {
		t.Fatalf("expected compact system prompt, got %q", req.SystemPrompt)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != providertypes.RoleUser {
		t.Fatalf("expected a single user prompt, got %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Content, "<archived_source_material>") {
		t.Fatalf("expected archived material boundary, got %q", req.Messages[0].Content)
	}
	if strings.Contains(req.Messages[0].Content, "\"role\": \"user\"") {
		t.Fatalf("expected transcript-style compact prompt instead of pretty JSON, got %q", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "[message 0] role=user") {
		t.Fatalf("expected transcript-style user message header, got %q", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "tool_call id=call-1 name=filesystem_read_file") {
		t.Fatalf("expected tool call metadata in compact prompt, got %q", req.Messages[0].Content)
	}
}

func TestCompactSummaryGeneratorRejectsToolCalls(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_read_file"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-1", "{}"),
			},
		},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		ArchivedMessages: []providertypes.Message{
			{Role: providertypes.RoleUser, Content: "legacy request"},
		},
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain tool calls") {
		t.Fatalf("expected tool call rejection, got %v", err)
	}
}

func TestCompactSummaryGeneratorRejectsMalformedStreamEvent(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				{Type: providertypes.StreamEventTextDelta},
			},
		},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode:   contextcompact.ModeManual,
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "text_delta event payload is nil") {
		t.Fatalf("expected malformed stream event rejection, got %v", err)
	}
}

func TestCompactSummaryGeneratorMalformedStreamEventDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	stream := []providertypes.StreamEvent{{Type: providertypes.StreamEventTextDelta}}
	for i := 0; i < 40; i++ {
		stream = append(stream, providertypes.NewTextDeltaStreamEvent("ignored"))
	}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{stream},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	errCh := make(chan error, 1)
	go func() {
		_, genErr := generator.Generate(context.Background(), contextcompact.SummaryInput{
			Mode:   contextcompact.ModeManual,
			Config: manager.Get().Context.Compact,
		})
		errCh <- genErr
	}()

	select {
	case genErr := <-errCh:
		if genErr == nil || !strings.Contains(genErr.Error(), "text_delta event payload is nil") {
			t.Fatalf("expected malformed stream event rejection, got %v", genErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected compact generation to fail instead of deadlocking on malformed stream event")
	}
}

func TestCompactSummaryGeneratorRejectsDriverWithoutStreaming(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("should not run")},
		},
	}
	factory := &scriptedProviderFactory{
		provider: scripted,
		capabilities: provider.DriverCapabilities{
			Streaming:     false,
			ToolTransport: true,
		},
	}
	generator := newCompactSummaryGenerator(factory, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode:   contextcompact.ModeManual,
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "does not support streaming") {
		t.Fatalf("expected streaming capability rejection, got %v", err)
	}
	if factory.calls != 0 {
		t.Fatalf("expected provider build to be skipped, got %d", factory.calls)
	}
	if scripted.callCount != 0 {
		t.Fatalf("expected provider Generate() to be skipped, got %d", scripted.callCount)
	}
}

func TestCompactSummaryGeneratorPropagatesDriverNotFoundFromCapabilities(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	factory := &scriptedProviderFactory{
		provider:        &scriptedProvider{},
		capabilitiesErr: provider.ErrDriverNotFound,
	}
	generator := newCompactSummaryGenerator(factory, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode:   contextcompact.ModeManual,
		Config: manager.Get().Context.Compact,
	})
	if !errors.Is(err, provider.ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got %v", err)
	}
	if factory.calls != 0 {
		t.Fatalf("expected provider build to be skipped, got %d", factory.calls)
	}
}
