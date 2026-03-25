package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/bootstrap"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultConfigPath = "config.yaml"

var buildRunDeps = defaultRunDeps

type programRunner interface {
	Run() (tea.Model, error)
}

type runDeps struct {
	stdin                   io.Reader
	stdout                  io.Writer
	stderr                  io.Writer
	setUTF8Mode             func()
	prepareWorkspace        func(string) (string, error)
	ensureAPIKeyInteractive func(context.Context, *bufio.Scanner, string) (bool, error)
	loadAppConfig           func(string) error
	loadPersonaPrompt       func(string) (string, string, error)
	newProgram              func(string, int, string, string) (programRunner, error)
}

func defaultRunDeps(stdin io.Reader, stdout, stderr io.Writer) runDeps {
	return runDeps{
		stdin:                   stdin,
		stdout:                  stdout,
		stderr:                  stderr,
		setUTF8Mode:             setUTF8Mode,
		prepareWorkspace:        bootstrap.PrepareWorkspace,
		ensureAPIKeyInteractive: bootstrap.EnsureAPIKeyInteractive,
		loadAppConfig:           configs.LoadAppConfig,
		loadPersonaPrompt:       configs.LoadPersonaPrompt,
		newProgram: func(persona string, historyTurns int, configPath, workspaceRoot string) (programRunner, error) {
			return bootstrap.NewProgram(persona, historyTurns, configPath, workspaceRoot)
		},
	}
}

func main() {
	workspaceFlag, err := parseWorkspaceFlag(os.Args[1:], os.Stderr)
	if err != nil {
		os.Exit(1)
	}
	_ = loadDotEnv(".env")

	if err := run(workspaceFlag, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseWorkspaceFlag(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)

	workspaceFlag := fs.String("workspace", "", "指定工作区根目录")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return *workspaceFlag, nil
}

func run(workspaceFlag string, stdin io.Reader, stdout, stderr io.Writer) error {
	return runWithDeps(workspaceFlag, buildRunDeps(stdin, stdout, stderr))
}

func runWithDeps(workspaceFlag string, deps runDeps) error {
	if deps.setUTF8Mode != nil {
		deps.setUTF8Mode()
	}

	workspaceRoot, err := deps.prepareWorkspace(workspaceFlag)
	if err != nil {
		return fmt.Errorf("工作区初始化失败: %w", err)
	}

	scanner := bufio.NewScanner(deps.stdin)
	ready, err := deps.ensureAPIKeyInteractive(context.Background(), scanner, defaultConfigPath)
	if err != nil {
		return fmt.Errorf("初始化配置失败: %w", err)
	}
	if !ready {
		fmt.Fprintln(deps.stdout, "已退出 NeoCode")
		return nil
	}

	if err := deps.loadAppConfig(defaultConfigPath); err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	persona, personaPath, err := deps.loadPersonaPrompt(configs.GlobalAppConfig.Persona.FilePath)
	if err != nil {
		fmt.Fprintf(deps.stderr, "警告: 人设加载失败: %v\n", err)
	} else if personaPath != "" && strings.TrimSpace(configs.GlobalAppConfig.Persona.FilePath) != personaPath {
		fmt.Fprintf(deps.stderr, "提示: 人设已从 %s 回退加载\n", personaPath)
	}

	historyTurns := configs.GlobalAppConfig.History.ShortTermTurns
	p, err := deps.newProgram(persona, historyTurns, defaultConfigPath, workspaceRoot)
	if err != nil {
		return fmt.Errorf("初始化失败: %w", err)
	}
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("运行失败: %w", err)
	}

	return nil
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.Trim(value, `"'`)
		os.Setenv(key, value)
	}

	return nil
}

func loadPersonaPrompt(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}
