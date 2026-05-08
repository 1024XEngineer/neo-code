package codebase

import (
	"context"
	"encoding/json"
	"strings"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// ReadTool implements the codebase_read tool.
type ReadTool struct {
	root string
	svc  *repository.Service
}

// NewRead creates a new codebase_read tool.
func NewRead(svc *repository.Service, root string) *ReadTool {
	return &ReadTool{root: root, svc: svc}
}

func (t *ReadTool) Name() string {
	return tools.ToolNameCodebaseRead
}

func (t *ReadTool) Description() string {
	return "Read the content of a file within the workspace. Use this when you need to see implementation details after locating a file via search tools."
}

func (t *ReadTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path to the file within the workspace.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Maximum bytes to read (default 256KB).",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

func (t *ReadTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Path     string `json:"path"`
		Workdir  string `json:"workdir,omitempty"`
		MaxBytes int    `json:"max_bytes,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(in.Path) == "" {
		err := &json.UnmarshalTypeError{}
		return tools.NewErrorResult(t.Name(), "missing required argument: path", "", nil), err
	}

	root, err := tools.ResolveEffectiveRoot(t.root, in.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}
	result, err := t.svc.Read(ctx, root, in.Path, repository.ReadOptions{MaxBytes: in.MaxBytes})
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	content := formatReadResult(result)
	return tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		Metadata: map[string]any{
			"path":      result.Path,
			"truncated": result.Truncated,
			"is_binary": result.IsBinary,
			"size":      result.Size,
		},
	}, nil
}

func formatReadResult(r repository.ReadResult) string {
	if r.Path == "" && r.Content == "" {
		return "file not found or access denied"
	}
	var b strings.Builder
	b.WriteString("path: ")
	b.WriteString(r.Path)
	b.WriteString("\nis_binary: ")
	b.WriteString(boolToString(r.IsBinary))
	b.WriteString("\ntruncated: ")
	b.WriteString(boolToString(r.Truncated))
	b.WriteString("\nsize: ")
	b.WriteString(itoa(int(r.Size)))
	if !r.IsBinary && r.Content != "" {
		b.WriteString("\n\n")
		b.WriteString(r.Content)
	}
	return b.String()
}
