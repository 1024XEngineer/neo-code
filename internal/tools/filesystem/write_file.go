package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"neocode/internal/tools"
)

// WriteFileTool writes text content within the configured workdir.
type WriteFileTool struct{}

// NewWriteFileTool constructs the file writer tool.
func NewWriteFileTool() *WriteFileTool {
	return &WriteFileTool{}
}

// Name returns the stable tool name.
func (t *WriteFileTool) Name() string {
	return "fs_write_file"
}

// Description describes the tool for the model.
func (t *WriteFileTool) Description() string {
	return "Create, overwrite, or append a UTF-8 text file within the current workdir. Use fs_edit_file for targeted in-place edits."
}

// Schema returns the JSON schema for tool arguments.
func (t *WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path under the workdir.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The text content to write.",
			},
			"append": map[string]any{
				"type":        "boolean",
				"description": "Whether to append instead of overwrite.",
			},
		},
		"required": []string{"path", "content"},
	}
}

// Execute performs the file write.
func (t *WriteFileTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = ctx

	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return tools.Result{}, fmt.Errorf("path is required")
	}
	if len(args.Content) > maxWriteBytes {
		return tools.Result{}, fmt.Errorf("content exceeds %d bytes", maxWriteBytes)
	}

	resolvedPath, err := resolvePath(call.Workdir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return tools.Result{}, err
	}

	var writeErr error
	if args.Append {
		var file *os.File
		file, writeErr = os.OpenFile(resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if writeErr == nil {
			_, writeErr = file.WriteString(args.Content)
			file.Close()
		}
	} else {
		writeErr = os.WriteFile(resolvedPath, []byte(args.Content), 0o644)
	}
	if writeErr != nil {
		return tools.Result{}, writeErr
	}

	return tools.Result{
		Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), resolvedPath),
		Metadata: map[string]any{
			"path":   resolvedPath,
			"append": args.Append,
			"bytes":  len(args.Content),
		},
	}, nil
}
