package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"neocode/internal/tools"
)

// EditFileTool replaces one exact text snippet in an existing file within the configured workdir.
type EditFileTool struct{}

// NewEditFileTool constructs the file editor tool.
func NewEditFileTool() *EditFileTool {
	return &EditFileTool{}
}

// Name returns the stable tool name.
func (t *EditFileTool) Name() string {
	return "fs_edit_file"
}

// Description describes the tool for the model.
func (t *EditFileTool) Description() string {
	return "Edit an existing UTF-8 text file within the current workdir by replacing one exact text snippet."
}

// Schema returns the JSON schema for tool arguments.
func (t *EditFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative file path under the workdir.",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "The exact existing text snippet to replace. It must match exactly once.",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "The replacement text snippet.",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

// Execute performs the file edit.
func (t *EditFileTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = ctx

	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return tools.Result{}, fmt.Errorf("path is required")
	}
	if args.OldText == "" {
		return tools.Result{}, fmt.Errorf("old_text is required")
	}

	resolvedPath, err := resolvePath(call.Workdir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return tools.Result{}, wrapPathError("stat", args.Path, err)
	}
	if info.IsDir() {
		return tools.Result{}, fmt.Errorf("path %q is a directory", args.Path)
	}
	if info.Size() > maxWriteBytes {
		return tools.Result{}, fmt.Errorf("file exceeds %d bytes", maxWriteBytes)
	}

	payload, err := os.ReadFile(resolvedPath)
	if err != nil {
		return tools.Result{}, wrapPathError("read", args.Path, err)
	}
	if len(payload) > maxWriteBytes {
		return tools.Result{}, fmt.Errorf("file exceeds %d bytes", maxWriteBytes)
	}

	content := string(payload)
	occurrences := strings.Count(content, args.OldText)
	switch {
	case occurrences == 0:
		return tools.Result{}, fmt.Errorf(
			"old_text was not found in %q; re-read the file with fs_read_file and retry with an exact snippet",
			args.Path,
		)
	case occurrences > 1:
		return tools.Result{}, fmt.Errorf(
			"old_text matched %d times in %q; provide a longer, unique snippet and retry",
			occurrences,
			args.Path,
		)
	}

	updated := strings.Replace(content, args.OldText, args.NewText, 1)
	if len(updated) > maxWriteBytes {
		return tools.Result{}, fmt.Errorf("edited content exceeds %d bytes", maxWriteBytes)
	}

	if err := writeFileAtomically(resolvedPath, []byte(updated), info.Mode().Perm()); err != nil {
		return tools.Result{}, err
	}

	return tools.Result{
		Content: fmt.Sprintf("edited %s by replacing 1 occurrence", resolvedPath),
		Metadata: map[string]any{
			"path":         resolvedPath,
			"replacements": 1,
			"bytes":        len(updated),
		},
	}, nil
}

func writeFileAtomically(path string, payload []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace file %q: %w", path, err)
		}
		if err := os.Rename(tempPath, path); err != nil {
			return fmt.Errorf("replace file %q after cleanup: %w", path, err)
		}
	}

	cleanup = false
	return nil
}
