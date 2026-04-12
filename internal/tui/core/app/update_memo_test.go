package tui

import (
	"testing"

	"neo-code/internal/memo"
)

func TestNormalizeRememberTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "collapse whitespace", input: "  keep   one\nline  ", want: "keep one line"},
		{name: "replace parens", input: "title (unsafe)", want: "title {unsafe}"},
		{name: "empty", input: "\n\t  ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := memo.NormalizeTitle(tt.input); got != tt.want {
				t.Fatalf("NormalizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
