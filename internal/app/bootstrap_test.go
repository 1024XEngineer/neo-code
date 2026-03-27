package app

import (
	"testing"

	"neocode/internal/config"
)

func TestRegisterBuiltinToolsIncludesEditFile(t *testing.T) {
	registry, err := registerBuiltinTools(config.Config{Shell: "powershell"})
	if err != nil {
		t.Fatalf("register builtin tools: %v", err)
	}

	found := false
	for _, schema := range registry.ListSchemas() {
		if schema.Name == "fs_edit_file" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fs_edit_file to be registered")
	}
}
