package provider

import "testing"

func TestNormalizeGenerateMaxRetries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero keeps explicit value", input: 0, want: 0},
		{name: "negative fallback", input: -1, want: DefaultGenerateMaxRetries},
		{name: "keep in range", input: 3, want: 3},
		{name: "clamp upper bound", input: MaxGenerateMaxRetries + 10, want: MaxGenerateMaxRetries},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeGenerateMaxRetries(tt.input); got != tt.want {
				t.Fatalf("NormalizeGenerateMaxRetries(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
