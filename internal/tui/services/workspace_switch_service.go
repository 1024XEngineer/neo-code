package services

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

// WorkspaceSwitcher 定义工作区进程切换所需的最小能力。
type WorkspaceSwitcher interface {
	SwitchWorkspace(ctx context.Context, workdir string) error
}

// WorkspaceSwitchRequest 描述一次工作区切换请求的静态参数。
type WorkspaceSwitchRequest struct {
	Notice     string
	Workdir    string
	Relaunched bool
}

// RunWorkspaceSwitchCmd 执行工作区切换，并将结果映射为 UI 消息。
func RunWorkspaceSwitchCmd(
	switcher WorkspaceSwitcher,
	request WorkspaceSwitchRequest,
	toMsg func(string, string, bool, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		if !request.Relaunched {
			return toMsg(request.Notice, request.Workdir, false, nil)
		}
		if switcher == nil {
			return toMsg(request.Notice, request.Workdir, true, errors.New("workspace switcher is nil"))
		}

		err := switcher.SwitchWorkspace(context.Background(), request.Workdir)
		return toMsg(request.Notice, request.Workdir, true, err)
	}
}
