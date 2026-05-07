package gateway

import (
	"context"
	"encoding/json"
	"strings"

	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/tools"
)

// runnerToolDispatcherBridge 适配 RunnerToolManager 为 runtime.RunnerToolDispatcher。
type runnerToolDispatcherBridge struct {
	manager *RunnerToolManager
}

// NewRunnerToolDispatcher 创建 runtime.RunnerToolDispatcher 的 gateway 端适配器。
func NewRunnerToolDispatcher(manager *RunnerToolManager) agentruntime.RunnerToolDispatcher {
	if manager == nil {
		return nil
	}
	return &runnerToolDispatcherBridge{manager: manager}
}

func (b *runnerToolDispatcherBridge) TryDispatch(
	ctx context.Context,
	sessionID string,
	runID string,
	input tools.ToolCallInput,
) (tools.ToolResult, bool, error) {
	content, isError, err := b.manager.DispatchToolRequest(
		ctx,
		strings.TrimSpace(sessionID),
		strings.TrimSpace(runID),
		strings.TrimSpace(input.ID),
		strings.TrimSpace(input.Name),
		json.RawMessage(input.Arguments),
	)
	if err != nil {
		if strings.Contains(err.Error(), "runner not online") {
			return tools.ToolResult{}, false, nil
		}
		return tools.ToolResult{Content: err.Error(), IsError: true}, true, nil
	}
	return tools.ToolResult{Content: content, IsError: isError}, true, nil
}
