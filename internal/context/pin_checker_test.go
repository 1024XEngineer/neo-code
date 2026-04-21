package context

import (
	"testing"
)

func TestDefaultPinCheckerMatchesKeyArtifacts(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	tests := []struct {
		toolName string
		path     string
		expected bool
	}{
		{toolName: "filesystem_write_file", path: "README.md", expected: true},
		{toolName: "filesystem_write_file", path: "README.txt", expected: true},
		{toolName: "filesystem_write_file", path: "readme.md", expected: false}, // glob 区分大小写
		{toolName: "filesystem_write_file", path: "api.spec.yaml", expected: true},
		{toolName: "filesystem_write_file", path: "design.spec.md", expected: true},
		{toolName: "filesystem_write_file", path: "db.schema.json", expected: true},
		{toolName: "filesystem_write_file", path: "schema.sql", expected: false}, // *schema.* 需要两端有内容
		{toolName: "filesystem_write_file", path: "db.schema.sql", expected: true},
		{toolName: "filesystem_write_file", path: "docker-compose.yml", expected: true},
		{toolName: "filesystem_write_file", path: "docker-compose.yaml", expected: true},
		{toolName: "filesystem_write_file", path: ".env", expected: false},
		{toolName: "filesystem_write_file", path: ".env.local", expected: false},
		{toolName: "filesystem_write_file", path: ".env.example", expected: false},
		{toolName: "filesystem_write_file", path: "01_migration.sql", expected: true},
		{toolName: "filesystem_write_file", path: "migration.rb", expected: true},
		{toolName: "filesystem_write_file", path: "create_users_migration.sql", expected: true},
		{toolName: "filesystem_write_file", path: "Makefile", expected: true},
		{toolName: "filesystem_write_file", path: "go.mod", expected: true},
		{toolName: "filesystem_write_file", path: "package.json", expected: true},
		{toolName: "filesystem_write_file", path: "main.go", expected: false},
		{toolName: "filesystem_write_file", path: "app.tsx", expected: false},
		{toolName: "filesystem_write_file", path: "index.js", expected: false},
		{toolName: "filesystem_write_file", path: "utils.py", expected: false},
		{toolName: "filesystem_write_file", path: "style.css", expected: false},
		{toolName: "filesystem_edit", path: "README.md", expected: true},
		{toolName: "filesystem_read_file", path: "README.md", expected: false},
		{toolName: "bash", path: "README.md", expected: false},
	}

	for _, tt := range tests {
		got := checker.ShouldPin(tt.toolName, map[string]string{"path": "/project/" + tt.path})
		if got != tt.expected {
			t.Errorf("ShouldPin(tool=%q, path=%q) = %v, want %v", tt.toolName, tt.path, got, tt.expected)
		}
	}
}

func TestDefaultPinCheckerUsesRelativePath(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	// relative_path 优先于 path
	got := checker.ShouldPin("filesystem_write_file", map[string]string{
		"relative_path": "api.spec.yaml",
	})
	if !got {
		t.Error("expected relative_path match for api.spec.yaml")
	}
}

func TestDefaultPinCheckerFallsBackToPath(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	got := checker.ShouldPin("filesystem_write_file", map[string]string{
		"path": "/project/README.md",
	})
	if !got {
		t.Error("expected path fallback match for README.md")
	}
}

func TestDefaultPinCheckerNoPathReturnsFalse(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	got := checker.ShouldPin("filesystem_write_file", map[string]string{"workdir": "/tmp"})
	if got {
		t.Error("expected false when no path in metadata")
	}
}

func TestDefaultPinCheckerEmptyMetadataReturnsFalse(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	got := checker.ShouldPin("filesystem_write_file", nil)
	if got {
		t.Error("expected false for nil metadata")
	}
}

func TestDefaultPinCheckerBashToolNotPinned(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	// bash 工具元信息只有 workdir，不应被钉住
	got := checker.ShouldPin("bash", map[string]string{"workdir": "/project"})
	if got {
		t.Error("expected bash tool with workdir only to not be pinned")
	}
}

func TestDefaultPinCheckerIgnoresPathMetadataForUnsupportedTool(t *testing.T) {
	t.Parallel()

	checker := NewDefaultPinChecker()

	got := checker.ShouldPin("filesystem_read_file", map[string]string{
		"path": "/project/README.md",
	})
	if got {
		t.Error("expected unsupported tool with path metadata to not be pinned")
	}
}
