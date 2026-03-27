package filesystem

import "testing"

func TestToolMetadataAndHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tools := []interface {
		Name() string
		Description() string
		Schema() map[string]any
	}{
		New(root),
		NewWrite(root),
		NewGrep(root),
		NewGlob(root),
		NewEdit(root),
	}

	for _, tool := range tools {
		if tool.Name() == "" {
			t.Fatalf("expected tool name")
		}
		if tool.Description() == "" {
			t.Fatalf("expected description for %q", tool.Name())
		}
		schema := tool.Schema()
		if schema["type"] != "object" {
			t.Fatalf("expected object schema for %q, got %+v", tool.Name(), schema)
		}
	}

	if effectiveRoot("", root) != root {
		t.Fatalf("expected workdir fallback")
	}
	if got := effectiveRoot(root, ""); got != root {
		t.Fatalf("expected default root, got %q", got)
	}
	if rel := toRelativePath(root, root); rel != "." {
		t.Fatalf("expected relative root path '.', got %q", rel)
	}
}
