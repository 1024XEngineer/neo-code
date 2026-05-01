package context

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/rules"
)

func TestResolveProjectRootPrefersStableProjectRoot(t *testing.T) {
	t.Parallel()

	metadata := Metadata{
		ProjectRoot: "/workspace/project",
		Workdir:     "/workspace/project/subdir",
	}
	if got := resolveProjectRoot(metadata); got != "/workspace/project" {
		t.Fatalf("resolveProjectRoot() = %q, want %q", got, "/workspace/project")
	}
}

func TestResolveProjectRootFallsBackToWorkdir(t *testing.T) {
	t.Parallel()

	metadata := Metadata{Workdir: "/workspace/project/subdir"}
	if got := resolveProjectRoot(metadata); got != "/workspace/project/subdir" {
		t.Fatalf("resolveProjectRoot() = %q, want fallback workdir", got)
	}
}

func TestRenderRulesSectionSkipsEmptySnapshot(t *testing.T) {
	t.Parallel()

	if section := renderRulesSection(rules.Snapshot{}); renderPromptSection(section) != "" {
		t.Fatalf("expected empty rules section, got %q", renderPromptSection(section))
	}
}

func TestRenderRulesSectionIncludesProjectBeforeGlobal(t *testing.T) {
	t.Parallel()

	section := renderPromptSection(renderRulesSection(rules.Snapshot{
		ProjectAGENTS: rules.Document{
			Path:    "/repo/AGENTS.md",
			Content: "project-rules",
		},
		GlobalAGENTS: rules.Document{
			Path:    "/home/.neocode/AGENTS.md",
			Content: "global-rules",
		},
	}))
	projectIndex := strings.Index(section, "### Project Rules")
	globalIndex := strings.Index(section, "### Global Rules")
	if projectIndex < 0 || globalIndex < 0 || projectIndex > globalIndex {
		t.Fatalf("expected project rules before global rules, got %q", section)
	}
	if !strings.Contains(section, "Treat them as higher priority than memory.") {
		t.Fatalf("expected rules priority intro, got %q", section)
	}
}

func TestRenderRulesDocumentBlockIncludesTruncationMarker(t *testing.T) {
	t.Parallel()

	block := renderRulesDocumentBlock("Project Rules", rules.Document{
		Path:      "/repo/AGENTS.md",
		Content:   "trimmed",
		Truncated: true,
	})
	if !strings.Contains(block, "[truncated to fit rule file limit]") {
		t.Fatalf("expected truncation marker, got %q", block)
	}
}

func TestRulesPromptSourceUsesProjectRootInsteadOfNestedWorkdir(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	projectRoot := t.TempDir()
	nested := filepath.Join(projectRoot, "nested")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, projectRuleFileName), []byte("project-root-rules"), 0o644); err != nil {
		t.Fatalf("write project AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, projectRuleFileName), []byte("nested-rules"), 0o644); err != nil {
		t.Fatalf("write nested AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, projectRuleFileName), []byte("global-rules"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	source := newRulesPromptSource(rules.NewLoader(baseDir))
	sections, err := source.Sections(context.Background(), BuildInput{
		Metadata: Metadata{
			ProjectRoot: projectRoot,
			Workdir:     nested,
		},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one rules section, got %+v", sections)
	}

	rendered := renderPromptSection(sections[0])
	if !strings.Contains(rendered, "project-root-rules") {
		t.Fatalf("expected project root rules, got %q", rendered)
	}
	if strings.Contains(rendered, "nested-rules") {
		t.Fatalf("did not expect nested workdir AGENTS.md to be used, got %q", rendered)
	}
	if !strings.Contains(rendered, "global-rules") {
		t.Fatalf("expected global rules, got %q", rendered)
	}
}
