package tools

import (
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

// Lookup returns a registered tool by name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	return tool, ok
}
