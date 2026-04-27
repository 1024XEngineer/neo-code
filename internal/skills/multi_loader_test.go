package skills

import (
	"context"
	"errors"
	"testing"
)

type stubSnapshotLoader struct {
	snapshot Snapshot
	err      error
}

func (s stubSnapshotLoader) Load(context.Context) (Snapshot, error) {
	if s.err != nil {
		return Snapshot{}, s.err
	}
	return s.snapshot, nil
}

func TestMultiSourceLoaderProjectOverridesGlobal(t *testing.T) {
	t.Parallel()

	projectSkill := Skill{
		Descriptor: Descriptor{
			ID:    "go-review",
			Name:  "project",
			Scope: ScopeExplicit,
			Source: Source{
				Kind:  SourceKindLocal,
				Layer: SourceLayerProject,
			},
		},
		Content: Content{Instruction: "project"},
	}
	globalSkill := Skill{
		Descriptor: Descriptor{
			ID:    "go-review",
			Name:  "global",
			Scope: ScopeExplicit,
			Source: Source{
				Kind:  SourceKindLocal,
				Layer: SourceLayerGlobal,
			},
		},
		Content: Content{Instruction: "global"},
	}
	loader := NewMultiSourceLoader(
		stubSnapshotLoader{snapshot: Snapshot{Skills: []Skill{projectSkill}}},
		stubSnapshotLoader{snapshot: Snapshot{Skills: []Skill{globalSkill}}},
	)
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
	if got := snapshot.Skills[0].Content.Instruction; got != "project" {
		t.Fatalf("instruction = %q, want project", got)
	}
}

func TestMultiSourceLoaderPreservesSameSourceDuplicates(t *testing.T) {
	t.Parallel()

	loader := NewMultiSourceLoader(
		stubSnapshotLoader{snapshot: Snapshot{
			Skills: []Skill{
				{
					Descriptor: Descriptor{
						ID:    "dup",
						Name:  "p1",
						Scope: ScopeExplicit,
						Source: Source{
							Kind:  SourceKindLocal,
							Layer: SourceLayerProject,
						},
					},
					Content: Content{Instruction: "p1"},
				},
				{
					Descriptor: Descriptor{
						ID:    "dup",
						Name:  "p2",
						Scope: ScopeExplicit,
						Source: Source{
							Kind:  SourceKindLocal,
							Layer: SourceLayerProject,
						},
					},
					Content: Content{Instruction: "p2"},
				},
			},
		}},
		stubSnapshotLoader{snapshot: Snapshot{
			Skills: []Skill{
				{
					Descriptor: Descriptor{
						ID:    "dup",
						Name:  "global",
						Scope: ScopeExplicit,
						Source: Source{
							Kind:  SourceKindLocal,
							Layer: SourceLayerGlobal,
						},
					},
					Content: Content{Instruction: "global"},
				},
			},
		}},
	)

	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 2 {
		t.Fatalf("skills count = %d, want 2 (same source duplicates preserved)", len(snapshot.Skills))
	}
	for _, item := range snapshot.Skills {
		if item.Descriptor.Source.Layer != SourceLayerProject {
			t.Fatalf("expected project layer skills only, got %+v", snapshot.Skills)
		}
	}
}

func TestMultiSourceLoaderSkipsMissingRoots(t *testing.T) {
	t.Parallel()

	loader := NewMultiSourceLoader(
		stubSnapshotLoader{err: ErrSkillRootNotFound},
		stubSnapshotLoader{snapshot: Snapshot{
			Skills: []Skill{{
				Descriptor: Descriptor{
					ID:    "go-review",
					Name:  "global",
					Scope: ScopeExplicit,
					Source: Source{
						Kind:  SourceKindLocal,
						Layer: SourceLayerGlobal,
					},
				},
				Content: Content{Instruction: "global"},
			}},
		}},
	)
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("skills count = %d, want 1", len(snapshot.Skills))
	}
}

func TestMultiSourceLoaderReturnsErrorWhenAllRootsMissing(t *testing.T) {
	t.Parallel()

	loader := NewMultiSourceLoader(stubSnapshotLoader{err: ErrSkillRootNotFound})
	_, err := loader.Load(context.Background())
	if !errors.Is(err, ErrSkillRootNotFound) {
		t.Fatalf("expected ErrSkillRootNotFound, got %v", err)
	}
}

func TestMultiSourceLoaderReturnsNonRootError(t *testing.T) {
	t.Parallel()

	loader := NewMultiSourceLoader(
		stubSnapshotLoader{err: errors.New("boom")},
		stubSnapshotLoader{snapshot: Snapshot{
			Skills: []Skill{{
				Descriptor: Descriptor{
					ID:    "global-skill",
					Name:  "global",
					Scope: ScopeExplicit,
					Source: Source{
						Kind:  SourceKindLocal,
						Layer: SourceLayerGlobal,
					},
				},
				Content: Content{Instruction: "global"},
			}},
		}},
	)
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(snapshot.Skills) != 1 {
		t.Fatalf("skills count = %d, want 1", len(snapshot.Skills))
	}
	if len(snapshot.Issues) == 0 {
		t.Fatalf("expected refresh issue for broken source, got %+v", snapshot.Issues)
	}
	if snapshot.Issues[0].Code != IssueRefreshFailed {
		t.Fatalf("issue code = %q, want %q", snapshot.Issues[0].Code, IssueRefreshFailed)
	}
}

func TestMultiSourceLoaderReturnsIssuesWhenAllSourcesFail(t *testing.T) {
	t.Parallel()

	loader := NewMultiSourceLoader(stubSnapshotLoader{err: errors.New("boom")})
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil with issues", err)
	}
	if len(snapshot.Skills) != 0 {
		t.Fatalf("skills count = %d, want 0", len(snapshot.Skills))
	}
	if len(snapshot.Issues) != 1 || snapshot.Issues[0].Code != IssueRefreshFailed {
		t.Fatalf("issues = %+v, want one refresh failure", snapshot.Issues)
	}
}
