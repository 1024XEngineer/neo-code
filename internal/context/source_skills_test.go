package context

import (
	stdcontext "context"
	"errors"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/skills"
)

func TestDefaultBuilderBuildInjectsSkillsInStableOrder(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	result, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		ActiveSkills: []skills.Skill{
			{
				Descriptor: skills.Descriptor{ID: "zeta", Name: "Zeta"},
				Content:    skills.Content{Instruction: "second"},
			},
			{
				Descriptor: skills.Descriptor{ID: "go_review", Name: "Go Review"},
				Content: skills.Content{
					Instruction: "first",
					ToolHints:   []string{"read docs", "run tests", "inspect code", "open diff"},
					References: []skills.Reference{
						{Title: "Ref A", Summary: "summary-a"},
						{Title: "Ref B", Summary: "summary-b"},
						{Title: "Ref C", Summary: "summary-c"},
						{Title: "Ref D", Summary: "summary-d"},
					},
					Examples: []string{"example-1", "example-1", "example-2", "example-3"},
				},
			},
			{
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review Duplicate"},
				Content:    skills.Content{Instruction: "duplicate"},
			},
		},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if !strings.Contains(result.SystemPrompt, "## Skills") {
		t.Fatalf("expected skills section, got %q", result.SystemPrompt)
	}
	goReviewIndex := strings.Index(result.SystemPrompt, "go_review")
	if goReviewIndex < 0 {
		goReviewIndex = strings.Index(result.SystemPrompt, "go-review")
	}
	zetaIndex := strings.Index(result.SystemPrompt, "zeta")
	if goReviewIndex < 0 || zetaIndex < 0 || goReviewIndex > zetaIndex {
		t.Fatalf("expected normalized stable order, got %q", result.SystemPrompt)
	}
	if strings.Count(result.SystemPrompt, "- skill: Go Review") != 1 {
		t.Fatalf("expected duplicate skill injection to be deduplicated, got %q", result.SystemPrompt)
	}
	if strings.Contains(result.SystemPrompt, "summary-d") {
		t.Fatalf("expected references to be truncated, got %q", result.SystemPrompt)
	}
	if strings.Contains(result.SystemPrompt, "example-3") {
		t.Fatalf("expected examples to be truncated, got %q", result.SystemPrompt)
	}
	if strings.Contains(result.SystemPrompt, "open diff") {
		t.Fatalf("expected tool hints to be truncated, got %q", result.SystemPrompt)
	}
}

func TestDefaultBuilderBuildSkipsSkillsSectionWhenNoActiveSkills(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	result, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(result.SystemPrompt, "## Skills") {
		t.Fatalf("did not expect skills section without active skills, got %q", result.SystemPrompt)
	}
}

func TestSkillPromptSourceSectionsHonorsContextCancel(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()
	_, err := (skillPromptSource{}).Sections(canceledCtx, BuildInput{})
	if !errors.Is(err, stdcontext.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestNormalizeActiveSkillsDropsBlankIDs(t *testing.T) {
	t.Parallel()

	normalized := normalizeActiveSkills([]skills.Skill{
		{Descriptor: skills.Descriptor{ID: "  "}},
		{Descriptor: skills.Descriptor{ID: "go_review", Name: "Go Review"}},
	})
	if len(normalized) != 1 || normalized[0].Descriptor.ID != "go_review" {
		t.Fatalf("unexpected normalized skills: %+v", normalized)
	}
}

func TestTruncateSkillReferencesAndHelpers(t *testing.T) {
	t.Parallel()

	references := truncateSkillReferences([]skills.Reference{
		{Title: "A", Summary: "sum-a"},
		{Title: "TitleOnly"},
		{Summary: "SummaryOnly"},
		{Path: "/tmp/path-only"},
		{Title: "A", Summary: "sum-a"},
	}, 4)
	if len(references) != 4 {
		t.Fatalf("expected four rendered references, got %+v", references)
	}
	if references[0] != "A: sum-a" || references[1] != "TitleOnly" || references[2] != "SummaryOnly" || references[3] != "/tmp/path-only" {
		t.Fatalf("unexpected rendered references: %+v", references)
	}
	if got := truncateSkillReferences([]skills.Reference{{Title: "x"}}, 0); got != nil {
		t.Fatalf("expected nil references when limit <= 0, got %+v", got)
	}
	if got := min(3, 1); got != 1 {
		t.Fatalf("unexpected min result: %d", got)
	}
	if got := normalizeSkillID(" - "); got != "" {
		t.Fatalf("expected blank normalized skill id, got %q", got)
	}
}
