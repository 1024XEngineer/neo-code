package runtime

import (
	"context"
	"testing"

	"neo-code/internal/tools"
)

type runnerDispatcherStub struct{}

func (runnerDispatcherStub) TryDispatch(context.Context, string, string, tools.ToolCallInput) (tools.ToolResult, bool, error) {
	return tools.ToolResult{}, false, nil
}

func TestServiceSetRunnerToolDispatcher(t *testing.T) {
	service := &Service{}
	dispatcher := runnerDispatcherStub{}
	service.SetRunnerToolDispatcher(dispatcher)
	if service.runnerToolDispatcher == nil {
		t.Fatal("runnerToolDispatcher = nil after SetRunnerToolDispatcher")
	}
}
