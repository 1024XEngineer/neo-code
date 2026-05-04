package ptyproxy

import "testing"

func TestPTYProxyCoverageGapBranches(t *testing.T) {
	t.Run("command exemption verb branch", func(t *testing.T) {
		if !isCommandExempted("   GrEp   keyword file.txt") {
			t.Fatal("expected grep command to be exempted")
		}
	})

	t.Run("fold carriage returns join branch", func(t *testing.T) {
		got := foldCarriageReturns("old\rnew\nline")
		if got != "new\nline" {
			t.Fatalf("foldCarriageReturns() = %q, want %q", got, "new\nline")
		}
	})

	t.Run("tmux inner osc payload branch", func(t *testing.T) {
		parser := &OSC133Parser{}
		raw := []byte("x\x1bPtmux;\x1b]133;C\a\x1b\\y")
		clean, events := parser.Feed(raw)
		if string(clean) != "xy" {
			t.Fatalf("clean = %q, want %q", clean, "xy")
		}
		if len(events) != 1 || events[0].Type != ShellEventCommandStart {
			t.Fatalf("events = %#v, want one command_start event", events)
		}
	})

	t.Run("keep leftover truncates over max", func(t *testing.T) {
		raw := make([]byte, maxOSCLeftover+128)
		for i := range raw {
			raw[i] = 'a'
		}
		kept := keepOSCLeftover(raw)
		if len(kept) != maxOSCLeftover {
			t.Fatalf("len(kept) = %d, want %d", len(kept), maxOSCLeftover)
		}
	})
}
