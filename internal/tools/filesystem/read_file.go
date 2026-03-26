package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"neocode/internal/tools"
)

// ReadFileTool reads a file inside the configured workdir.
type ReadFileTool struct{}

// NewReadFileTool constructs the file reader tool.
func NewReadFileTool() *ReadFileTool {
	return &ReadFileTool{}
}

// Name returns the stable tool name.
func (t *ReadFileTool) Name() string {
	return "fs_read_file"
}

// Description describes the tool for the model.
func (t *ReadFileTool) Description() string {
	return "Read a UTF-8 text file within the current workdir."
}

// Schema returns the JSON schema for tool arguments.
func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path under the workdir.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Optional byte offset to start reading from.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Optional maximum bytes to read, capped internally.",
			},
		},
		"required": []string{"path"},
	}
}

// Execute performs the file read.
func (t *ReadFileTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = ctx

	var args struct {
		Path   string `json:"path"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return tools.Result{}, fmt.Errorf("path is required")
	}

	resolvedPath, err := resolvePath(call.Workdir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return tools.Result{}, err
	}
	if info.IsDir() {
		return tools.Result{}, fmt.Errorf("path %q is a directory", args.Path)
	}

	limit := args.Limit
	if limit <= 0 || limit > maxReadBytes {
		limit = maxReadBytes
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return tools.Result{}, err
	}
	defer file.Close()

	if args.Offset > 0 {
		if _, err := file.Seek(args.Offset, io.SeekStart); err != nil {
			return tools.Result{}, err
		}
	}

	content, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return tools.Result{}, err
	}

	return tools.Result{
		Content: fmt.Sprintf("path: %s\nsize: %d bytes\n\n%s", resolvedPath, info.Size(), string(content)),
		Metadata: map[string]any{
			"path": resolvedPath,
			"size": info.Size(),
		},
	}, nil
}
