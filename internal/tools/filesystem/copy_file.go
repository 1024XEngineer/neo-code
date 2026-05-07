package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/tools"
)

type CopyFileTool struct {
	root string
}

type copyFileInput struct {
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Overwrite       bool   `json:"overwrite,omitempty"`
}

func NewCopy(root string) *CopyFileTool {
	return &CopyFileTool{root: root}
}

func (t *CopyFileTool) Name() string {
	return copyFileToolName
}

func (t *CopyFileTool) Description() string {
	return "Copy a file inside the workspace. Both paths must resolve inside the workspace."
}

func (t *CopyFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_path": map[string]any{
				"type":        "string",
				"description": "Existing file path to copy, relative to workspace root or absolute inside workspace.",
			},
			"destination_path": map[string]any{
				"type":        "string",
				"description": "Destination file path, relative to workspace root or absolute inside workspace.",
			},
			"overwrite": map[string]any{
				"type":        "boolean",
				"description": "When true, replace destination if it already exists. Defaults to false.",
			},
		},
		"required": []string{"source_path", "destination_path"},
	}
}

func (t *CopyFileTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *CopyFileTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args copyFileInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.SourcePath) == "" {
		err := errors.New(copyFileToolName + ": source_path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if strings.TrimSpace(args.DestinationPath) == "" {
		err := errors.New(copyFileToolName + ": destination_path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	base, err := tools.ResolveEffectiveRoot(t.root, input.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}

	src, err := resolvePath(base, args.SourcePath)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	dst, err := resolvePath(base, args.DestinationPath)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	srcInfo, statErr := os.Stat(src)
	if statErr != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), statErr), "", nil), statErr
	}
	if srcInfo.IsDir() {
		err := errors.New(copyFileToolName + ": source_path must be a file, not a directory")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if _, err := os.Stat(dst); err == nil {
		if !args.Overwrite {
			err := errors.New(copyFileToolName + ": destination_path already exists; pass overwrite=true to replace it")
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	} else if !os.IsNotExist(err) {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := copyFileContents(src, dst, srcInfo.Mode().Perm()); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"source_path":      src,
			"destination_path": dst,
			"paths":            []string{dst},
			"bytes":            srcInfo.Size(),
			"overwrite":        args.Overwrite,
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}, nil
}
