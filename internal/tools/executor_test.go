package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type executorTestTool struct {
	name        string
	description string
	schema      map[string]any
	result      Result
	err         error
}

func (t *executorTestTool) Name() string {
	return t.name
}

func (t *executorTestTool) Description() string {
	return t.description
}

func (t *executorTestTool) Schema() map[string]any {
	if t.schema != nil {
		return t.schema
	}
	return map[string]any{"type": "object"}
}

func (t *executorTestTool) Execute(context.Context, Invocation) (Result, error) {
	return t.result, t.err
}

func TestRegistryLookupFindsRegisteredTool(t *testing.T) {
	registry := NewRegistry()
	tool := &executorTestTool{name: "echo"}

	if err := registry.Register(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	got, ok := registry.Lookup("echo")
	if !ok {
		t.Fatalf("expected lookup to find tool")
	}
	if got != tool {
		t.Fatalf("expected lookup to return registered tool instance")
	}
}

func TestRegistryLookupMissingTool(t *testing.T) {
	registry := NewRegistry()

	if _, ok := registry.Lookup("missing"); ok {
		t.Fatalf("expected missing tool lookup to return false")
	}
}

func TestRegistryRegisterRejectsDuplicateName(t *testing.T) {
	registry := NewRegistry()
	tool := &executorTestTool{name: "echo"}

	if err := registry.Register(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	err := registry.Register(tool)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}
}

func TestExecutorReturnsErrorForUnknownTool(t *testing.T) {
	executor := NewExecutor(NewRegistry())
	args, err := json.Marshal(map[string]any{"value": "hello"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result, execErr := executor.Execute(context.Background(), Invocation{
		ID:        "call-1",
		Name:      "missing",
		Arguments: args,
	})

	if execErr == nil {
		t.Fatalf("expected unknown tool error")
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if result.ToolCallID != "call-1" {
		t.Fatalf("expected tool call id to be normalized, got %q", result.ToolCallID)
	}
	if result.Name != "missing" {
		t.Fatalf("expected result name to be normalized, got %q", result.Name)
	}
	if !strings.Contains(result.Content, "not found") {
		t.Fatalf("expected missing tool message, got %q", result.Content)
	}
}

func TestExecutorNormalizesEmptyResultIdentity(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&executorTestTool{name: "echo"}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	result, err := NewExecutor(registry).Execute(context.Background(), Invocation{
		ID:   "call-2",
		Name: "echo",
	})
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	if result.ToolCallID != "call-2" {
		t.Fatalf("expected normalized tool call id, got %q", result.ToolCallID)
	}
	if result.Name != "echo" {
		t.Fatalf("expected normalized tool name, got %q", result.Name)
	}
}

func TestExecutorNormalizesErrorResult(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&executorTestTool{
		name:   "explode",
		result: Result{},
		err:    errors.New("boom"),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	result, execErr := NewExecutor(registry).Execute(context.Background(), Invocation{
		ID:   "call-3",
		Name: "explode",
	})
	if execErr == nil {
		t.Fatalf("expected tool error")
	}
	if !result.IsError {
		t.Fatalf("expected error result to be marked")
	}
	if result.Content != "boom" {
		t.Fatalf("expected fallback error content, got %q", result.Content)
	}
	if result.ToolCallID != "call-3" || result.Name != "explode" {
		t.Fatalf("expected normalized identity, got %#v", result)
	}
}

func TestExecutorPreservesToolErrorContent(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&executorTestTool{
		name: "explode",
		result: Result{
			Content: "tool failed with context",
		},
		err: errors.New("boom"),
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	result, execErr := NewExecutor(registry).Execute(context.Background(), Invocation{
		ID:   "call-4",
		Name: "explode",
	})
	if execErr == nil {
		t.Fatalf("expected tool error")
	}
	if result.Content != "tool failed with context" {
		t.Fatalf("expected explicit tool content to be preserved, got %q", result.Content)
	}
	if !result.IsError {
		t.Fatalf("expected result to be marked as error")
	}
}
