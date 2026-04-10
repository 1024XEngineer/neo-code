package context

import (
	"strings"
	"testing"
)

func TestDefaultSystemPromptSectionsReturnsCachedSections(t *testing.T) {
	t.Parallel()

	sections := defaultSystemPromptSections()
	if len(sections) != len(defaultPromptSections) {
		t.Fatalf("expected %d default sections, got %d", len(defaultPromptSections), len(sections))
	}
	if len(sections) == 0 {
		t.Fatalf("expected non-empty default sections")
	}
	if sections[0].title != "Agent Identity" {
		t.Fatalf("expected first default section title, got %q", sections[0].title)
	}
}

func TestRenderPromptSectionBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		section promptSection
		want    string
	}{
		{
			name:    "empty title and content renders empty",
			section: promptSection{},
			want:    "",
		},
		{
			name: "content only renders content",
			section: promptSection{
				content: "content only",
			},
			want: "content only",
		},
		{
			name: "title only renders empty",
			section: promptSection{
				title: "Title Only",
			},
			want: "",
		},
		{
			name: "title and content render heading",
			section: promptSection{
				title:   "Section",
				content: "body",
			},
			want: "## Section\n\nbody",
		},
		{
			name: "title and content are trimmed before rendering",
			section: promptSection{
				title:   " Section ",
				content: "\nbody\n",
			},
			want: "## Section\n\nbody",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderPromptSection(tt.section)
			if got != tt.want {
				t.Fatalf("renderPromptSection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeSystemPromptSkipsEmptySections(t *testing.T) {
	t.Parallel()

	got := composeSystemPrompt(
		promptSection{},
		promptSection{content: "plain"},
		promptSection{title: "Title Only"},
		promptSection{title: "Section", content: "body"},
	)

	want := "plain\n\n## Section\n\nbody"
	if got != want {
		t.Fatalf("composeSystemPrompt() = %q, want %q", got, want)
	}
}

func TestDefaultToolUsagePromptEncouragesAskFlow(t *testing.T) {
	t.Parallel()

	sections := defaultSystemPromptSections()
	var toolUsage string
	for _, section := range sections {
		if section.title == "Tool Usage" {
			toolUsage = section.content
			break
		}
	}
	if toolUsage == "" {
		t.Fatalf("expected Tool Usage section to exist")
	}
	if !strings.Contains(toolUsage, "permission layer") {
		t.Fatalf("expected Tool Usage to mention permission layer, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "Do not self-reject") {
		t.Fatalf("expected Tool Usage to discourage self-reject, got %q", toolUsage)
	}
}
