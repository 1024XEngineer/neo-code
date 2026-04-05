package context

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	dir     bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func (f fakeFileInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}

func TestLoadProjectRulesOrdersGlobalToLocal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	rootRules := filepath.Join(root, projectRuleFileName)
	localRules := filepath.Join(root, "a", projectRuleFileName)
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

	section := renderPromptSection(renderProjectRulesSection(documents))
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
	if err := os.WriteFile(filepath.Join(nested, projectRuleFileName), []byte("right-case"), 0o644); err != nil {
		t.Fatalf("write uppercase rules: %v", err)
	}

	documents, err := loadProjectRules(context.Background(), nested)
	if err != nil {
		t.Fatalf("loadProjectRules() error = %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("expected only uppercase AGENTS.md to be loaded, got %+v", documents)
	}
	if filepath.Base(documents[0].Path) != projectRuleFileName {
		t.Fatalf("expected uppercase AGENTS.md match, got %q", documents[0].Path)
	}
	if strings.Contains(documents[0].Content, "wrong-case") {
		t.Fatalf("did not expect lowercase agents.md content to be loaded")
	}
}

func TestLoadRuleDocumentsReturnsReadError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, projectRuleFileName)
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

func TestDiscoverRuleFilesReturnsDirectoryReadError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	rootRules := filepath.Join(root, projectRuleFileName)
	localRules := filepath.Join(root, "a", projectRuleFileName)
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
	if err == nil || !strings.Contains(err.Error(), permissionErr.Error()) {
		t.Fatalf("expected discoverRuleFilesWithFinder() to return permission error, got %v", err)
	}
	if paths != nil {
		t.Fatalf("expected no paths on discovery failure, got %+v", paths)
	}
}

func TestProjectRulesSourceCachesAndInvalidatesByMTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rulesPath := filepath.Join(root, projectRuleFileName)
	if err := os.WriteFile(rulesPath, []byte("version-1"), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	loadCalls := 0
	source := &projectRulesSource{
		loadRules: func(ctx context.Context, workdir string) ([]ruleDocument, error) {
			loadCalls++
			return loadProjectRules(ctx, workdir)
		},
		statFile: os.Stat,
	}

	if _, err := source.Sections(context.Background(), BuildInput{Metadata: testMetadata(root)}); err != nil {
		t.Fatalf("first Sections() error = %v", err)
	}
	if _, err := source.Sections(context.Background(), BuildInput{Metadata: testMetadata(root)}); err != nil {
		t.Fatalf("second Sections() error = %v", err)
	}
	if loadCalls != 1 {
		t.Fatalf("expected cached rules on second call, got loadCalls=%d", loadCalls)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(rulesPath, []byte("version-2 with more content"), 0o644); err != nil {
		t.Fatalf("rewrite rules: %v", err)
	}
	if err := os.Chtimes(rulesPath, future, future); err != nil {
		t.Fatalf("chtimes rules: %v", err)
	}

	if _, err := source.Sections(context.Background(), BuildInput{Metadata: testMetadata(root)}); err != nil {
		t.Fatalf("third Sections() error = %v", err)
	}
	if loadCalls != 2 {
		t.Fatalf("expected cache invalidation after mtime change, got loadCalls=%d", loadCalls)
	}
}

func TestRenderProjectRulesSectionTruncatesSingleFileAndTotalBudget(t *testing.T) {
	t.Parallel()

	largeSingle := strings.Repeat("a", projectRulePerFileRuneLimit+32)
	largeTotalA := strings.Repeat("b", 7000)
	largeTotalB := strings.Repeat("c", 7000)

	section := renderPromptSection(renderProjectRulesSection([]ruleDocument{
		{Path: "/repo/AGENTS.md", Content: largeSingle[:projectRulePerFileRuneLimit], Truncated: true},
	}))
	if !strings.Contains(section, "[truncated to fit per-file limit]") {
		t.Fatalf("expected per-file truncation marker, got %q", section)
	}

	totalPromptSection := renderProjectRulesSection([]ruleDocument{
		{Path: "/repo/root/AGENTS.md", Content: largeTotalA},
		{Path: "/repo/root/app/AGENTS.md", Content: largeTotalB},
	})
	totalSection := renderPromptSection(totalPromptSection)
	if !strings.Contains(totalSection, "[additional project rules truncated to fit total limit]") {
		t.Fatalf("expected total truncation marker, got %q", totalSection)
	}
	if strings.Contains(totalSection, strings.Repeat("c", 6500)) {
		t.Fatalf("expected total rules section to be truncated")
	}
	if runeCount(totalPromptSection.content) > projectRuleTotalRuneLimit {
		t.Fatalf(
			"expected rendered rules body to respect total rune budget, got %d > %d",
			runeCount(totalPromptSection.content),
			projectRuleTotalRuneLimit,
		)
	}
}

func TestProjectRulesSourceReturnsCacheValidationError(t *testing.T) {
	t.Parallel()

	source := &projectRulesSource{
		cache: map[string]cachedRuleDocuments{
			normalizeRuleCacheKey("/workspace"): {
				snapshots: []ruleFileSnapshot{{
					Path:    "/workspace/AGENTS.md",
					ModTime: time.Unix(10, 0),
					Size:    12,
				}},
			},
		},
		statFile: func(path string) (os.FileInfo, error) {
			return nil, errors.New("stat boom")
		},
		loadRules: func(ctx context.Context, workdir string) ([]ruleDocument, error) {
			t.Fatalf("loadRules should not run when cache validation fails")
			return nil, nil
		},
	}

	_, err := source.loadCachedProjectRules(context.Background(), "/workspace")
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected cache validation error, got %v", err)
	}
}

func TestProjectRulesSourceInvalidatesCacheWhenRuleFileIsMissing(t *testing.T) {
	t.Parallel()

	source := &projectRulesSource{
		statFile: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}

	valid, err := source.isRuleCacheEntryValid(cachedRuleDocuments{
		snapshots: []ruleFileSnapshot{{
			Path:    "/workspace/AGENTS.md",
			ModTime: time.Unix(20, 0),
			Size:    8,
		}},
	})
	if err != nil {
		t.Fatalf("isRuleCacheEntryValid() error = %v", err)
	}
	if valid {
		t.Fatalf("expected missing rule file to invalidate cache")
	}
}

func TestDiscoverRuleFilesWithFinderStartsFromFileParentDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	filePath := filepath.Join(child, "main.go")
	if err := os.WriteFile(filePath, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rootRule := filepath.Join(root, projectRuleFileName)
	childRule := filepath.Join(child, projectRuleFileName)

	paths, err := discoverRuleFilesWithFinder(context.Background(), filePath, func(dir string) (string, error) {
		switch dir {
		case child:
			return childRule, nil
		case root:
			return rootRule, nil
		default:
			return "", nil
		}
	})
	if err != nil {
		t.Fatalf("discoverRuleFilesWithFinder() error = %v", err)
	}
	if len(paths) != 2 || paths[0] != rootRule || paths[1] != childRule {
		t.Fatalf("expected parent-to-child rule order, got %+v", paths)
	}
}

func TestFindExactRuleFileReturnsNoMatchForMissingDirectory(t *testing.T) {
	t.Parallel()

	got, err := findExactRuleFile(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("findExactRuleFile() error = %v", err)
	}
	if got != "" {
		t.Fatalf("expected no rule file, got %q", got)
	}
}
