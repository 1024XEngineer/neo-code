package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

type RemoveDirTool struct {
	root string
}

type removeDirInput struct {
	Path  string `json:"path"`
	Force bool   `json:"force,omitempty"`
}

func NewRemoveDir(root string) *RemoveDirTool {
	return &RemoveDirTool{root: root}
}

func (t *RemoveDirTool) Name() string {
	return removeDirToolName
}

func (t *RemoveDirTool) Description() string {
	return "Remove a directory inside the workspace. By default only empty directories are removed; pass force=true to remove recursively."
}

func (t *RemoveDirTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path relative to workspace root, or absolute inside the workspace.",
			},
			"force": map[string]any{
				"type":        "boolean",
				"description": "When true, remove directory and all contents recursively. Defaults to false (empty directory only).",
			},
		},
		"required": []string{"path"},
	}
}

func (t *RemoveDirTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *RemoveDirTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args removeDirInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.Path) == "" {
		err := errors.New(removeDirToolName + ": path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	base := effectiveRoot(t.root, input.Workdir)

	_, target, err := tools.ResolveWorkspaceTarget(input, security.TargetTypePath, base, args.Path, resolvePath)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return tools.ToolResult{
				Name:    t.Name(),
				Content: "ok",
				Metadata: map[string]any{
					"path":       target,
					"removed":    false,
					"noop_write": true,
					"force":      args.Force,
				},
				Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
			}, nil
		}
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), statErr), "", nil), statErr
	}
	if !info.IsDir() {
		err := errors.New(removeDirToolName + ": path is not a directory; use filesystem_delete_file")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if args.Force {
		if err := os.RemoveAll(target); err != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	} else {
		if err := os.Remove(target); err != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":    target,
			"removed": true,
			"force":   args.Force,
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}, nil
}
