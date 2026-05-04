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

type CreateDirTool struct {
	root string
}

type createDirInput struct {
	Path      string `json:"path"`
	Recursive *bool  `json:"recursive,omitempty"`
}

func NewCreateDir(root string) *CreateDirTool {
	return &CreateDirTool{root: root}
}

func (t *CreateDirTool) Name() string {
	return createDirToolName
}

func (t *CreateDirTool) Description() string {
	return "Create a directory inside the workspace. Recursive by default."
}

func (t *CreateDirTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path relative to workspace root, or absolute inside the workspace.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "When true (default), create parent directories as needed; when false, fail if the parent is missing.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *CreateDirTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *CreateDirTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args createDirInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.Path) == "" {
		err := errors.New(createDirToolName + ": path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	recursive := true
	if args.Recursive != nil {
		recursive = *args.Recursive
	}

	base := effectiveRoot(t.root, input.Workdir)

	_, target, err := tools.ResolveWorkspaceTarget(input, security.TargetTypePath, base, args.Path, resolvePath)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			err := errors.New(createDirToolName + ": path exists and is not a directory")
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
		return tools.ToolResult{
			Name:    t.Name(),
			Content: "ok",
			Metadata: map[string]any{
				"path":       target,
				"created":    false,
				"noop_write": true,
				"recursive":  recursive,
			},
			Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
		}, nil
	} else if !os.IsNotExist(statErr) {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), statErr), "", nil), statErr
	}

	if recursive {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	} else {
		if err := os.Mkdir(target, 0o755); err != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":      target,
			"created":   true,
			"recursive": recursive,
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}, nil
}
