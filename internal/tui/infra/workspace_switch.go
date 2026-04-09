package infra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type executableResolver func() (string, error)
type processStarter func(processLaunchSpec) error

type processLaunchSpec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// ProcessWorkspaceSwitcher 负责以当前可执行文件重新拉起 NeoCode，并切换到新工作区。
type ProcessWorkspaceSwitcher struct {
	resolveExecutable executableResolver
	startProcess      processStarter
}

// NewProcessWorkspaceSwitcher 创建默认的进程级工作区切换器。
func NewProcessWorkspaceSwitcher() *ProcessWorkspaceSwitcher {
	return &ProcessWorkspaceSwitcher{
		resolveExecutable: os.Executable,
		startProcess:      startWorkspaceProcess,
	}
}

// SwitchWorkspace 拉起带 `--workdir` 参数的新进程，并在成功启动后返回。
func (s *ProcessWorkspaceSwitcher) SwitchWorkspace(ctx context.Context, workdir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	targetWorkdir := strings.TrimSpace(workdir)
	if targetWorkdir == "" {
		return fmt.Errorf("workspace switch: workdir is empty")
	}
	if s == nil {
		return fmt.Errorf("workspace switch: switcher is nil")
	}
	if s.resolveExecutable == nil {
		return fmt.Errorf("workspace switch: executable resolver is nil")
	}
	if s.startProcess == nil {
		return fmt.Errorf("workspace switch: process starter is nil")
	}

	executablePath, err := s.resolveExecutable()
	if err != nil {
		return fmt.Errorf("workspace switch: resolve executable: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	spec := processLaunchSpec{
		Path: executablePath,
		Args: []string{"--workdir", targetWorkdir},
		Dir:  targetWorkdir,
		Env:  os.Environ(),
	}
	if err := s.startProcess(spec); err != nil {
		return fmt.Errorf("workspace switch: start process: %w", err)
	}
	return nil
}

// startWorkspaceProcess 使用当前终端句柄启动新的 NeoCode 进程。
func startWorkspaceProcess(spec processLaunchSpec) error {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append([]string(nil), spec.Env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
