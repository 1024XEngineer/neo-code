package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	tuiservices "neo-code/internal/tui/services"
)

var webUIExecutablePath = os.Executable
var startWebUIProcess = defaultStartWebUIProcess

func (a *App) handleWebCommand(rest string) tea.Cmd {
	if strings.TrimSpace(rest) != "" {
		a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageWeb))
		return nil
	}

	workdir := strings.TrimSpace(a.state.CurrentWorkdir)
	if workdir == "" {
		workdir = strings.TrimSpace(a.configManager.Get().Workdir)
	}

	return tuiservices.RunLocalCommandCmd(
		func(_ context.Context) (string, error) {
			if err := startWebUIProcess(workdir); err != nil {
				return "", err
			}
			if workdir == "" {
				return "Web UI startup requested. Browser will open when server is ready.", nil
			}
			return fmt.Sprintf("Web UI startup requested for workdir: %s", workdir), nil
		},
		func(notice string, err error) tea.Msg {
			return localCommandResultMsg{Notice: notice, Err: err}
		},
	)
}

func defaultStartWebUIProcess(workdir string) error {
	executablePath, err := webUIExecutablePath()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	args := []string{"web", "--open-browser=true"}
	if strings.TrimSpace(workdir) != "" {
		args = append([]string{"--workdir", strings.TrimSpace(workdir)}, args...)
	}

	command := exec.Command(executablePath, args...)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("prepare web process output: %w", err)
	}
	defer devNull.Close()

	command.Stdout = devNull
	command.Stderr = devNull

	if err := command.Start(); err != nil {
		return fmt.Errorf("start web ui process: %w", err)
	}
	return nil
}
