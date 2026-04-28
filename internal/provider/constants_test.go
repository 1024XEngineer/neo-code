package provider

import (
	"testing"
	"time"
)

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

func TestNormalizeGenerateStartTimeout(t *testing.T) {
	t.Parallel()

	if got := NormalizeGenerateStartTimeout(0); got != DefaultGenerateStartTimeout {
		t.Fatalf("NormalizeGenerateStartTimeout(0) = %s, want %s", got, DefaultGenerateStartTimeout)
	}
	if got := NormalizeGenerateStartTimeout(-time.Second); got != DefaultGenerateStartTimeout {
		t.Fatalf("NormalizeGenerateStartTimeout(-1s) = %s, want %s", got, DefaultGenerateStartTimeout)
	}
	want := 3 * time.Second
	if got := NormalizeGenerateStartTimeout(want); got != want {
		t.Fatalf("NormalizeGenerateStartTimeout(3s) = %s, want %s", got, want)
	}
}

func TestNormalizeGenerateIdleTimeout(t *testing.T) {
	t.Parallel()

	if got := NormalizeGenerateIdleTimeout(0); got != DefaultGenerateIdleTimeout {
		t.Fatalf("NormalizeGenerateIdleTimeout(0) = %s, want %s", got, DefaultGenerateIdleTimeout)
	}
	if got := NormalizeGenerateIdleTimeout(-time.Second); got != DefaultGenerateIdleTimeout {
		t.Fatalf("NormalizeGenerateIdleTimeout(-1s) = %s, want %s", got, DefaultGenerateIdleTimeout)
	}
	want := 4 * time.Second
	if got := NormalizeGenerateIdleTimeout(want); got != want {
		t.Fatalf("NormalizeGenerateIdleTimeout(4s) = %s, want %s", got, want)
	}
}

func TestDefaultGenerateRetryMaxWait(t *testing.T) {
	t.Parallel()

	if DefaultGenerateRetryMaxWait != 7*time.Second {
		t.Fatalf("DefaultGenerateRetryMaxWait = %s, want 7s", DefaultGenerateRetryMaxWait)
	}
}
