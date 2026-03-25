package tools

import (
	"strings"
	"testing"
)

func TestBuildSchemaPromptIncludesDefaultsAndEnums(t *testing.T) {
	prompt, err := BuildSchemaPrompt([]ToolDefinition{
		{
			Name:        "todo",
			Description: "Manage todos.",
			Parameters: []ToolParamSpec{
				{Name: "action", Type: "string", Required: true, Enum: []string{"add", "list"}},
				{Name: "priority", Type: "string", DefaultValue: "medium"},
				{Name: "timeout", Type: "integer", DefaultValue: 120000},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(prompt, "[TOOL_SCHEMAS]") {
		t.Fatalf("expected tool schema marker, got %q", prompt)
	}
	if !strings.Contains(prompt, `"name": "todo"`) {
		t.Fatalf("expected tool name in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, `"default": "medium"`) {
		t.Fatalf("expected string default in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, `"default": 120000`) {
		t.Fatalf("expected integer default in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, `"enum": [`) || !strings.Contains(prompt, `"add"`) {
		t.Fatalf("expected enum values in prompt, got %q", prompt)
	}
}

func TestBuildSchemaPromptHandlesEmptyDefinitions(t *testing.T) {
	prompt, err := BuildSchemaPrompt(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if prompt != "" {
		t.Fatalf("expected empty prompt for empty schema, got %q", prompt)
	}
}

func TestToolDefinitionsExposeStructuredDefaults(t *testing.T) {
	defs := NewToolRegistry().ListDefinitions()
	var foundBash bool

	for _, def := range defs {
		if def.Name == "bash" {
			foundBash = true
			var timeoutFound bool
			for _, param := range def.Parameters {
				if param.Name == "timeout" {
					timeoutFound = true
					if param.DefaultValue != 120000 {
						t.Fatalf("expected bash timeout default 120000, got %#v", param.DefaultValue)
					}
				}
			}
			if !timeoutFound {
				t.Fatal("expected bash timeout parameter")
			}
		}
	}

	if !foundBash {
		t.Fatal("expected bash definition to be registered")
	}
}
