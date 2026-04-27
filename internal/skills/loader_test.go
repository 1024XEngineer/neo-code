package skills

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalLoaderLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(t *testing.T, root string)
		wantSkills    int
		wantIssueCode LoadIssueCode
	}{
		{
			name: "load success with frontmatter and sections",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "go-review", `---
id: go-review
name: Go Review
description: review go code
version: v1.2.0
scope: explicit
source: local
tool_hints:
  - filesystem_read_file
---
# Go Review
## Instruction
Review Go code with concrete findings.

## References
- [Go Spec](https://go.dev/ref/spec)

## Examples
- report critical bug with file path and line

## ToolHints
- filesystem_grep
`)
			},
			wantSkills: 1,
		},
		{
			name: "missing skill file creates issue but no fatal error",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "missing-skill"), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			wantSkills:    0,
			wantIssueCode: IssueSkillFileMissing,
		},
		{
			name: "invalid metadata creates issue and skips bad skill",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "bad-metadata", `---
id: bad-meta
scope: invalid_scope
---
# bad
## Instruction
test`)
			},
			wantSkills:    0,
			wantIssueCode: IssueInvalidMetadata,
		},
		{
			name: "empty content creates issue and skips bad skill",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "empty-content", `---
id: empty-content
scope: explicit
---
`)
			},
			wantSkills:    0,
			wantIssueCode: IssueEmptyContent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			tt.setup(t, root)

			loader := NewLocalLoader(root)
			snapshot, err := loader.Load(context.Background())
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(snapshot.Skills) != tt.wantSkills {
				t.Fatalf("skills count = %d, want %d", len(snapshot.Skills), tt.wantSkills)
			}
			if tt.wantIssueCode != "" {
				found := false
				for _, issue := range snapshot.Issues {
					if issue.Code == tt.wantIssueCode {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected issue code %q, got %+v", tt.wantIssueCode, snapshot.Issues)
				}
			}

			if len(snapshot.Skills) > 0 {
				skill := snapshot.Skills[0]
				if strings.TrimSpace(skill.Descriptor.ID) == "" {
					t.Fatalf("expected non-empty skill id")
				}
				if strings.TrimSpace(skill.Content.Instruction) == "" {
					t.Fatalf("expected non-empty instruction")
				}
				if skill.Descriptor.Source.Kind != SourceKindLocal {
					t.Fatalf("expected source kind local, got %q", skill.Descriptor.Source.Kind)
				}
			}
		})
	}
}

func TestLocalLoaderLoadBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		root      string
		prepare   func(t *testing.T, root string)
		expectErr string
	}{
		{
			name:      "empty root",
			root:      "",
			expectErr: "root directory not found",
		},
		{
			name:      "root not exists",
			root:      filepath.Join(t.TempDir(), "missing-root"),
			expectErr: "root directory not found",
		},
		{
			name: "root is file",
			root: filepath.Join(t.TempDir(), "root.txt"),
			prepare: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
			},
			expectErr: "not directory",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.prepare != nil {
				tt.prepare(t, tt.root)
			}
			loader := NewLocalLoader(tt.root)
			_, err := loader.Load(context.Background())
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.expectErr)) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestLocalLoaderLoadFrontmatterEOF(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "eof-frontmatter", `---
id: eof-frontmatter
name: EOF Frontmatter
---`)

	loader := NewLocalLoader(root)
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 0 {
		t.Fatalf("expected no loaded skill, got %d", len(snapshot.Skills))
	}

	var invalidMetaFound bool
	var emptyContentFound bool
	for _, issue := range snapshot.Issues {
		if issue.Code == IssueInvalidMetadata {
			invalidMetaFound = true
		}
		if issue.Code == IssueEmptyContent {
			emptyContentFound = true
		}
	}
	if invalidMetaFound {
		t.Fatalf("unexpected invalid metadata issue, got %+v", snapshot.Issues)
	}
	if !emptyContentFound {
		t.Fatalf("expected empty content issue, got %+v", snapshot.Issues)
	}
}

func TestLocalLoaderLoadFileConstraints(t *testing.T) {
	t.Parallel()

	t.Run("non regular file reports read issue", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeSkillFile(t, root, "good", "## Instruction\ngood")

		badDir := filepath.Join(root, "bad")
		if err := os.MkdirAll(filepath.Join(badDir, skillFileName), 0o755); err != nil {
			t.Fatalf("mkdir bad skill dir: %v", err)
		}

		loader := NewLocalLoader(root)
		snapshot, err := loader.Load(context.Background())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(snapshot.Skills) != 1 {
			t.Fatalf("expected 1 loaded skill, got %d", len(snapshot.Skills))
		}

		found := false
		for _, issue := range snapshot.Issues {
			if issue.Code == IssueReadFailed && strings.Contains(issue.Message, "not regular") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected non-regular read issue, got %+v", snapshot.Issues)
		}
	})

	t.Run("oversized file reports read issue", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeSkillFile(t, root, "good", "## Instruction\ngood")

		largeDir := filepath.Join(root, "large")
		if err := os.MkdirAll(largeDir, 0o755); err != nil {
			t.Fatalf("mkdir large dir: %v", err)
		}
		large := bytes.Repeat([]byte("a"), int(defaultMaxSkillFileBytes)+1)
		if err := os.WriteFile(filepath.Join(largeDir, skillFileName), large, 0o644); err != nil {
			t.Fatalf("write large SKILL.md: %v", err)
		}

		loader := NewLocalLoader(root)
		snapshot, err := loader.Load(context.Background())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(snapshot.Skills) != 1 {
			t.Fatalf("expected 1 loaded skill, got %d", len(snapshot.Skills))
		}

		found := false
		for _, issue := range snapshot.Issues {
			if issue.Code == IssueReadFailed && strings.Contains(issue.Message, "size limit") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected oversize read issue, got %+v", snapshot.Issues)
		}
	})
}

func TestLocalLoaderLoadReportsRootSkillStatError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "good", "## Instruction\ngood")

	loader := NewLocalLoader(root)
	originStat := loader.statPath
	rootSkillPath := filepath.Join(root, skillFileName)
	loader.statPath = func(path string) (os.FileInfo, error) {
		if path == rootSkillPath {
			return nil, os.ErrPermission
		}
		return originStat(path)
	}

	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(snapshot.Skills))
	}

	requireLoadIssue(t, snapshot, "root skill stat failure", func(issue LoadIssue) bool {
		return issue.Code == IssueReadFailed && issue.Path == rootSkillPath && errors.Is(issue.Err, os.ErrPermission)
	})
}

func TestLocalLoaderLoadEnforcesSizeLimitAtReadTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "good", "## Instruction\ngood")

	largeDir := filepath.Join(root, "large-race")
	if err := os.MkdirAll(largeDir, 0o755); err != nil {
		t.Fatalf("mkdir large dir: %v", err)
	}
	largePath := filepath.Join(largeDir, skillFileName)
	large := bytes.Repeat([]byte("a"), int(defaultMaxSkillFileBytes)+1)
	if err := os.WriteFile(largePath, large, 0o644); err != nil {
		t.Fatalf("write large SKILL.md: %v", err)
	}

	loader := NewLocalLoader(root)
	originLstat := loader.lstatPath
	loader.lstatPath = func(path string) (os.FileInfo, error) {
		if path == largePath {
			return fixedRegularFileInfo{name: skillFileName, size: 1}, nil
		}
		return originLstat(path)
	}

	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(snapshot.Skills))
	}

	requireLoadIssue(t, snapshot, "enforced read limit", func(issue LoadIssue) bool {
		return issue.Code == IssueReadFailed && issue.Path == largePath && strings.Contains(issue.Message, "size limit")
	})
}

func TestParseLocalSkillNameFallbackUsesOnlyLevelOneHeading(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "heading-fallback")
	skillPath := filepath.Join(skillDir, skillFileName)
	raw := `---
id: heading-fallback
---
## References
- [Doc](./doc.md)
`

	skill, issues, err := parseLocalSkill(root, skillDir, skillPath, raw)
	if err != nil {
		t.Fatalf("parseLocalSkill() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
	if skill.Descriptor.Name != skill.Descriptor.ID {
		t.Fatalf("expected name fallback to id, got name=%q id=%q", skill.Descriptor.Name, skill.Descriptor.ID)
	}
}

func TestLocalLoaderWithSourceLayer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "go-review", `---
id: go-review
name: Go Review
---
## Instruction
review`)

	loader := NewLocalLoaderWithSourceLayer(root, SourceLayerProject)
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("skills count = %d, want 1", len(snapshot.Skills))
	}
	if got := snapshot.Skills[0].Descriptor.Source.Layer; got != SourceLayerProject {
		t.Fatalf("source layer = %q, want %q", got, SourceLayerProject)
	}
}

func requireLoadIssue(t *testing.T, snapshot Snapshot, desc string, match func(LoadIssue) bool) {
	t.Helper()
	for _, issue := range snapshot.Issues {
		if match(issue) {
			return
		}
	}
	t.Fatalf("%s: expected matching issue, got %+v", desc, snapshot.Issues)
}

func writeSkillFile(t *testing.T, root string, dir string, content string) {
	t.Helper()
	skillDir := filepath.Join(root, dir)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

type fixedRegularFileInfo struct {
	name string
	size int64
}

func (f fixedRegularFileInfo) Name() string       { return f.name }
func (f fixedRegularFileInfo) Size() int64        { return f.size }
func (f fixedRegularFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fixedRegularFileInfo) ModTime() time.Time { return time.Time{} }
func (f fixedRegularFileInfo) IsDir() bool        { return false }
func (f fixedRegularFileInfo) Sys() any           { return nil }
