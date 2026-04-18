package subagent

import (
	"testing"
	"time"
)

func TestDefaultRetryBackoffWithBounds(t *testing.T) {
	t.Parallel()

	backoff := defaultRetryBackoffWithBounds(0, 0)
	if got := backoff(0); got != 0 {
		t.Fatalf("attempt=0 delay = %v, want 0", got)
	}
	if got := backoff(1); got != time.Second {
		t.Fatalf("attempt=1 delay = %v, want %v", got, time.Second)
	}
	if got := backoff(10); got != 30*time.Second {
		t.Fatalf("attempt=10 delay = %v, want %v", got, 30*time.Second)
	}

	bounded := defaultRetryBackoffWithBounds(5*time.Second, 2*time.Second)
	if got := bounded(2); got != 5*time.Second {
		t.Fatalf("max<base should clamp to base, got %v", got)
	}
}
