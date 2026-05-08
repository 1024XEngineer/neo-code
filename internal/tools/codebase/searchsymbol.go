package codebase

import (
	"context"
	"encoding/json"
	"strings"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// SearchSymbolTool implements the codebase_search_symbol tool.
type SearchSymbolTool struct {
	root string
	svc  *repository.Service
}

// NewSearchSymbol creates a new codebase_search_symbol tool.
func NewSearchSymbol(svc *repository.Service, root string) *SearchSymbolTool {
	return &SearchSymbolTool{root: root, svc: svc}
}

func (t *SearchSymbolTool) Name() string {
	return tools.ToolNameCodebaseSearchSymbol
}

func (t *SearchSymbolTool) Description() string {
	return "Search for symbol definitions across the workspace. Returns file paths, line hints, kind (function/type/method/etc.), and signature. Does NOT return the function body; use codebase_read to view implementation."
}

func (t *SearchSymbolTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"symbol": map[string]any{
				"type":        "string",
				"description": "Symbol name to search for.",
			},
			"scope_dir": map[string]any{
				"type":        "string",
				"description": "Optional subdirectory to limit the search scope.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of hits to return (default 20, max 50).",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
		},
		"required": []string{"symbol"},
	}
}

func (t *SearchSymbolTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *SearchSymbolTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Symbol   string `json:"symbol"`
		ScopeDir string `json:"scope_dir,omitempty"`
		Limit    int    `json:"limit,omitempty"`
		Workdir  string `json:"workdir,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(in.Symbol) == "" {
		return tools.NewErrorResult(t.Name(), "missing required argument: symbol", "", nil), nil
	}

	root, err := tools.ResolveEffectiveRoot(t.root, in.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}
	opts := repository.SearchOptions{
		ScopeDir: in.ScopeDir,
		Limit:    in.Limit,
	}
	result, err := t.svc.SearchSymbol(ctx, root, in.Symbol, opts)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	content := formatSymbolSearchResult(result)
	return tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		Metadata: map[string]any{
			"returned_count": len(result.Hits),
			"total_count":    result.TotalCount,
			"truncated":      result.Truncated,
		},
	}, nil
}

func formatSymbolSearchResult(r repository.SymbolSearchResult) string {
	var b strings.Builder
	b.WriteString("returned_count: ")
	b.WriteString(itoa(len(r.Hits)))
	b.WriteString("\ntotal_count: ")
	b.WriteString(itoa(r.TotalCount))
	b.WriteString("\ntruncated: ")
	b.WriteString(boolToString(r.Truncated))
	if len(r.Hits) > 0 {
		b.WriteString("\n")
	}
	for _, h := range r.Hits {
		b.WriteString("\n- path: ")
		b.WriteString(h.Path)
		b.WriteString("\n  line_hint: ")
		b.WriteString(itoa(h.LineHint))
		b.WriteString("\n  kind: ")
		b.WriteString(h.Kind)
		b.WriteString("\n  signature: ")
		b.WriteString(h.Signature)
	}
	return b.String()
}
