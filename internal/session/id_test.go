package session

import (
	"strings"
	"testing"
)

func TestNewIDFormatAndUniqueness(t *testing.T) {
	t.Parallel()

	id1 := NewID("session")
	id2 := NewID("session")

	if !strings.HasPrefix(id1, "session_") || !strings.HasPrefix(id2, "session_") {
		t.Fatalf("expected prefix session_, got %q and %q", id1, id2)
	}

	hex1 := strings.TrimPrefix(id1, "session_")
	hex2 := strings.TrimPrefix(id2, "session_")

	if len(hex1) != 16 || len(hex2) != 16 {
		t.Fatalf("expected 16 hex chars, got %d and %d", len(hex1), len(hex2))
	}
	for _, ch := range hex1 + hex2 {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("expected lowercase hex, got %q in ids %q %q", ch, id1, id2)
		}
	}
	if id1 == id2 {
		t.Fatalf("expected different ids, got identical %q", id1)
	}
}

func TestNewIDAllowsEmptyPrefix(t *testing.T) {
	t.Parallel()

	id := NewID("")
	if len(id) != 17 || id[0] != '_' {
		t.Fatalf("expected format _<16hex>, got %q", id)
	}
}

func TestNewIDCompatibilityWrapper(t *testing.T) {
	t.Parallel()

	id := newID("session")
	if !strings.HasPrefix(id, "session_") {
		t.Fatalf("expected compatibility wrapper to preserve prefix, got %q", id)
	}
	if len(strings.TrimPrefix(id, "session_")) != 16 {
		t.Fatalf("expected compatibility wrapper to return 16 hex chars, got %q", id)
	}
}
