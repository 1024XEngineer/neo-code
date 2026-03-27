package bash

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dust/neo-code/internal/tools"
)

type Tool struct {
	root        string
	shell       string
	timeout     time.Duration
	outputLimit int
}

type input struct {
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
}

func New(root string, shell string, timeout time.Duration) *Tool {
	return &Tool{
		root:        root,
		shell:       shell,
		timeout:     timeout,
		outputLimit: 16 * 1024,
	}
}

func (t *Tool) Name() string {
	return "bash"
}

func (t *Tool) Description() string {
	return "Execute a shell command inside the workspace with timeout and bounded output."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	if strings.TrimSpace(in.Command) == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New("bash: command is empty")
	}

	workdir, err := resolveWorkdir(call.Workdir, in.Workdir)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	args := t.shellArgs(in.Command)
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = workdir
	output, err := cmd.CombinedOutput()

	content := string(output)
	if len(content) > t.outputLimit {
		content = content[:t.outputLimit] + "\n...[truncated]"
	}
	result := tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		IsError: err != nil,
		Metadata: map[string]any{
			"workdir": workdir,
		},
	}
	if err != nil && strings.TrimSpace(result.Content) == "" {
		result.Content = err.Error()
	}
	return result, err
}

func (t *Tool) shellArgs(command string) []string {
	shell := strings.ToLower(strings.TrimSpace(t.shell))
	switch shell {
	case "powershell", "pwsh":
		return []string{"powershell", "-NoProfile", "-Command", command}
	case "bash":
		return []string{"bash", "-lc", command}
	case "sh":
		return []string{"sh", "-lc", command}
	}
	if runtime.GOOS == "windows" {
		return []string{"powershell", "-NoProfile", "-Command", command}
	}
	return []string{"sh", "-lc", command}
}

func resolveWorkdir(root string, requested string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := requested
	if strings.TrimSpace(target) == "" {
		target = base
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("bash: workdir escapes workspace root")
	}
	return target, nil
}
