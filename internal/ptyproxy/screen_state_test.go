package ptyproxy

import (
	"bytes"
	"testing"
)

func TestAltScreenStateTracksEnterExitAndSuppressWindow(t *testing.T) {
	state := newAltScreenState(true)
	state.Observe([]byte("\x1b[?1049h"))
	if !state.inAltScreen {
		t.Fatal("expected inAltScreen=true after enter sequence")
	}
	if !state.ShouldSuppressAutoTrigger(false) {
		t.Fatal("expected suppression while in alt-screen")
	}

	state.Observe([]byte("\x1b[?1049l"))
	if state.inAltScreen {
		t.Fatal("expected inAltScreen=false after exit sequence")
	}
	if !state.ShouldSuppressAutoTrigger(false) {
		t.Fatal("expected post-exit suppression window")
	}
	if !state.ShouldSuppressAutoTrigger(true) {
		t.Fatal("expected consume=true still returns suppression on first consume")
	}
	if state.ShouldSuppressAutoTrigger(false) {
		t.Fatal("expected suppression window to be consumed")
	}
}

func TestAltScreenStateSupportsChunkedCSISequence(t *testing.T) {
	state := newAltScreenState(true)
	state.Observe([]byte("\x1b[?10"))
	if state.inAltScreen {
		t.Fatal("inAltScreen should remain false before sequence is complete")
	}
	state.Observe([]byte("49h"))
	if !state.inAltScreen {
		t.Fatal("expected inAltScreen=true after chunked enter sequence")
	}
}

func TestAltScreenStateSupportsChunkedBareEscapeCSISequence(t *testing.T) {
	state := newAltScreenState(true)
	state.Observe([]byte("\x1b"))
	if state.inAltScreen {
		t.Fatal("inAltScreen should remain false before CSI prefix is complete")
	}
	state.Observe([]byte("[?1049h"))
	if !state.inAltScreen {
		t.Fatal("expected inAltScreen=true after bare ESC and CSI suffix")
	}
}

func TestAltScreenStateSupportsMode47And1047(t *testing.T) {
	state := newAltScreenState(true)
	state.Observe([]byte("\x1b[?47h"))
	if !state.inAltScreen {
		t.Fatal("expected mode 47 to enter alt-screen")
	}
	state.Observe([]byte("\x1b[?47l"))
	if state.inAltScreen {
		t.Fatal("expected mode 47 to leave alt-screen")
	}

	state.Observe([]byte("\x1b[?1047h"))
	if !state.inAltScreen {
		t.Fatal("expected mode 1047 to enter alt-screen")
	}
	state.Observe([]byte("\x1b[?1047l"))
	if state.inAltScreen {
		t.Fatal("expected mode 1047 to leave alt-screen")
	}
}

func TestAltScreenStateSupportsTmuxPassthrough(t *testing.T) {
	state := newAltScreenState(true)
	enter := []byte("x\x1bPtmux;\x1b\x1b[?1049h\x1b\\y")
	exit := []byte("\x1bPtmux;\x1b\x1b[?1049l\x1b\\")
	state.Observe(enter)
	if !state.inAltScreen {
		t.Fatal("expected tmux passthrough enter sequence to be recognized")
	}
	state.Observe(exit)
	if state.inAltScreen {
		t.Fatal("expected tmux passthrough exit sequence to be recognized")
	}
	if !state.ShouldSuppressAutoTrigger(false) {
		t.Fatal("expected suppress window after tmux passthrough exit")
	}
}

func TestAltScreenStateSupportsChunkedTmuxPassthroughPrefix(t *testing.T) {
	state := newAltScreenState(true)
	state.Observe([]byte("\x1bPtm"))
	if state.inAltScreen {
		t.Fatal("inAltScreen should remain false before tmux prefix is complete")
	}
	state.Observe([]byte("ux;\x1b\x1b[?1049h\x1b\\"))
	if !state.inAltScreen {
		t.Fatal("expected chunked tmux passthrough enter sequence to be recognized")
	}
}

func TestAltScreenStateGuardDisabled(t *testing.T) {
	state := newAltScreenState(false)
	state.Observe([]byte("\x1b[?1049h"))
	if state.inAltScreen {
		t.Fatal("expected disabled guard to skip state updates")
	}
	if state.ShouldSuppressAutoTrigger(false) {
		t.Fatal("expected disabled guard to never suppress auto trigger")
	}
}

func TestKeepAltScreenLeftoverBounded(t *testing.T) {
	raw := bytes.Repeat([]byte("a"), maxAltScreenLeftover+256)
	kept := keepAltScreenLeftover(raw)
	if len(kept) != maxAltScreenLeftover {
		t.Fatalf("len(kept) = %d, want %d", len(kept), maxAltScreenLeftover)
	}
}
