package tools

import (
	"testing"

	"go-llm-demo/internal/server/domain"
)

func TestBuildToolSchemasMapsRequiredAndDefaults(t *testing.T) {
	defs := []domain.ToolDefinition{
		{
			Name:        "demo",
			Description: "demo tool",
			Parameters: []domain.ToolParamSpec{
				{Name: "path", Type: "string", Required: true, Description: "target path"},
				{Name: "limit", Type: "integer", DefaultValue: 10},
				{Name: "mode", Type: "string", Enum: []string{"fast", "slow"}},
			},
		},
	}

	got := BuildToolSchemas(defs)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool schema, got %d", len(got))
	}
	schema := got[0]
	if schema.Type != "function" {
		t.Fatalf("expected function schema, got %q", schema.Type)
	}
	if schema.Function.Name != "demo" {
		t.Fatalf("expected tool name demo, got %q", schema.Function.Name)
	}
	if schema.Function.Parameters.Type != "object" {
		t.Fatalf("expected parameters type object, got %q", schema.Function.Parameters.Type)
	}
	if len(schema.Function.Parameters.Required) != 1 || schema.Function.Parameters.Required[0] != "path" {
		t.Fatalf("expected required path, got %+v", schema.Function.Parameters.Required)
	}
	if schema.Function.Parameters.Properties["limit"].Default != 10 {
		t.Fatalf("expected limit default 10, got %+v", schema.Function.Parameters.Properties["limit"].Default)
	}
	if len(schema.Function.Parameters.Properties["mode"].Enum) != 2 {
		t.Fatalf("expected enum values, got %+v", schema.Function.Parameters.Properties["mode"].Enum)
	}
}
