package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GlobalSchemaPrompt renders the currently registered tool schemas as an
// English system-context block for the model.
func GlobalSchemaPrompt() (string, error) {
	return BuildSchemaPrompt(GlobalRegistry.ListDefinitions())
}

// BuildSchemaPrompt converts tool definitions into a stable, machine-readable
// prompt block so the model can treat schema definitions as the single source
// of truth for tool calling.
func BuildSchemaPrompt(defs []ToolDefinition) (string, error) {
	if len(defs) == 0 {
		return "", nil
	}

	payload := struct {
		Tools []ToolDefinition `json:"tools"`
	}{
		Tools: defs,
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal tool schema prompt: %w", err)
	}

	parts := []string{
		"[TOOL_SCHEMAS]",
		"Use these tool schemas as the single source of truth for tool names, parameter names, parameter types, required fields, default values, and enumerated choices.",
		"When you need to call a tool, emit exactly one JSON object in the form {\"tool\":\"<name>\",\"params\":{...}}.",
		"Do not invent tool names or parameters. Omit optional parameters when they are unnecessary.",
		string(encoded),
		"[/TOOL_SCHEMAS]",
	}
	return strings.Join(parts, "\n"), nil
}
