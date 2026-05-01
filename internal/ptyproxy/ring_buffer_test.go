package ptyproxy

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestUTF8RingBufferSnapshotKeepsUTF8Boundary(t *testing.T) {
	buffer := NewUTF8RingBuffer(8)

	_, _ = buffer.Write([]byte("abc你"))
	_, _ = buffer.Write([]byte("好"))

	snapshot := buffer.SnapshotString()
	if !utf8.ValidString(snapshot) {
		t.Fatalf("snapshot is not utf8 valid: %q", snapshot)
	}
	if strings.ContainsRune(snapshot, '\uFFFD') {
		t.Fatalf("snapshot contains replacement rune: %q", snapshot)
	}
}

func TestUTF8RingBufferConcurrentWriteAndCapacity(t *testing.T) {
	buffer := NewUTF8RingBuffer(64 * 1024)

	const goroutines = 8
	const rounds = 500

	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutines)
	for index := 0; index < goroutines; index++ {
		go func() {
			defer waitGroup.Done()
			for round := 0; round < rounds; round++ {
				_, _ = buffer.Write([]byte("日志-中文-ansi-\u001b[31mred\u001b[0m\n"))
			}
		}()
	}
	waitGroup.Wait()

	snapshot := buffer.SnapshotBytes()
	if len(snapshot) > 64*1024 {
		t.Fatalf("snapshot size = %d, want <= %d", len(snapshot), 64*1024)
	}
	if !utf8.Valid(snapshot) {
		t.Fatalf("snapshot bytes are not utf8 valid")
	}
	if strings.ContainsRune(string(snapshot), '\uFFFD') {
		t.Fatalf("snapshot contains replacement rune")
	}
}

func TestNormalizeUTF8WindowStripsBrokenPrefixAndSuffix(t *testing.T) {
	// "你" = e4 bd a0，"好" = e5 a5 bd
	raw := []byte{0xBD, 0xA0, 0xE5, 0xA5}

	safe := normalizeUTF8Window(raw)
	if len(safe) != 0 {
		t.Fatalf("safe len = %d, want 0 for fully broken window", len(safe))
	}
}

func TestNormalizeUTF8WindowEmptyInput(t *testing.T) {
	result := normalizeUTF8Window(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
	result = normalizeUTF8Window([]byte{})
	if len(result) != 0 {
		t.Fatalf("expected empty for empty input, got %d bytes", len(result))
	}
}

func TestNormalizeUTF8WindowValidInput(t *testing.T) {
	input := []byte("hello world")
	result := normalizeUTF8Window(input)
	if string(result) != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", string(result))
	}
}

func TestNormalizeUTF8WindowPartialTrimRecoversValid(t *testing.T) {
	// "你" = E4 BD A0; prepend continuation bytes then append partial sequence
	raw := []byte{0xBD, 0xA0, 0xE4, 0xBD, 0xA0, 0xE5}
	safe := normalizeUTF8Window(raw)
	// After trimming leading continuation bytes: [E4 BD A0 E5]
	// E5 is start of 3-byte sequence but incomplete → trim to [E4 BD A0] = "你"
	if string(safe) != "你" {
		t.Fatalf("expected %q, got %q", "你", string(safe))
	}
}

func TestNewUTF8RingBufferNonPositive(t *testing.T) {
	zero := NewUTF8RingBuffer(0)
	if zero.capacity != 1 {
		t.Fatalf("capacity = %d, want 1", zero.capacity)
	}
	negative := NewUTF8RingBuffer(-5)
	if negative.capacity != 1 {
		t.Fatalf("capacity = %d, want 1", negative.capacity)
	}
}

func TestUTF8RingBufferNilWrite(t *testing.T) {
	var buffer *UTF8RingBuffer
	n, err := buffer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len("hello") {
		t.Fatalf("Write() n = %d, want %d", n, len("hello"))
	}
}

func TestUTF8RingBufferNilSnapshotBytes(t *testing.T) {
	var buffer *UTF8RingBuffer
	result := buffer.SnapshotBytes()
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestUTF8RingBufferNilSnapshotString(t *testing.T) {
	var buffer *UTF8RingBuffer
	result := buffer.SnapshotString()
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestUTF8RingBufferEmptyWrite(t *testing.T) {
	buffer := NewUTF8RingBuffer(8)
	n, err := buffer.Write(nil)
	if err != nil {
		t.Fatalf("Write(nil) error = %v", err)
	}
	if n != 0 {
		t.Fatalf("Write(nil) n = %d, want 0", n)
	}
	n, err = buffer.Write([]byte{})
	if err != nil {
		t.Fatalf("Write(empty) error = %v", err)
	}
	if n != 0 {
		t.Fatalf("Write(empty) n = %d, want 0", n)
	}
}

func TestUTF8RingBufferCapacityBoundary(t *testing.T) {
	buffer := NewUTF8RingBuffer(5)
	_, _ = buffer.Write([]byte("abcdefgh"))
	snapshot := buffer.SnapshotString()
	if len(snapshot) > 5 {
		t.Fatalf("snapshot length = %d, want <= 5", len(snapshot))
	}
}
