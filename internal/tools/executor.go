package tools

import (
	"context"
	"fmt"
)

// Executor runs a named tool and normalizes the result contract for Runtime.
type Executor interface {
	Execute(ctx context.Context, call Invocation) (Result, error)
}

type registryExecutor struct {
	registry *Registry
}

// NewExecutor creates an executor backed by the provided registry.
func NewExecutor(registry *Registry) Executor {
	return &registryExecutor{registry: registry}
}

func (e *registryExecutor) Execute(ctx context.Context, call Invocation) (Result, error) {
	tool, ok := e.registry.Lookup(call.Name)
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
