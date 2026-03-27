package tools

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/dust/neo-code/internal/provider"
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

func (r *Registry) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	tool, err := r.Get(input.Name)
	if err != nil {
		return ToolResult{
			ToolCallID: input.ID,
			Name:       input.Name,
			Content:    err.Error(),
			IsError:    true,
		}, err
	}

	result, execErr := tool.Execute(ctx, input)
	result.ToolCallID = input.ID
	if execErr != nil {
		result.IsError = true
		if strings.TrimSpace(result.Content) == "" {
			result.Content = execErr.Error()
		}
		return result, execErr
	}
	return result, nil
}
