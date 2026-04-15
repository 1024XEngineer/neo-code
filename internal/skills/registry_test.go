package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryRegistryRefreshAndQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(t *testing.T, root string)
		wantList      int
		wantIssueCode LoadIssueCode
		getID         string
		expectGetErr  string
	}{
		{
			name: "refresh success and get by id",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "go-review", `---
id: go-review
name: Go Review
scope: explicit
---
## Instruction
Review code strictly.`)
			},
			wantList: 1,
			getID:    "go-review",
		},
		{
			name: "duplicate id produces issue but keeps one skill",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "skill-a", `---
id: duplicate-id
name: A
scope: explicit
---
## Instruction
A`)
				writeSkillFile(t, root, "skill-b", `---
id: duplicate-id
name: B
scope: explicit
---
## Instruction
B`)
			},
			wantList:      1,
			wantIssueCode: IssueDuplicateID,
			getID:         "duplicate-id",
		},
		{
			name: "get missing skill returns not found",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeSkillFile(t, root, "present", `---
id: present
name: Present
scope: explicit
---
## Instruction
present`)
			},
			wantList:     1,
			getID:        "missing-id",
			expectGetErr: "skill not found",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			tt.setup(t, root)

			registry := NewRegistry(NewLocalLoader(root))
			if err := registry.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}

			gotList, err := registry.List(context.Background(), ListInput{})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(gotList) != tt.wantList {
				t.Fatalf("List() count = %d, want %d", len(gotList), tt.wantList)
			}

			if tt.wantIssueCode != "" {
				found := false
				for _, issue := range registry.Issues() {
					if issue.Code == tt.wantIssueCode {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected issue code %q, got %+v", tt.wantIssueCode, registry.Issues())
				}
			}

			_, _, getErr := registry.Get(context.Background(), tt.getID)
			if tt.expectGetErr != "" {
				if getErr == nil || !strings.Contains(strings.ToLower(getErr.Error()), strings.ToLower(tt.expectGetErr)) {
					t.Fatalf("expected get error containing %q, got %v", tt.expectGetErr, getErr)
				}
				return
			}
			if getErr != nil {
				t.Fatalf("unexpected Get() error: %v", getErr)
			}
		})
	}
}

func TestMemoryRegistryRefreshUpdatesResult(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "skill-a", `---
id: skill-a
name: Skill A
scope: explicit
---
## Instruction
skill a`)

	registry := NewRegistry(NewLocalLoader(root))
	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}

	listBefore, err := registry.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("List() before refresh error = %v", err)
	}
	if len(listBefore) != 1 {
		t.Fatalf("expected 1 skill before refresh, got %d", len(listBefore))
	}

	writeSkillFile(t, root, "skill-b", `---
id: skill-b
name: Skill B
scope: explicit
---
## Instruction
skill b`)

	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	listAfter, err := registry.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("List() after refresh error = %v", err)
	}
	if len(listAfter) != 2 {
		t.Fatalf("expected 2 skills after refresh, got %d", len(listAfter))
	}
}

func TestMemoryRegistryListFilters(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "global", `---
id: global-skill
name: Global
scope: global
---
## Instruction
global`)
	writeSkillFile(t, root, "workspace", `---
id: workspace-skill
name: Workspace
scope: workspace
---
## Instruction
workspace`)

	registry := NewRegistry(NewLocalLoader(root))
	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	tests := []struct {
		name      string
		input     ListInput
		wantCount int
	}{
		{
			name:      "default list hides workspace scope without workspace context",
			input:     ListInput{},
			wantCount: 1,
		},
		{
			name: "workspace context includes workspace scope",
			input: ListInput{
				Workspace: filepath.Join(root, "repo"),
			},
			wantCount: 2,
		},
		{
			name: "scope filter returns explicit scope subset",
			input: ListInput{
				Scopes: []ActivationScope{ScopeWorkspace},
			},
			wantCount: 1,
		},
		{
			name: "source filter local",
			input: ListInput{
				SourceKinds: []SourceKind{SourceKindLocal},
				Workspace:   filepath.Join(root, "repo"),
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			list, err := registry.List(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(list) != tt.wantCount {
				t.Fatalf("List() count = %d, want %d", len(list), tt.wantCount)
			}
		})
	}
}

type failingLoader struct{}

func (failingLoader) Load(ctx context.Context) (Snapshot, error) {
	return Snapshot{}, os.ErrPermission
}

type flakyLoader struct {
	calls int
}

func (l *flakyLoader) Load(ctx context.Context) (Snapshot, error) {
	l.calls++
	if l.calls == 1 {
		return Snapshot{}, os.ErrPermission
	}
	return Snapshot{
		Skills: []Skill{{
			Descriptor: Descriptor{
				ID:   "recovered",
				Name: "Recovered",
				Source: Source{
					Kind: SourceKindLocal,
				},
				Scope: ScopeExplicit,
			},
			Content: Content{
				Instruction: "ok",
			},
		}},
	}, nil
}

func TestMemoryRegistryRefreshFailure(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(failingLoader{})
	err := registry.Refresh(context.Background())
	if err == nil {
		t.Fatalf("expected refresh error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "permission") {
		t.Fatalf("expected permission error, got %v", err)
	}

	issues := registry.Issues()
	if len(issues) == 0 || issues[0].Code != IssueRefreshFailed {
		t.Fatalf("expected refresh failed issue, got %+v", issues)
	}
}

func TestMemoryRegistryTreatsMissingRootAsEmptyCatalog(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing-skills-root")
	registry := NewRegistry(NewLocalLoader(root))

	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	list, err := registry.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
	if len(registry.Issues()) != 0 {
		t.Fatalf("expected no issues for missing root, got %+v", registry.Issues())
	}

	_, _, err = registry.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestMemoryRegistryEnsureLoadedRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	loader := &flakyLoader{}
	registry := NewRegistry(loader)

	if _, err := registry.List(context.Background(), ListInput{}); err == nil {
		t.Fatalf("expected first list error")
	}
	if loader.calls != 1 {
		t.Fatalf("expected 1 load call after first list, got %d", loader.calls)
	}

	list, err := registry.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("expected second list success, got %v", err)
	}
	if loader.calls != 2 {
		t.Fatalf("expected 2 load calls after retry, got %d", loader.calls)
	}
	if len(list) != 1 || list[0].ID != "recovered" {
		t.Fatalf("unexpected list after retry: %+v", list)
	}
}

func TestNormalizeSkillID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "  Go_Review  ", want: "go-review"},
		{input: "skill id", want: "skill-id"},
		{input: "---", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeSkillID(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeSkillID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
