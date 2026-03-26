package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"neocode/internal/tools"
)

// ListDirTool lists files under the configured workdir.
type ListDirTool struct{}

// NewListDirTool constructs the directory listing tool.
func NewListDirTool() *ListDirTool {
	return &ListDirTool{}
}

// Name returns the stable tool name.
func (t *ListDirTool) Name() string {
	return "fs_list_dir"
}

// Description describes the tool for the model.
func (t *ListDirTool) Description() string {
	return "List directories and files within the current workdir."
}

// Schema returns the JSON schema for tool arguments.
func (t *ListDirTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative directory path under the workdir. Defaults to current directory.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "Whether to walk subdirectories recursively.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of entries to return.",
			},
		},
	}
}

// Execute performs the directory listing.
func (t *ListDirTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = ctx

	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}

	resolvedPath, err := resolvePath(call.Workdir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return tools.Result{}, err
	}
	if !info.IsDir() {
		return tools.Result{}, fmt.Errorf("path %q is not a directory", args.Path)
	}

	limit := args.Limit
	if limit <= 0 || limit > defaultListLimit {
		limit = defaultListLimit
	}

	lines := make([]string, 0, limit)
	if args.Recursive {
		count := 0
		walkErr := filepath.WalkDir(resolvedPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if count >= limit {
				return fs.SkipAll
			}
			rel, err := filepath.Rel(resolvedPath, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			lines = append(lines, formatEntry(rel, entry))
			count++
			return nil
		})
		if walkErr != nil && walkErr != fs.SkipAll {
			return tools.Result{}, walkErr
		}
	} else {
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return tools.Result{}, err
		}
		for idx, entry := range entries {
			if idx >= limit {
				break
			}
			lines = append(lines, formatEntry(entry.Name(), entry))
		}
	}

	if len(lines) == 0 {
		lines = append(lines, "[empty]")
	}

	return tools.Result{
		Content: fmt.Sprintf("directory: %s\n\n%s", resolvedPath, strings.Join(lines, "\n")),
		Metadata: map[string]any{
			"path":  resolvedPath,
			"count": len(lines),
		},
	}, nil
}

func formatEntry(path string, entry fs.DirEntry) string {
	if entry.IsDir() {
		return "[dir]  " + path
	}
	return "[file] " + path
}
