package runtime

import (
	"context"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

func TestServiceRunToolCallWithPartsInput(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{
				Name:        tools.ToolNameFilesystemReadFile,
				Description: "read file",
				Schema:      map[string]any{"type": "object"},
			},
		},
		result: tools.ToolResult{
			Name:    tools.ToolNameFilesystemReadFile,
			Content: "ok",
		},
	}

	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "tool-call-1",
							Name:      tools.ToolNameFilesystemReadFile,
							Arguments: `{"path":"README.md"}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(
		manager,
		toolManager,
		store,
		&scriptedProviderFactory{provider: providerImpl},
		&stubContextBuilder{},
	)

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-parts-tool",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("请记录一个待办并继续")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(providerImpl.requests) < 1 {
		t.Fatalf("expected provider requests, got 0")
	}
	toolFound := false
	for _, spec := range providerImpl.requests[0].Tools {
		if strings.EqualFold(strings.TrimSpace(spec.Name), tools.ToolNameFilesystemReadFile) {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Fatalf("expected first request tools to include %q", tools.ToolNameFilesystemReadFile)
	}

	session := onlySession(t, store)
	if len(session.Messages) == 0 {
		t.Fatalf("expected session messages, got 0")
	}
	userMsg := session.Messages[0]
	if userMsg.Role != providertypes.RoleUser {
		t.Fatalf("expected first message role user, got %q", userMsg.Role)
	}
	if got := providertypes.ExtractTextForProjection(userMsg.Parts); got != "请记录一个待办并继续" {
		t.Fatalf("expected first message parts text to persist, got %q", got)
	}

	if toolManager.executeCalls != 1 {
		t.Fatalf("expected 1 tool execute call, got %d", toolManager.executeCalls)
	}
	if toolManager.lastInput.Name != tools.ToolNameFilesystemReadFile {
		t.Fatalf("unexpected tool call name: %q", toolManager.lastInput.Name)
	}

	events := collectRuntimeEvents(service.Events())
	foundToolResult := false
	for _, event := range events {
		if event.Type == EventToolResult {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("expected %q event in runtime events", EventToolResult)
	}
}
