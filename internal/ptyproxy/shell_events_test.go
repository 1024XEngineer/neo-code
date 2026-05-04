package ptyproxy

import (
	"bytes"
	"testing"
)

func TestOSC133ParserExtractsEventsAndStripsControlBytes(t *testing.T) {
	parser := &OSC133Parser{}
	payload := []byte("hello\x1b]133;C\aerr\n\x1b]133;D;2\aworld\x1b]133;A\a!")
	clean, events := parser.Feed(payload)

	if string(clean) != "helloerr\nworld!" {
		t.Fatalf("clean = %q, want %q", string(clean), "helloerr\nworld!")
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	if events[0].Type != ShellEventCommandStart {
		t.Fatalf("events[0].Type = %q", events[0].Type)
	}
	if events[1].Type != ShellEventCommandDone || events[1].ExitCode != 2 {
		t.Fatalf("events[1] = %#v, want command_done/2", events[1])
	}
	if events[2].Type != ShellEventPromptReady {
		t.Fatalf("events[2].Type = %q", events[2].Type)
	}
}

func TestOSC133ParserSupportsChunkedInput(t *testing.T) {
	parser := &OSC133Parser{}
	part1 := []byte("before\x1b]133;D;")
	part2 := []byte("137\amiddle\x1b]133;A\aafter")

	clean1, events1 := parser.Feed(part1)
	clean2, events2 := parser.Feed(part2)

	if string(clean1) != "before" {
		t.Fatalf("clean1 = %q, want %q", string(clean1), "before")
	}
	if len(events1) != 0 {
		t.Fatalf("events1 len = %d, want 0", len(events1))
	}
	if string(clean2) != "middleafter" {
		t.Fatalf("clean2 = %q, want %q", string(clean2), "middleafter")
	}
	if len(events2) != 2 {
		t.Fatalf("events2 len = %d, want 2", len(events2))
	}
	if events2[0].Type != ShellEventCommandDone || events2[0].ExitCode != 137 {
		t.Fatalf("events2[0] = %#v", events2[0])
	}
	if events2[1].Type != ShellEventPromptReady {
		t.Fatalf("events2[1] = %#v", events2[1])
	}
}

func TestOSC133ParserSupportsTmuxPassthrough(t *testing.T) {
	parser := &OSC133Parser{}
	raw := []byte("x\x1bPtmux;\x1b\x1b]133;C\a\x1b\\y")
	clean, events := parser.Feed(raw)
	if string(clean) != "xy" {
		t.Fatalf("clean = %q, want %q", string(clean), "xy")
	}
	if len(events) != 1 || events[0].Type != ShellEventCommandStart {
		t.Fatalf("events = %#v", events)
	}
}

func TestOSC133ParserLeftoverBounded(t *testing.T) {
	parser := &OSC133Parser{}
	noise := bytes.Repeat([]byte("a"), maxOSCLeftover+128)
	// 制造一个不完整的 OSC 前缀，确保 leftover 会被上限裁剪。
	chunk := append([]byte("\x1b]133;"), noise...)
	_, _ = parser.Feed(chunk)
	if len(parser.leftover) > maxOSCLeftover {
		t.Fatalf("leftover len = %d, want <= %d", len(parser.leftover), maxOSCLeftover)
	}
}

func TestOSC133ParserAndHelpersAdditionalBranches(t *testing.T) {
	t.Run("feed empty chunk", func(t *testing.T) {
		parser := &OSC133Parser{}
		clean, events := parser.Feed(nil)
		if clean != nil || events != nil {
			t.Fatalf("Feed(nil) = (%v, %v), want (nil, nil)", clean, events)
		}
	})

	t.Run("tmux passthrough unknown event", func(t *testing.T) {
		parser := &OSC133Parser{}
		raw := []byte("x\x1bPtmux;\x1b\x1b]133;X\a\x1b\\y")
		clean, events := parser.Feed(raw)
		if string(clean) != "xy" {
			t.Fatalf("clean = %q, want xy", clean)
		}
		if len(events) != 0 {
			t.Fatalf("events = %#v, want empty", events)
		}
	})

	t.Run("command done with invalid exit code", func(t *testing.T) {
		event, ok := parseOSC133Event("D;not-a-number")
		if !ok {
			t.Fatal("expected parseOSC133Event to match D payload")
		}
		if event.Type != ShellEventCommandDone || event.ExitCode != 0 {
			t.Fatalf("event = %#v, want command_done with exit 0", event)
		}
	})

	t.Run("hasPrefixAt bounds and keep leftover short", func(t *testing.T) {
		if hasPrefixAt([]byte("abc"), -1, "a") {
			t.Fatal("hasPrefixAt should return false for negative index")
		}
		if hasPrefixAt([]byte("abc"), 3, "a") {
			t.Fatal("hasPrefixAt should return false for out of range index")
		}
		if !hasPrefixAt([]byte("abc"), 0, "ab") {
			t.Fatal("hasPrefixAt should return true for matching prefix")
		}

		raw := []byte("short")
		kept := keepOSCLeftover(raw)
		if string(kept) != "short" {
			t.Fatalf("keepOSCLeftover(short) = %q, want short", kept)
		}
	})
}

func TestOSC133ParseHelpers(t *testing.T) {
	t.Run("parse osc payload and esc terminator", func(t *testing.T) {
		payload, next, ok := parseOSCFromBuffer([]byte("\x1b]133;D;7\x1b\\tail"), 0)
		if !ok {
			t.Fatal("parseOSCFromBuffer should parse ESC terminator payload")
		}
		if payload != "D;7" {
			t.Fatalf("payload = %q, want D;7", payload)
		}
		if next <= 0 {
			t.Fatalf("next = %d, want >0", next)
		}

		parsed, ok := parseOSCPayload([]byte("\x1b]133;C\a"))
		if !ok || parsed != "C" {
			t.Fatalf("parseOSCPayload() = (%q, %v), want (C, true)", parsed, ok)
		}
	})

	t.Run("parse osc event empty payload", func(t *testing.T) {
		if _, ok := parseOSC133Event("   "); ok {
			t.Fatal("parseOSC133Event should reject empty payload")
		}
	})
}
