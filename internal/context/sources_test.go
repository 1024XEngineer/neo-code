package context

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCorePromptSourceSectionsReturnsClone(t *testing.T) {
	t.Parallel()

	source := corePromptSource{}
	first, err := source.Sections(context.Background(), BuildInput{})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("expected non-empty core prompt sections")
	}

	first[0].Title = "changed"

	second, err := source.Sections(context.Background(), BuildInput{})
	if err != nil {
		t.Fatalf("Sections() second call error = %v", err)
	}
	if second[0].Title != defaultPromptSections[0].Title {
		t.Fatalf("expected cloned sections, got %+v", second)
	}
}

func TestProjectRulesSourceSectionsSkipsWhenNoRulesExist(t *testing.T) {
	t.Parallel()

	sections, err := (&projectRulesSource{}).Sections(context.Background(), BuildInput{
		Metadata: Metadata{Workdir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("expected no project rule sections, got %+v", sections)
	}
}

func TestProjectRulesSourceSectionsRendersRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectRuleFileName), []byte("rule-body"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	sections, err := (&projectRulesSource{}).Sections(context.Background(), BuildInput{
		Metadata: Metadata{Workdir: root},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one project rule section, got %+v", sections)
	}
	if got := renderPromptSection(sections[0]); got == "" {
		t.Fatalf("expected rendered project rule section")
	}
}

func TestCorePromptSourceSectionsHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (corePromptSource{}).Sections(ctx, BuildInput{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
