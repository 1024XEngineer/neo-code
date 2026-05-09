package runtime

import (
	"context"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

func TestServiceRunHonorsThinkingOverrideDisable(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}
	service := NewWithFactory(
		manager,
		tools.NewRegistry(),
		newMemoryStore(),
		&scriptedProviderFactory{provider: scripted},
		&stubContextBuilder{},
	)

	enabled := false
	err := service.Run(context.Background(), UserInput{
		RunID:            "run-thinking-override-disable",
		Parts:            []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		ThinkingOverride: &ThinkingOverride{Enabled: &enabled},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(scripted.requests) == 0 {
		t.Fatal("expected provider request")
	}
	if scripted.requests[0].ThinkingConfig == nil || scripted.requests[0].ThinkingConfig.Enabled {
		t.Fatalf("expected disabled thinking config, got %+v", scripted.requests[0].ThinkingConfig)
	}
}

func TestServiceRunDisableToolsSkipsToolSpecs(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "dummy_tool", content: "ok"})
	service := NewWithFactory(
		manager,
		registry,
		newMemoryStore(),
		&scriptedProviderFactory{provider: scripted},
		&stubContextBuilder{},
	)

	err := service.Run(context.Background(), UserInput{
		RunID:        "run-disable-tools",
		Parts:        []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		DisableTools: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(scripted.requests) == 0 {
		t.Fatal("expected provider request")
	}
	if len(scripted.requests[0].Tools) != 0 {
		t.Fatalf("expected no tools in request, got %+v", scripted.requests[0].Tools)
	}
}
