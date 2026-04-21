package services

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tools"
)

const permissionResolveTimeout = 10 * time.Second

// Runner 定义执行 run 所需的最小能力。
type Runner interface {
	Run(ctx context.Context, input UserInput) error
}

// Submitter 定义单入口提交所需能力。
type Submitter interface {
	Submit(ctx context.Context, input PrepareInput) error
}

// Compactor 定义执行 compact 所需能力。
type Compactor interface {
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
}

// SystemToolRunner 定义执行系统工具能力。
type SystemToolRunner interface {
	ExecuteSystemTool(ctx context.Context, input SystemToolInput) (tools.ToolResult, error)
}

// PermissionResolver 定义提交权限决策能力。
type PermissionResolver interface {
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
}

// ListenForRuntimeEventCmd 监听事件通道并映射为 UI 消息。
func ListenForRuntimeEventCmd(sub <-chan RuntimeEvent, eventMsg func(RuntimeEvent) tea.Msg, closedMsg func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-sub
		if !ok {
			return closedMsg()
		}
		return eventMsg(event)
	}
}

// RunAgentCmd 执行 run 并回传结果。
func RunAgentCmd(runtime Runner, input UserInput, doneMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		err := runtime.Run(context.Background(), input)
		return doneMsg(err)
	}
}

// RunSubmitCmd 执行 submit 并回传结果。
func RunSubmitCmd(runtime Submitter, input PrepareInput, doneMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		err := runtime.Submit(context.Background(), input)
		return doneMsg(err)
	}
}

// RunCompactCmd 执行 compact 并回传结果。
func RunCompactCmd(runtime Compactor, input CompactInput, doneMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		_, err := runtime.Compact(context.Background(), input)
		return doneMsg(err)
	}
}

// RunSystemToolCmd 执行系统工具并回传结果。
func RunSystemToolCmd(runtime SystemToolRunner, input SystemToolInput, doneMsg func(tools.ToolResult, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		result, err := runtime.ExecuteSystemTool(context.Background(), input)
		return doneMsg(result, err)
	}
}

// RunResolvePermissionCmd 提交权限决策并回传结果。
func RunResolvePermissionCmd(
	runtime PermissionResolver,
	input PermissionResolutionInput,
	doneMsg func(PermissionResolutionInput, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), permissionResolveTimeout)
		defer cancel()

		err := runtime.ResolvePermission(ctx, input)
		return doneMsg(input, err)
	}
}
