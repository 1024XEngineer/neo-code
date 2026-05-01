package context

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/promptasset"
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
	if second[0].Title != promptasset.CoreSections()[0].Title {
		t.Fatalf("expected cloned sections, got %+v", second)
	}
}

func TestRulesPromptSourceSectionsSkipsWhenNoRulesExist(t *testing.T) {
	t.Parallel()

	sections, err := newRulesPromptSource(nil).Sections(context.Background(), BuildInput{
		Metadata: Metadata{Workdir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("expected no rules sections, got %+v", sections)
	}
}

func TestRulesPromptSourceSectionsRendersRules(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectRuleFileName), []byte("rule-body"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	sections, err := newRulesPromptSource(nil).Sections(context.Background(), BuildInput{
		Metadata: Metadata{ProjectRoot: root, Workdir: root},
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one rules section, got %+v", sections)
	}
	if got := renderPromptSection(sections[0]); got == "" {
		t.Fatalf("expected rendered rules section")
	}
	if got := renderPromptSection(sections[0]); !strings.Contains(got, "### Project Rules") {
		t.Fatalf("expected project rules block, got %q", got)
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
