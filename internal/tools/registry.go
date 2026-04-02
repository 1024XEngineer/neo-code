package tools

import (
	"context"
	"errors"
	"sort"
	"strings"

	"neo-code/internal/provider"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: map[string]Tool{},
	}
}

func (r *Registry) Register(tool Tool) {
	if tool == nil {
		return
	}
	r.tools[strings.ToLower(tool.Name())] = tool
}

func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.tools[strings.ToLower(name)]
	if !ok {
		return nil, errors.New("tool: not found")
	}
	return tool, nil
}

// Supports reports whether a tool is registered.
func (r *Registry) Supports(name string) bool {
	_, err := r.Get(name)
	return err == nil
}

func (r *Registry) GetSpecs() []provider.ToolSpec {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]provider.ToolSpec, 0, len(names))
	for _, name := range names {
		tool := r.tools[name]
		specs = append(specs, provider.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.Schema(),
		})
	}
	return specs
}

func (r *Registry) ListSchemas() []provider.ToolSpec {
	return r.GetSpecs()
}

// ListAvailableSpecs returns all registered tool specs.
func (r *Registry) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]provider.ToolSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.GetSpecs(), nil
}

func (r *Registry) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	tool, err := r.Get(input.Name)
	if err != nil {
		content := FormatError(input.Name, NormalizeErrorReason(input.Name, err), "")
		return ToolResult{
			ToolCallID: input.ID,
			Name:       input.Name,
			Content:    content,
			IsError:    true,
		}, err
	}

	result, execErr := tool.Execute(ctx, input)
	result.ToolCallID = input.ID
	if execErr != nil {
		result.IsError = true
		if strings.TrimSpace(result.Content) == "" {
			result.Content = FormatError(result.Name, NormalizeErrorReason(result.Name, execErr), "")
		}
		return result, execErr
	}
	return result, nil
}
