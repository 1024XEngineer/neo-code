package ptyproxy

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8HeadTailNoTruncation(t *testing.T) {
	text := "hello 世界"
	got := TruncateUTF8HeadTail(text, len(text)+16)
	if got != text {
		t.Fatalf("got %q, want %q", got, text)
	}
}

func TestTruncateUTF8HeadTailKeepsUTF8Validity(t *testing.T) {
	text := strings.Repeat("中", 120) + "tail"
	got := TruncateUTF8HeadTail(text, 64)
	if !utf8.ValidString(got) {
		t.Fatalf("result is invalid utf-8: %q", got)
	}
	if !strings.Contains(got, truncationMarker) {
		t.Fatalf("result should contain truncation marker: %q", got)
	}
}

func TestTruncateUTF8HeadTailTinyBudget(t *testing.T) {
	text := "你好，世界，这是一个很长的字符串。"
	got := TruncateUTF8HeadTail(text, 6)
	if !utf8.ValidString(got) {
		t.Fatalf("result is invalid utf-8: %q", got)
	}
	if len(got) > 6 {
		t.Fatalf("len(result) = %d, want <= 6", len(got))
	}
}

func TestUTF8SafePrefixSuffixBranchCoverage(t *testing.T) {
	text := "hello"
	if got := utf8SafePrefix(text, 0); got != "" {
		t.Fatalf("utf8SafePrefix(bytes=0) = %q, want empty", got)
	}
	if got := utf8SafePrefix(text, len(text)+1); got != text {
		t.Fatalf("utf8SafePrefix(full) = %q, want %q", got, text)
	}
	if got := utf8SafeSuffix(text, 0); got != "" {
		t.Fatalf("utf8SafeSuffix(bytes=0) = %q, want empty", got)
	}
	if got := utf8SafeSuffix(text, len(text)+1); got != text {
		t.Fatalf("utf8SafeSuffix(full) = %q, want %q", got, text)
	}
	if got := TruncateUTF8HeadTail(text, 0); got != text {
		t.Fatalf("TruncateUTF8HeadTail(max=0) = %q, want %q", got, text)
	}
}
