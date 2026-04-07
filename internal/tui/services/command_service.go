package services

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// RunLocalCommandCmd 执行本地命令并将结果映射为 UI 消息。
func RunLocalCommandCmd(
	execute func(context.Context) (string, error),
	toMsg func(string, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		notice, err := execute(context.Background())
		return toMsg(notice, err)
	}
}

// RunWorkspaceCommandCmd 执行工作区命令并将命令/输出/错误映射为 UI 消息。
func RunWorkspaceCommandCmd(
	execute func(context.Context) (string, string, error),
	toMsg func(string, string, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		command, output, err := execute(context.Background())
		return toMsg(command, output, err)
	}
}
