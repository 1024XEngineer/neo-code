package context

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectRulesOrdersGlobalToLocal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	rootRules := filepath.Join(root, ruleFileName)
	localRules := filepath.Join(root, "a", ruleFileName)
	if err := os.WriteFile(rootRules, []byte("root-rules"), 0o644); err != nil {
		t.Fatalf("write root rules: %v", err)
	}
	if err := os.WriteFile(localRules, []byte("local-rules"), 0o644); err != nil {
		t.Fatalf("write local rules: %v", err)
	}

	documents, err := loadProjectRules(context.Background(), nested)
	if err != nil {
		t.Fatalf("loadProjectRules() error = %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("expected 2 rule documents, got %d", len(documents))
	}
	if documents[0].Path != rootRules || documents[1].Path != localRules {
		t.Fatalf("expected global-to-local order, got %+v", documents)
	}

	section := renderProjectRulesSection(documents)
	rootIndex := strings.Index(section, rootRules)
	localIndex := strings.Index(section, localRules)
	if rootIndex < 0 || localIndex < 0 || rootIndex >= localIndex {
		t.Fatalf("expected rendered rules to stay global-to-local, got %q", section)
	}
}

func TestLoadProjectRulesOnlyMatchesUppercase(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "child")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "agents.md"), []byte("wrong-case"), 0o644); err != nil {
		t.Fatalf("write lowercase rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, ruleFileName), []byte("right-case"), 0o644); err != nil {
		t.Fatalf("write uppercase rules: %v", err)
	}

	documents, err := loadProjectRules(context.Background(), nested)
	if err != nil {
		t.Fatalf("loadProjectRules() error = %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("expected only uppercase AGENTS.md to be loaded, got %+v", documents)
	}
	if filepath.Base(documents[0].Path) != ruleFileName {
		t.Fatalf("expected uppercase AGENTS.md match, got %q", documents[0].Path)
	}
	if strings.Contains(documents[0].Content, "wrong-case") {
		t.Fatalf("did not expect lowercase agents.md content to be loaded")
	}
}

func TestLoadRuleDocumentsReturnsReadError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ruleFileName)
	if err := os.WriteFile(path, []byte("rules"), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	_, err := loadRuleDocuments(context.Background(), []string{path}, func(string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestDiscoverRuleFilesStopsOnDirectoryReadError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	rootRules := filepath.Join(root, ruleFileName)
	localRules := filepath.Join(root, "a", ruleFileName)
	if err := os.WriteFile(rootRules, []byte("root-rules"), 0o644); err != nil {
		t.Fatalf("write root rules: %v", err)
	}
	if err := os.WriteFile(localRules, []byte("local-rules"), 0o644); err != nil {
		t.Fatalf("write local rules: %v", err)
	}

	permissionErr := errors.New("permission denied")
	paths, err := discoverRuleFilesWithFinder(context.Background(), nested, func(dir string) (string, error) {
		switch dir {
		case nested:
			return "", nil
		case filepath.Join(root, "a"):
			return localRules, nil
		case root:
			return "", permissionErr
		default:
			return "", nil
		}
	})
	if err != nil {
		t.Fatalf("discoverRuleFilesWithFinder() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != localRules {
		t.Fatalf("expected traversal to stop after read error and keep collected paths, got %+v", paths)
	}
}

func TestRenderProjectRulesSectionTruncatesSingleFileAndTotalBudget(t *testing.T) {
	t.Parallel()

	largeSingle := strings.Repeat("a", maxRuleFileRunes+32)
	largeTotalA := strings.Repeat("b", 7000)
	largeTotalB := strings.Repeat("c", 7000)

	section := renderProjectRulesSection([]ruleDocument{
		{Path: "/repo/AGENTS.md", Content: largeSingle[:maxRuleFileRunes], Truncated: true},
	})
	if !strings.Contains(section, "[truncated to fit per-file limit]") {
		t.Fatalf("expected per-file truncation marker, got %q", section)
	}

	totalSection := renderProjectRulesSection([]ruleDocument{
		{Path: "/repo/root/AGENTS.md", Content: largeTotalA},
		{Path: "/repo/root/app/AGENTS.md", Content: largeTotalB},
	})
	if !strings.Contains(totalSection, "[additional project rules truncated to fit total limit]") {
		t.Fatalf("expected total truncation marker, got %q", totalSection)
	}
	if strings.Contains(totalSection, strings.Repeat("c", 6500)) {
		t.Fatalf("expected total rules section to be truncated")
	}
	body := strings.TrimPrefix(totalSection, "## Project Rules\n")
	if runeCount(body) > maxTotalRuleRunes {
		t.Fatalf("expected rendered rules body to respect total rune budget, got %d > %d", runeCount(body), maxTotalRuleRunes)
	}
}
