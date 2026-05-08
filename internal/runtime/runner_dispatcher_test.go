package runtime

import (
	"context"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

type runnerDispatcherStub struct {
	result  tools.ToolResult
	handled bool
	err     error
}

func (s runnerDispatcherStub) TryDispatch(context.Context, string, string, tools.ToolCallInput) (tools.ToolResult, bool, error) {
	return s.result, s.handled, s.err
}

func TestServiceSetRunnerToolDispatcher(t *testing.T) {
	service := &Service{}
	dispatcher := runnerDispatcherStub{}
	service.SetRunnerToolDispatcher(dispatcher)
	if service.runnerToolDispatcher == nil {
		t.Fatal("runnerToolDispatcher = nil after SetRunnerToolDispatcher")
	}
}

func TestExecuteToolCallWithPermissionUsesRunnerDispatcherWhenHandled(t *testing.T) {
	service := NewWithFactory(newRuntimeConfigManager(t), &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.SetRunnerToolDispatcher(runnerDispatcherStub{
		result:  tools.ToolResult{Name: "bash", Content: "runner-ok"},
		handled: true,
	})

	result, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{
		RunID:       "run-1",
		SessionID:   "session-1",
		Call:        providertypes.ToolCall{ID: "call-1", Name: "bash", Arguments: `{"command":"pwd"}`},
		ToolTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("executeToolCallWithPermission() error = %v", err)
	}
	if result.Content != "runner-ok" {
		t.Fatalf("result = %+v", result)
	}
}
