package runtime

import (
	"context"
	"strings"
	"testing"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
)

func TestCompactSummaryGeneratorBuildsProviderRequestWithoutTools(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		responses: []provider.ChatResponse{{
			Message: provider.Message{
				Role: provider.RoleAssistant,
				Content: strings.Join([]string{
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
				}, "\n"),
			},
		}},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	generator := newCompactSummaryGenerator(factory, resolvedProvider, "session-model")

	summary, err := generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		ArchivedMessages: []provider.Message{
			{Role: provider.RoleUser, Content: "legacy request"},
			{Role: provider.RoleAssistant, Content: "legacy answer"},
		},
		RetainedMessages: []provider.Message{
			{Role: provider.RoleAssistant, Content: "recent answer"},
		},
		RemovedSpans: 2,
		Config:       manager.Get().Context.Compact,
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
	if len(req.Messages) != 1 || req.Messages[0].Role != provider.RoleUser {
		t.Fatalf("expected a single user prompt, got %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Content, "<archived_source_material>") {
		t.Fatalf("expected archived material boundary, got %q", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "\"role\": \"user\"") {
		t.Fatalf("expected archived messages rendered as JSON, got %q", req.Messages[0].Content)
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
		responses: []provider.ChatResponse{{
			Message: provider.Message{
				Role: provider.RoleAssistant,
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
				},
			},
		}},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider, "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		ArchivedMessages: []provider.Message{
			{Role: provider.RoleUser, Content: "legacy request"},
		},
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain tool calls") {
		t.Fatalf("expected tool call rejection, got %v", err)
	}
}
