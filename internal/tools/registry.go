package tools

import (
	"context"
	"fmt"
	"sync"

	"neocode/internal/provider"
)

// Registry stores builtin tools and exposes them in a provider-friendly form.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool %q already registered", tool.Name())
	}

	r.tools[tool.Name()] = tool
	return nil
}

// ListSchemas returns all registered tools as provider tool specs.
func (r *Registry) ListSchemas() []provider.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]provider.ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, provider.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.Schema(),
		})
	}

	return specs
}

// Execute runs a named tool and normalizes error output.
func (r *Registry) Execute(ctx context.Context, call Invocation) (Result, error) {
	r.mu.RLock()
	tool, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		err := fmt.Errorf("tool %q not found", call.Name)
		return Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}, err
	}

	result, err := tool.Execute(ctx, call)
	if result.ToolCallID == "" {
		result.ToolCallID = call.ID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	if err != nil {
		result.IsError = true
		if result.Content == "" {
			result.Content = err.Error()
		}
	}

	return result, err
}
