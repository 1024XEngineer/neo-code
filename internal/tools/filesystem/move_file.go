package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/tools"
)

type MoveFileTool struct {
	root string
}

type moveFileInput struct {
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Overwrite       bool   `json:"overwrite,omitempty"`
}

func NewMove(root string) *MoveFileTool {
	return &MoveFileTool{root: root}
}

func (t *MoveFileTool) Name() string {
	return moveFileToolName
}

func (t *MoveFileTool) Description() string {
	return "Move or rename a file inside the workspace. Both paths must resolve inside the workspace."
}

func (t *MoveFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_path": map[string]any{
				"type":        "string",
				"description": "Existing file path to move, relative to workspace root or absolute inside workspace.",
			},
			"destination_path": map[string]any{
				"type":        "string",
				"description": "New file path, relative to workspace root or absolute inside workspace.",
			},
			"overwrite": map[string]any{
				"type":        "boolean",
				"description": "When true, replace destination if it already exists. Defaults to false.",
			},
		},
		"required": []string{"source_path", "destination_path"},
	}
}

func (t *MoveFileTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *MoveFileTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args moveFileInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.SourcePath) == "" {
		err := errors.New(moveFileToolName + ": source_path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if strings.TrimSpace(args.DestinationPath) == "" {
		err := errors.New(moveFileToolName + ": destination_path is required")
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
		err := errors.New(moveFileToolName + ": source_path must be a file, not a directory")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if _, err := os.Stat(dst); err == nil {
		if !args.Overwrite {
			err := errors.New(moveFileToolName + ": destination_path already exists; pass overwrite=true to replace it")
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
	} else if !os.IsNotExist(err) {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := os.Rename(src, dst); err != nil {
		if !isCrossDeviceLinkError(err) {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
		}
		if copyErr := copyFileContents(src, dst, srcInfo.Mode().Perm()); copyErr != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), copyErr), "", nil), copyErr
		}
		if removeErr := os.Remove(src); removeErr != nil {
			return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), removeErr), "", nil), removeErr
		}
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"source_path":      src,
			"destination_path": dst,
			"paths":            []string{src, dst},
			"bytes":            srcInfo.Size(),
			"overwrite":        args.Overwrite,
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}, nil
}

func copyFileContents(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func isCrossDeviceLinkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cross-device") || strings.Contains(msg, "exdev")
}
