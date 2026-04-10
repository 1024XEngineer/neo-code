package chatcompletions

import (
	"errors"
	"io"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestBoundedSSEReaderAndTrimLineEnding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		isEOF bool
	}{
		{name: "newline", input: "data: hello\n", want: "data: hello"},
		{name: "crlf", input: "data: world\r\n", want: "data: world"},
		{name: "empty", input: "\n", want: ""},
		{name: "comment", input: ": heartbeat\n", want: ": heartbeat"},
		{name: "eof", input: "data: partial", want: "data: partial", isEOF: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := NewBoundedSSEReader(strings.NewReader(tt.input))
			got, err := reader.ReadLine()
			if got != tt.want {
				t.Fatalf("ReadLine() = %q, want %q", got, tt.want)
			}
			if tt.isEOF {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("expected io.EOF, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	reader := NewBoundedSSEReader(strings.NewReader("line1\nline2\n\nline4\n"))
	for _, want := range []string{"line1", "line2", "", "line4"} {
		got, err := reader.ReadLine()
		if err != nil || got != want {
			t.Fatalf("unexpected line read: got=%q err=%v want=%q", got, err, want)
		}
	}
	if _, err := reader.ReadLine(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if _, err := NewBoundedSSEReader(strings.NewReader(strings.Repeat("x", MaxSSELineSize+1) + "\n")).ReadLine(); !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
	if got, err := NewBoundedSSEReader(strings.NewReader(strings.Repeat("a", MaxSSELineSize) + "\n")).ReadLine(); err != nil || len(got) != MaxSSELineSize {
		t.Fatalf("expected exact limit to pass, got len=%d err=%v", len(got), err)
	}
	if got, err := NewBoundedSSEReader(strings.NewReader(strings.Repeat("b", MaxSSELineSize))).ReadLine(); !errors.Is(err, io.EOF) || len(got) != MaxSSELineSize {
		t.Fatalf("expected exact limit EOF read, got len=%d err=%v", len(got), err)
	}
	if _, err := NewBoundedSSEReader(strings.NewReader(strings.Repeat("c", MaxSSELineSize+1))).ReadLine(); !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong for oversized EOF line, got %v", err)
	}

	line := strings.Repeat("x", 1024) + "\n"
	lineSize := int64(len(line))
	var builder strings.Builder
	for i := 0; i < int(MaxStreamTotalSize/lineSize)+1; i++ {
		builder.WriteString(line)
	}
	reader = NewBoundedSSEReader(strings.NewReader(builder.String()))
	for i := 0; i < int(MaxStreamTotalSize/lineSize); i++ {
		if _, err := reader.ReadLine(); err != nil {
			t.Fatalf("unexpected error before size limit: %v", err)
		}
	}
	if _, err := reader.ReadLine(); !errors.Is(err, provider.ErrStreamTooLarge) {
		t.Fatalf("expected ErrStreamTooLarge, got %v", err)
	}

	if _, err := NewBoundedSSEReader(&readerErr{err: io.ErrClosedPipe}).ReadLine(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected io.ErrClosedPipe, got %v", err)
	}

	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "hello\n", want: "hello"},
		{input: "hello\r\n", want: "hello"},
		{input: "hello\r\n\n", want: "hello"},
		{input: "\r", want: ""},
		{input: "", want: ""},
	} {
		if got := TrimLineEnding(tt.input); got != tt.want {
			t.Fatalf("TrimLineEnding(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

type readerErr struct{ err error }

func (r *readerErr) Read(_ []byte) (int, error) { return 0, r.err }
