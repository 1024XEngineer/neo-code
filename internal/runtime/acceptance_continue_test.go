package runtime

import (
	"context"
	"testing"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/tools"
)

func TestRunAcceptGateHookBlocksBeforeSystemPrecheckAndContinues(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{{
						ID:        "call-write",
						Name:      "filesystem_write_file",
						Arguments: `{"path":"app.go","content":"package main"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("first finish")},
				},
				FinishReason: "stop",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("fixed finish")},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		result: tools.ToolResult{
			Name:    "filesystem_write_file",
			Content: "ok",
			Facts:   tools.ToolExecutionFacts{WorkspaceWrite: true},
			Metadata: map[string]any{
				"path":          "app.go",
				"tool_diff":     "+package main",
				"tool_diff_new": true,
			},
		},
	}
	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})

	blockCount := 0
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "accept-gate-review",
		Point: runtimehooks.HookPointAcceptGate,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			blockCount++
			if blockCount == 1 {
				return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "tests failed"}
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register accept_gate hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-accept-gate-continue",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("update code")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if scripted.callCount != 3 {
		t.Fatalf("provider call count = %d, want 3", scripted.callCount)
	}
	if blockCount != 2 {
		t.Fatalf("accept_gate hook count = %d, want 2", blockCount)
	}

	events := collectRuntimeEvents(service.Events())
	blocked := eventIndex(events, EventHookBlocked)
	accepted := lastEventIndex(events, EventAcceptanceDecided)
	if blocked < 0 || accepted < 0 || blocked > accepted {
		t.Fatalf("expected hook block before final acceptance decision, events=%v", eventTypes(events))
	}
}

func TestRunEmptyOutputContinuesUntilExhausted(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		estimateFn: func(context.Context, providertypes.GenerateRequest) (providertypes.BudgetEstimate, error) {
			return providertypes.BudgetEstimate{EstimatedInputTokens: 1, EstimateSource: provider.EstimateSourceLocal}, nil
		},
		responses: []scriptedResponse{
			{Message: providertypes.Message{Role: providertypes.RoleAssistant}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant}, FinishReason: "stop"},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-empty-output-continue",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue until exhausted")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if scripted.callCount != 4 {
		t.Fatalf("provider call count = %d, want 4", scripted.callCount)
	}
	events := collectRuntimeEvents(service.Events())
	verificationFailed := lastEventOfType(events, EventVerificationFailed)
	payload, ok := verificationFailed.Payload.(VerificationFailedPayload)
	if !ok || payload.StopReason != controlplane.StopReasonAcceptContinueExhausted {
		t.Fatalf("verification failed payload = %+v, want accept_continue_exhausted", verificationFailed.Payload)
	}
}

func eventTypes(events []RuntimeEvent) []EventType {
	out := make([]EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func lastEventIndex(events []RuntimeEvent, eventType EventType) int {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == eventType {
			return index
		}
	}
	return -1
}

func lastEventOfType(events []RuntimeEvent, eventType EventType) RuntimeEvent {
	index := lastEventIndex(events, eventType)
	if index < 0 {
		return RuntimeEvent{}
	}
	return events[index]
}
