package session

import (
	"strings"
	"testing"
)

func TestClampTaskStateBoundariesTruncatesFieldsAndLists(t *testing.T) {
	t.Parallel()

	input := TaskState{
		Goal:            strings.Repeat("g", taskStateMaxFieldChars+10),
		NextStep:        strings.Repeat("n", taskStateMaxFieldChars+5),
		Progress:        []string{strings.Repeat("p", taskStateMaxListItemChars+10)},
		OpenItems:       []string{strings.Repeat("o", taskStateMaxListItemChars+10)},
		Blockers:        []string{strings.Repeat("b", taskStateMaxListItemChars+10)},
		KeyArtifacts:    []string{strings.Repeat("k", taskStateMaxListItemChars+10)},
		Decisions:       []string{strings.Repeat("d", taskStateMaxListItemChars+10)},
		UserConstraints: []string{strings.Repeat("u", taskStateMaxListItemChars+10)},
	}

	for i := 0; i < taskStateMaxListItems+6; i++ {
		input.Progress = append(input.Progress, strings.Repeat("x", taskStateMaxListItemChars-4)+buildIndexedSuffix(i))
	}

	clamped := ClampTaskStateBoundaries(input)
	if len([]rune(clamped.Goal)) != taskStateMaxFieldChars {
		t.Fatalf("expected goal to be clamped to %d runes, got %d", taskStateMaxFieldChars, len([]rune(clamped.Goal)))
	}
	if len([]rune(clamped.NextStep)) != taskStateMaxFieldChars {
		t.Fatalf("expected next_step to be clamped to %d runes, got %d", taskStateMaxFieldChars, len([]rune(clamped.NextStep)))
	}
	if len(clamped.Progress) != taskStateMaxListItems {
		t.Fatalf("expected progress length %d, got %d", taskStateMaxListItems, len(clamped.Progress))
	}
	if len([]rune(clamped.Progress[0])) != taskStateMaxListItemChars {
		t.Fatalf(
			"expected progress item to be clamped to %d runes, got %d",
			taskStateMaxListItemChars,
			len([]rune(clamped.Progress[0])),
		)
	}
	if len([]rune(clamped.OpenItems[0])) != taskStateMaxListItemChars {
		t.Fatalf("expected open item clamped to %d runes, got %d", taskStateMaxListItemChars, len([]rune(clamped.OpenItems[0])))
	}
}

func TestClampTaskStateBoundariesKeepsZeroValueListsNil(t *testing.T) {
	t.Parallel()

	clamped := ClampTaskStateBoundaries(TaskState{})
	if clamped.Progress != nil || clamped.OpenItems != nil || clamped.Blockers != nil {
		t.Fatalf("expected empty list fields to stay nil, got %+v", clamped)
	}
}

func TestTruncateRunesHandlesBoundaryConditions(t *testing.T) {
	t.Parallel()

	if got := truncateRunes("abc", 0); got != "" {
		t.Fatalf("expected zero limit to return empty string, got %q", got)
	}
	if got := truncateRunes("", 10); got != "" {
		t.Fatalf("expected empty input to stay empty, got %q", got)
	}
	if got := truncateRunes("你好世界", 2); got != "你好" {
		t.Fatalf("expected unicode-safe truncate result %q, got %q", "你好", got)
	}
}

func TestNormalizeTaskStateListPreservesCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "大小写不同项共存",
			input: []string{"React", "react"},
			want:  []string{"React", "react"},
		},
		{
			name:  "iOS 与 IOS 不同",
			input: []string{"iOS", "IOS"},
			want:  []string{"iOS", "IOS"},
		},
		{
			name:  "精确重复仍去重",
			input: []string{"react", "react"},
			want:  []string{"react"},
		},
		{
			name:  "空白 trim 后精确去重",
			input: []string{" item ", "item"},
			want:  []string{"item"},
		},
		{
			name:  "空白项被过滤",
			input: []string{"valid", "  ", "", "also-valid"},
			want:  []string{"valid", "also-valid"},
		},
		{
			name:  "全空白返回 nil",
			input: []string{" ", "", "\t"},
			want:  nil,
		},
		{
			name:  "空输入返回 nil",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeTaskStateList(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeTaskStateList(%v) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("normalizeTaskStateList(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeTaskState(t *testing.T) {
	t.Parallel()

	state := TaskState{
		Goal:     "  goal  ",
		Progress: []string{"A", "a", "A"},
	}
	normalized := NormalizeTaskState(state)

	if normalized.Goal != "goal" {
		t.Fatalf("expected goal %q, got %q", "goal", normalized.Goal)
	}
	if len(normalized.Progress) != 2 {
		t.Fatalf("expected 2 progress items (A and a are distinct), got %d: %v", len(normalized.Progress), normalized.Progress)
	}
}
