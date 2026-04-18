package subagent

import "testing"

func TestDedupeAndTrim(t *testing.T) {
	t.Parallel()

	if got := dedupeAndTrim(nil); got != nil {
		t.Fatalf("dedupeAndTrim(nil) = %#v, want nil", got)
	}
	if got := dedupeAndTrim([]string{" ", "\t"}); got != nil {
		t.Fatalf("dedupeAndTrim(empty) = %#v, want nil", got)
	}
	got := dedupeAndTrim([]string{" Bash ", "bash", "FS", "fs", "Go "})
	want := []string{"Bash", "FS", "Go"}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}
