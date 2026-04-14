package session

import (
	"testing"
)

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
