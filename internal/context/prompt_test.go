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
	if sections[0].Title != "Agent Identity" {
		t.Fatalf("expected first default section title, got %q", sections[0].Title)
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
				Content: "content only",
			},
			want: "content only",
		},
		{
			name: "title only renders empty",
			section: promptSection{
				Title: "Title Only",
			},
			want: "",
		},
		{
			name: "title and content render heading",
			section: promptSection{
				Title:   "Section",
				Content: "body",
			},
			want: "## Section\n\nbody",
		},
		{
			name: "title and content are trimmed before rendering",
			section: promptSection{
				Title:   " Section ",
				Content: "\nbody\n",
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
		promptSection{Content: "plain"},
		promptSection{Title: "Title Only"},
		promptSection{Title: "Section", Content: "body"},
	)

	want := "plain\n\n## Section\n\nbody"
	if got != want {
		t.Fatalf("composeSystemPrompt() = %q, want %q", got, want)
	}
}

func TestDefaultToolUsagePromptIncludesPermissionAndAntiLoopGuidance(t *testing.T) {
	t.Parallel()

	sections := defaultSystemPromptSections()
	var toolUsage string
	var failureRecovery string
	for _, section := range sections {
		if section.Title == "Tool Usage" {
			toolUsage = section.Content
		}
		if section.Title == "Failure Recovery" {
			failureRecovery = section.Content
		}
	}
	if toolUsage == "" {
		t.Fatalf("expected Tool Usage section to exist")
	}
	if !strings.Contains(toolUsage, "permission layer") {
		t.Fatalf("expected Tool Usage to mention permission layer, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "Do not invent tool names") {
		t.Fatalf("expected Tool Usage to forbid invented tool names, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "`todo_write`") {
		t.Fatalf("expected Tool Usage to mention todo_write for task state, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "`filesystem_read_file`, `filesystem_grep`, and `filesystem_glob`") {
		t.Fatalf("expected Tool Usage to prefer structured read/search tools, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "`filesystem_edit` for precise edits") {
		t.Fatalf("expected Tool Usage to describe edit tool preference, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "Do not use `bash` to edit files") {
		t.Fatalf("expected Tool Usage to discourage bash file edits, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "avoid interactive or blocking commands") {
		t.Fatalf("expected Tool Usage to constrain bash usage, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "Do not self-reject") {
		t.Fatalf("expected Tool Usage to discourage self-reject, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "Do not repeat the same tool call with identical arguments") {
		t.Fatalf("expected Tool Usage to include anti-loop guidance, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "focused verification call") {
		t.Fatalf("expected Tool Usage to limit write verification retries, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "stop using tools and give the user the result") {
		t.Fatalf("expected Tool Usage to tell the agent when to stop, got %q", toolUsage)
	}
	if !strings.Contains(toolUsage, "`status`, `truncated`, `tool_call_id`, `meta.*`, and `content`") {
		t.Fatalf("expected Tool Usage to explain structured tool results, got %q", toolUsage)
	}
	if !strings.Contains(failureRecovery, "change something concrete") {
		t.Fatalf("expected Failure Recovery to discourage identical retries, got %q", failureRecovery)
	}
}
