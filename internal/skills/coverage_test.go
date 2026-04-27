package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceKindValidateCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    SourceKind
		wantErr bool
	}{
		{name: "local", kind: SourceKindLocal},
		{name: "builtin", kind: SourceKindBuiltin},
		{name: "invalid", kind: SourceKind("x"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.kind.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for kind=%q", tt.kind)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for kind=%q: %v", tt.kind, err)
			}
		})
	}
}

func TestActivationScopeValidateCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   ActivationScope
		wantErr bool
	}{
		{name: "global", scope: ScopeGlobal},
		{name: "workspace", scope: ScopeWorkspace},
		{name: "session", scope: ScopeSession},
		{name: "explicit", scope: ScopeExplicit},
		{name: "invalid", scope: ActivationScope("x"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.scope.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for scope=%q", tt.scope)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for scope=%q: %v", tt.scope, err)
			}
		})
	}
}

func TestSourceLayerValidateCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		layer   SourceLayer
		wantErr bool
	}{
		{name: "empty", layer: ""},
		{name: "global", layer: SourceLayerGlobal},
		{name: "project", layer: SourceLayerProject},
		{name: "builtin", layer: SourceLayerBuiltin},
		{name: "invalid", layer: SourceLayer("x"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.layer.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for layer=%q", tt.layer)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for layer=%q: %v", tt.layer, err)
			}
		})
	}
}

func TestDescriptorValidateCases(t *testing.T) {
	t.Parallel()

	base := Descriptor{
		ID:   "go-review",
		Name: "Go Review",
		Source: Source{
			Kind: SourceKindLocal,
		},
		Scope: ScopeExplicit,
	}

	tests := []struct {
		name      string
		mutate    func(d Descriptor) Descriptor
		wantError string
	}{
		{
			name: "valid",
			mutate: func(d Descriptor) Descriptor {
				return d
			},
		},
		{
			name: "empty id",
			mutate: func(d Descriptor) Descriptor {
				d.ID = " "
				return d
			},
			wantError: "id is empty",
		},
		{
			name: "empty name",
			mutate: func(d Descriptor) Descriptor {
				d.Name = ""
				return d
			},
			wantError: "name is empty",
		},
		{
			name: "invalid source kind",
			mutate: func(d Descriptor) Descriptor {
				d.Source.Kind = SourceKind("unknown")
				return d
			},
			wantError: "invalid source kind",
		},
		{
			name: "invalid source layer",
			mutate: func(d Descriptor) Descriptor {
				d.Source.Layer = SourceLayer("unknown")
				return d
			},
			wantError: "invalid source layer",
		},
		{
			name: "invalid scope",
			mutate: func(d Descriptor) Descriptor {
				d.Scope = ActivationScope("unknown")
				return d
			},
			wantError: "invalid activation scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := tt.mutate(base)
			err := d.Validate()
			if tt.wantError == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantError != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantError)) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
			}
		})
	}
}

func TestContentValidateCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content Content
		wantErr bool
	}{
		{
			name: "ok",
			content: Content{
				Instruction: "review code",
			},
		},
		{
			name: "empty instruction",
			content: Content{
				Instruction: " ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.content.Validate()
			if tt.wantErr && !errors.Is(err, ErrEmptyContent) {
				t.Fatalf("expected ErrEmptyContent, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadIssueErrorCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		issue LoadIssue
		want  string
	}{
		{
			name: "message and path",
			issue: LoadIssue{
				Code:    IssueInvalidMetadata,
				Path:    "/tmp/skill/SKILL.md",
				Message: "bad metadata",
			},
			want: "skills: bad metadata (/tmp/skill/SKILL.md)",
		},
		{
			name: "fallback to err",
			issue: LoadIssue{
				Code: IssueReadFailed,
				Err:  errors.New("permission denied"),
			},
			want: "skills: permission denied",
		},
		{
			name: "fallback to code",
			issue: LoadIssue{
				Code: IssueDuplicateID,
			},
			want: "skills: duplicate_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.issue.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeWorkspaceCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workspace string
		want      string
	}{
		{name: "empty", workspace: "   ", want: ""},
		{
			name:      "cleaned",
			workspace: filepath.Join("a", "b", "..", "c"),
			want:      filepath.Clean(filepath.Join("a", "b", "..", "c")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeWorkspace(tt.workspace); got != tt.want {
				t.Fatalf("normalizeWorkspace(%q) = %q, want %q", tt.workspace, got, tt.want)
			}
		})
	}
}

func TestFilterCases(t *testing.T) {
	t.Parallel()

	descriptor := Descriptor{
		ID:   "go-review",
		Name: "Go Review",
		Source: Source{
			Kind: SourceKindLocal,
		},
		Scope: ScopeWorkspace,
	}

	tests := []struct {
		name       string
		input      ListInput
		wantSource bool
		wantScope  bool
	}{
		{
			name:       "default all source but workspace requires workspace path",
			input:      ListInput{},
			wantSource: true,
			wantScope:  false,
		},
		{
			name: "source miss and scope hit",
			input: ListInput{
				SourceKinds: []SourceKind{SourceKindBuiltin},
				Workspace:   "repo",
			},
			wantSource: false,
			wantScope:  true,
		},
		{
			name: "source and scope explicit filters",
			input: ListInput{
				SourceKinds: []SourceKind{SourceKindLocal},
				Scopes:      []ActivationScope{ScopeWorkspace},
			},
			wantSource: true,
			wantScope:  true,
		},
		{
			name: "scope explicit mismatch",
			input: ListInput{
				Scopes: []ActivationScope{ScopeGlobal},
			},
			wantSource: true,
			wantScope:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := allowBySource(tt.input, descriptor); got != tt.wantSource {
				t.Fatalf("allowBySource() = %v, want %v", got, tt.wantSource)
			}
			if got := allowByScope(tt.input, descriptor); got != tt.wantScope {
				t.Fatalf("allowByScope() = %v, want %v", got, tt.wantScope)
			}
		})
	}
}

func TestLocalLoaderAdditionalBranches(t *testing.T) {
	t.Parallel()

	t.Run("context canceled before load", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewLocalLoader(root).Load(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("root skill loaded and unreadable skill file reported", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		// root-level skill should be discoverable.
		if err := os.WriteFile(filepath.Join(root, skillFileName), []byte("## Instruction\nroot skill"), 0o644); err != nil {
			t.Fatalf("write root skill: %v", err)
		}

		// Make one candidate unreadable by turning SKILL.md into a directory.
		badDir := filepath.Join(root, "bad")
		if err := os.MkdirAll(filepath.Join(badDir, skillFileName), 0o755); err != nil {
			t.Fatalf("mkdir bad skill dir: %v", err)
		}

		snapshot, err := NewLocalLoader(root).Load(context.Background())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(snapshot.Skills) != 1 {
			t.Fatalf("expected 1 loaded skill, got %d", len(snapshot.Skills))
		}
		foundReadFail := false
		for _, issue := range snapshot.Issues {
			if issue.Code == IssueReadFailed {
				foundReadFail = true
				break
			}
		}
		if !foundReadFail {
			t.Fatalf("expected IssueReadFailed in %+v", snapshot.Issues)
		}
	})

	t.Run("load reports abs error via hook", func(t *testing.T) {
		t.Parallel()
		loader := NewLocalLoader("x")
		loader.absPath = func(string) (string, error) {
			return "", errors.New("abs failed")
		}
		_, err := loader.Load(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "resolve root") {
			t.Fatalf("expected resolve root error, got %v", err)
		}
	})

	t.Run("load reports stat error via hook", func(t *testing.T) {
		t.Parallel()
		loader := NewLocalLoader("x")
		loader.absPath = func(string) (string, error) {
			return "x", nil
		}
		loader.statPath = func(string) (os.FileInfo, error) {
			return nil, errors.New("access denied")
		}
		_, err := loader.Load(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "stat root") {
			t.Fatalf("expected stat root error, got %v", err)
		}
	})

	t.Run("load reports read root error via hook", func(t *testing.T) {
		t.Parallel()
		loader := NewLocalLoader("x")
		loader.absPath = func(string) (string, error) {
			return "x", nil
		}
		loader.statPath = func(string) (os.FileInfo, error) {
			return fakeDirInfo{name: "x"}, nil
		}
		loader.readDir = func(string) ([]os.DirEntry, error) {
			return nil, errors.New("readdir denied")
		}
		_, err := loader.Load(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "read root") {
			t.Fatalf("expected read root error, got %v", err)
		}
	})

	t.Run("load exits when context canceled inside candidate loop", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		loader := NewLocalLoader(root)
		ctx := &errAfterNCallsContext{cancelAt: 2}
		_, err := loader.Load(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("load handles hidden skill directory and nil hooks fallback", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".hidden"), 0o755); err != nil {
			t.Fatalf("mkdir hidden: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "visible"), 0o755); err != nil {
			t.Fatalf("mkdir visible: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "visible", skillFileName), []byte("## Instruction\nvisible"), 0o644); err != nil {
			t.Fatalf("write visible skill: %v", err)
		}

		loader := &LocalLoader{root: root}
		snapshot, err := loader.Load(context.Background())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(snapshot.Skills) != 1 {
			t.Fatalf("expected only visible skill loaded, got %d", len(snapshot.Skills))
		}
	})
}

func TestParseLocalSkillCases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "Go_Review")
	skillPath := filepath.Join(skillDir, skillFileName)

	tests := []struct {
		name          string
		raw           string
		wantErr       string
		wantIssueCode LoadIssueCode
		assertSkill   func(t *testing.T, skill Skill)
	}{
		{
			name: "invalid frontmatter",
			raw: `---
id: a
`,
			wantErr:       "frontmatter end marker",
			wantIssueCode: IssueInvalidMetadata,
		},
		{
			name: "invalid yaml frontmatter",
			raw: `---
id: [a
---
## Instruction
do it`,
			wantErr:       "did not find expected",
			wantIssueCode: IssueInvalidMetadata,
		},
		{
			name: "invalid scope",
			raw: `---
id: a
scope: bad
---
## Instruction
do it`,
			wantErr:       "invalid activation scope",
			wantIssueCode: IssueInvalidMetadata,
		},
		{
			name: "invalid source kind",
			raw: `---
id: a
source: cloud
---
## Instruction
do it`,
			wantErr:       "invalid source kind",
			wantIssueCode: IssueInvalidMetadata,
		},
		{
			name: "unsupported source",
			raw: `---
id: a
source: builtin
---
## Instruction
do it`,
			wantErr:       "only supports source=local",
			wantIssueCode: IssueUnsupportedSource,
		},
		{
			name: "defaults and helpers",
			raw: `This is plain body instruction.

## references
- [Spec](https://go.dev/ref/spec)
- ./README.md

## examples
- first
- second

## tool_hints
- filesystem_read_file
- bash
`,
			assertSkill: func(t *testing.T, skill Skill) {
				t.Helper()
				if skill.Descriptor.ID != "go-review" {
					t.Fatalf("unexpected id: %q", skill.Descriptor.ID)
				}
				if skill.Descriptor.Name != "go-review" {
					t.Fatalf("unexpected name from first heading: %q", skill.Descriptor.Name)
				}
				if skill.Descriptor.Version != "v1" {
					t.Fatalf("unexpected default version: %q", skill.Descriptor.Version)
				}
				if skill.Descriptor.Scope != ScopeExplicit {
					t.Fatalf("unexpected default scope: %q", skill.Descriptor.Scope)
				}
				if skill.Descriptor.Source.Kind != SourceKindLocal {
					t.Fatalf("unexpected source kind: %q", skill.Descriptor.Source.Kind)
				}
				if len(skill.Content.References) != 2 {
					t.Fatalf("unexpected references: %+v", skill.Content.References)
				}
				if len(skill.Content.Examples) != 2 {
					t.Fatalf("unexpected examples: %+v", skill.Content.Examples)
				}
				if len(skill.Content.ToolHints) != 2 {
					t.Fatalf("unexpected tool hints: %+v", skill.Content.ToolHints)
				}
			},
		},
		{
			name: "name falls back to id without heading",
			raw: `---
id: no-heading-name
---
just plain instruction without heading`,
			assertSkill: func(t *testing.T, skill Skill) {
				t.Helper()
				if skill.Descriptor.Name != "no-heading-name" {
					t.Fatalf("expected name fallback to id, got %q", skill.Descriptor.Name)
				}
			},
		},
		{
			name: "heading used as name",
			raw: `---
id: heading-name
---
# Heading Name
## Instruction
Do review`,
			assertSkill: func(t *testing.T, skill Skill) {
				t.Helper()
				if skill.Descriptor.Name != "Heading Name" {
					t.Fatalf("expected heading as name, got %q", skill.Descriptor.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skill, issues, err := parseLocalSkill(root, skillDir, skillPath, tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
					t.Fatalf("expected parse error containing %q, got %v", tt.wantErr, err)
				}
				if len(issues) == 0 || issues[0].Code != tt.wantIssueCode {
					t.Fatalf("expected issue code %q, got %+v", tt.wantIssueCode, issues)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %+v", issues)
			}
			if tt.assertSkill != nil {
				tt.assertSkill(t, skill)
			}
		})
	}
}

func TestParseLocalSkillDescriptorValidationFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "good")
	skillPath := filepath.Join(skillDir, skillFileName)
	raw := `---
id: good
name: Good
---
## Instruction
do it`

	_, issues, err := parseLocalSkillWithValidator(
		root,
		skillDir,
		skillPath,
		raw,
		"",
		func(Descriptor) error { return errors.New("descriptor broken") },
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "descriptor broken") {
		t.Fatalf("expected descriptor validation error, got %v", err)
	}
	if len(issues) == 0 || issues[0].Code != IssueInvalidMetadata {
		t.Fatalf("expected invalid metadata issue, got %+v", issues)
	}
}

func TestSplitFrontMatterCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantMeta  string
		wantBody  string
		wantHas   bool
		wantError string
	}{
		{
			name:     "no frontmatter",
			raw:      "plain text",
			wantHas:  false,
			wantBody: "plain text",
		},
		{
			name: "valid frontmatter",
			raw: `---
id: a
---
hello`,
			wantMeta: "id: a",
			wantBody: "hello",
			wantHas:  true,
		},
		{
			name: "missing end marker",
			raw: `---
id: a`,
			wantError: "end marker not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			meta, body, has, err := splitFrontMatter(tt.raw)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantError)) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if has != tt.wantHas {
				t.Fatalf("has = %v, want %v", has, tt.wantHas)
			}
			if meta != tt.wantMeta {
				t.Fatalf("meta = %q, want %q", meta, tt.wantMeta)
			}
			if body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestLoaderHelperFunctions(t *testing.T) {
	t.Parallel()

	if got := deriveSkillID("___"); got != "skill" {
		t.Fatalf("deriveSkillID fallback = %q, want skill", got)
	}
	if got := deriveSkillID(filepath.Join("tmp", "Go Review")); got != "go-review" {
		t.Fatalf("deriveSkillID normalize = %q, want go-review", got)
	}

	if got := firstHeading("no heading"); got != "" {
		t.Fatalf("firstHeading = %q, want empty", got)
	}
	if got := firstHeading("# My Heading\ncontent"); got != "My Heading" {
		t.Fatalf("firstHeading = %q, want My Heading", got)
	}
	if got := firstHeading("## Section Heading\ncontent"); got != "" {
		t.Fatalf("firstHeading(level2) = %q, want empty", got)
	}

	if got := normalizedHeading("## Tool-Hints "); got != "tool-hints" {
		t.Fatalf("normalizedHeading = %q", got)
	}
	if got := normalizedHeading("plain text"); got != "" {
		t.Fatalf("normalizedHeading for plain text = %q, want empty", got)
	}

	refs := parseReferences("- [Doc](./doc.md)\n- ./raw.txt")
	if len(refs) != 2 || refs[0].Title != "Doc" || refs[1].Path != "./raw.txt" {
		t.Fatalf("parseReferences unexpected result: %+v", refs)
	}

	items := parseListItems("- one\n* two\n\n")
	if len(items) != 2 || items[0] != "one" || items[1] != "two" {
		t.Fatalf("parseListItems unexpected result: %+v", items)
	}

	merged := mergeToolHints(
		[]string{"bash", "BASH", "filesystem_read_file", " "},
		[]string{"filesystem_read_file", "webfetch"},
	)
	if len(merged) != 3 {
		t.Fatalf("mergeToolHints unexpected result: %+v", merged)
	}
}

func TestParseSectionsAliasesAndDefaultBranch(t *testing.T) {
	t.Parallel()

	sections := parseSections(`## Reference
- [Doc](./doc.md)
## Example
- e1
## Tool-Hints
- bash
## Unknown
go back to instruction`)

	if !strings.Contains(sections["references"], "[Doc](./doc.md)") {
		t.Fatalf("references section unexpected: %q", sections["references"])
	}
	if !strings.Contains(sections["examples"], "e1") {
		t.Fatalf("examples section unexpected: %q", sections["examples"])
	}
	if !strings.Contains(sections["toolhints"], "bash") {
		t.Fatalf("toolhints section unexpected: %q", sections["toolhints"])
	}
	if !strings.Contains(sections["instruction"], "go back to instruction") {
		t.Fatalf("instruction section unexpected: %q", sections["instruction"])
	}
}

type staticLoader struct {
	snapshot Snapshot
	err      error
}

func (l staticLoader) Load(ctx context.Context) (Snapshot, error) {
	return l.snapshot, l.err
}

func TestMemoryRegistryBoundaryCases(t *testing.T) {
	t.Parallel()

	t.Run("nil registry methods", func(t *testing.T) {
		t.Parallel()
		var r *MemoryRegistry
		if issues := r.Issues(); issues != nil {
			t.Fatalf("expected nil issues, got %+v", issues)
		}
		err := r.Refresh(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "registry is nil") {
			t.Fatalf("expected nil registry error, got %v", err)
		}
	})

	t.Run("nil loader", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(nil)
		err := r.Refresh(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "loader is nil") {
			t.Fatalf("expected nil loader error, got %v", err)
		}
	})

	t.Run("refresh canceled by context", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := r.Refresh(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("list/get with canceled context", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{
			snapshot: Snapshot{
				Skills: []Skill{{
					Descriptor: Descriptor{
						ID:   "go-review",
						Name: "Go Review",
						Source: Source{
							Kind: SourceKindLocal,
						},
						Scope: ScopeExplicit,
					},
					Content: Content{Instruction: "test"},
				}},
			},
		})
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.List(ctx, ListInput{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled from List, got %v", err)
		}
		if _, _, err := r.Get(ctx, "go-review"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled from Get, got %v", err)
		}
	})

	t.Run("get empty id", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{
			snapshot: Snapshot{
				Skills: []Skill{{
					Descriptor: Descriptor{
						ID:   "go-review",
						Name: "Go Review",
						Source: Source{
							Kind: SourceKindLocal,
						},
						Scope: ScopeExplicit,
					},
					Content: Content{Instruction: "test"},
				}},
			},
		})
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		_, _, err := r.Get(context.Background(), " ")
		if err == nil || !errors.Is(err, ErrSkillNotFound) {
			t.Fatalf("expected ErrSkillNotFound for empty id, got %v", err)
		}
	})

	t.Run("refresh records invalid normalized id issue", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{
			snapshot: Snapshot{
				Skills: []Skill{
					{
						Descriptor: Descriptor{
							ID:   "---",
							Name: "Bad",
							Source: Source{
								Kind: SourceKindLocal,
							},
							Scope: ScopeExplicit,
						},
						Content: Content{Instruction: "bad"},
					},
				},
			},
		})
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		issues := r.Issues()
		if len(issues) == 0 || issues[0].Code != IssueInvalidMetadata {
			t.Fatalf("expected invalid metadata issue, got %+v", issues)
		}
	})

	t.Run("list and get return ensureLoaded error", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(failingLoader{})
		if _, err := r.List(context.Background(), ListInput{}); err == nil {
			t.Fatalf("expected list error")
		}
		r2 := NewRegistry(failingLoader{})
		if _, _, err := r2.Get(context.Background(), "x"); err == nil {
			t.Fatalf("expected get error")
		}
	})

	t.Run("ensureLoaded returns nil when already loaded", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{
			snapshot: Snapshot{
				Skills: []Skill{{
					Descriptor: Descriptor{
						ID:   "x",
						Name: "X",
						Source: Source{
							Kind: SourceKindLocal,
						},
						Scope: ScopeExplicit,
					},
					Content: Content{Instruction: "x"},
				}},
			},
		})
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if err := r.ensureLoaded(context.Background()); err != nil {
			t.Fatalf("ensureLoaded: %v", err)
		}
	})

	t.Run("list skips by source filter", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(staticLoader{
			snapshot: Snapshot{
				Skills: []Skill{{
					Descriptor: Descriptor{
						ID:   "x",
						Name: "X",
						Source: Source{
							Kind: SourceKindLocal,
						},
						Scope: ScopeExplicit,
					},
					Content: Content{Instruction: "x"},
				}},
			},
		})
		list, err := r.List(context.Background(), ListInput{
			SourceKinds: []SourceKind{SourceKindBuiltin},
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("expected source filtered empty list, got %d", len(list))
		}
	})
}

type fakeDirInfo struct {
	name string
}

func (f fakeDirInfo) Name() string       { return f.name }
func (f fakeDirInfo) Size() int64        { return 0 }
func (f fakeDirInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (f fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (f fakeDirInfo) IsDir() bool        { return true }
func (f fakeDirInfo) Sys() any           { return nil }

type errAfterNCallsContext struct {
	calls    int
	cancelAt int
}

func (c *errAfterNCallsContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *errAfterNCallsContext) Done() <-chan struct{}       { return nil }
func (c *errAfterNCallsContext) Value(any) any               { return nil }
func (c *errAfterNCallsContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}
