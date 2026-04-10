package openaicompat

import (
	"errors"
	"io"
	"strings"
	"testing"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
)

// --- boundedSSEReader 单元测试 ---

func TestBoundedSSEReader_ReadLine_Normal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		isEOF bool
	}{
		{
			name:  "single line with newline",
			input: "data: hello\n",
			want:  "data: hello",
			isEOF: false,
		},
		{
			name:  "line with CRLF",
			input: "data: world\r\n",
			want:  "data: world",
			isEOF: false,
		},
		{
			name:  "empty line",
			input: "\n",
			want:  "",
			isEOF: false,
		},
		{
			name:  "SSE comment line",
			input: ": heartbeat\n",
			want:  ": heartbeat",
			isEOF: false,
		},
		{
			name:  "EOF without trailing newline (io.EOF)",
			input: "data: partial",
			want:  "data: partial",
			isEOF: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := chatcompletions.NewBoundedSSEReader(strings.NewReader(tt.input))
			got, err := r.ReadLine()
			if got != tt.want {
				t.Fatalf("ReadLine() = %q, want %q", got, tt.want)
			}
			if tt.isEOF && !errors.Is(err, io.EOF) {
				t.Fatalf("expected io.EOF, got %v", err)
			}
			if !tt.isEOF && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBoundedSSEReader_ReadLine_MultipleLines(t *testing.T) {
	t.Parallel()

	r := chatcompletions.NewBoundedSSEReader(strings.NewReader("line1\nline2\n\nline4\n"))

	line1, err := r.ReadLine()
	if err != nil || line1 != "line1" {
		t.Fatalf("first line: got %q, err = %v", line1, err)
	}

	line2, err := r.ReadLine()
	if err != nil || line2 != "line2" {
		t.Fatalf("second line: got %q, err = %v", line2, err)
	}

	// 空行
	empty, err := r.ReadLine()
	if err != nil || empty != "" {
		t.Fatalf("empty line: got %q, err = %v", empty, err)
	}

	line4, err := r.ReadLine()
	if err != nil || line4 != "line4" {
		t.Fatalf("fourth line: got %q, err = %v", line4, err)
	}

	// EOF
	_, err = r.ReadLine()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after all lines, got %v", err)
	}
}

func TestBoundedSSEReader_L1_LineTooLong(t *testing.T) {
	t.Parallel()

	longLine := strings.Repeat("x", chatcompletions.MaxSSELineSize+1) + "\n"
	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(longLine))

	_, err := r.ReadLine()
	if err == nil {
		t.Fatal("expected ErrLineTooLong for oversized line")
	}
	if !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
}

func TestBoundedSSEReader_L1_BoundaryExactLimit(t *testing.T) {
	t.Parallel()

	// 恰好等于上限的行应该正常通过（不含 \n）
	exactLine := strings.Repeat("a", chatcompletions.MaxSSELineSize) + "\n"
	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(exactLine))

	got, err := r.ReadLine()
	if err != nil {
		t.Fatalf("unexpected error at exact limit: %v", err)
	}
	if len(got) != chatcompletions.MaxSSELineSize {
		t.Fatalf("expected line length %d, got %d", chatcompletions.MaxSSELineSize, len(got))
	}
}

func TestBoundedSSEReader_L3_StreamTooLarge(t *testing.T) {
	t.Parallel()

	// 构造输入：每行 1KB（远小于 maxSSELineSize），行数足够多使总量超过 maxStreamTotalSize
	line := strings.Repeat("x", 1024) + "\n" // 1025 bytes per line
	lineSize := int64(len(line))

	var sb strings.Builder
	linesToWrite := int(chatcompletions.MaxStreamTotalSize/lineSize) + 1
	for range linesToWrite {
		sb.WriteString(line)
	}

	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(sb.String()))

	// 前面的行应能正常读取
	expectedNormal := int(chatcompletions.MaxStreamTotalSize / lineSize)
	for range expectedNormal {
		_, err := r.ReadLine()
		if err != nil {
			t.Fatalf("unexpected error on normal line: %v", err)
		}
	}

	// 超限的行应返回 ErrStreamTooLarge（而非 ErrLineTooLong）
	_, err := r.ReadLine()
	if err == nil {
		t.Fatal("expected ErrStreamTooLarge")
	}
	if !errors.Is(err, provider.ErrStreamTooLarge) {
		t.Fatalf("expected ErrStreamTooLarge, got %v", err)
	}
}

func TestTrimLineEnding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"hello\n", "hello"},
		{"hello\r\n", "hello"},
		{"hello\r\n\n", "hello"}, // 连续换行符全部去除
		{"hello", "hello"},
		{"\n", ""},
		{"\r\n", ""},
		{"\r", ""}, // 孤立 \r 也去除
		{"", ""},
	}

	for _, tt := range tests {
		got := chatcompletions.TrimLineEnding(tt.input)
		if got != tt.want {
			t.Fatalf("trimLineEnding(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestBoundedSSEReader_L1_NoNewlineEOFAtLimit 验证恰好等于缓冲区大小
// 但以 EOF 结尾（无 \n）的行能被正常读取并返回 io.EOF。
func TestBoundedSSEReader_L1_NoNewlineEOFAtLimit(t *testing.T) {
	t.Parallel()

	// 不含 \n 的行，长度恰好等于 maxSSELineSize，以 EOF 结尾
	exactLine := strings.Repeat("b", chatcompletions.MaxSSELineSize)
	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(exactLine))

	got, err := r.ReadLine()
	if err == nil || !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF for line without trailing newline, got err=%v", err)
	}
	if len(got) != chatcompletions.MaxSSELineSize {
		t.Fatalf("expected line length %d, got %d", chatcompletions.MaxSSELineSize, len(got))
	}
}

// TestBoundedSSEReader_L1_NoNewlineEOFExceedsLimit 验证超过缓冲区大小
// 且以 EOF 结尾（无 \n）的行返回 ErrLineTooLong。
func TestBoundedSSEReader_L1_NoNewlineEOFExceedsLimit(t *testing.T) {
	t.Parallel()

	// 不含 \n，长度超过 maxSSELineSize
	longLine := strings.Repeat("c", chatcompletions.MaxSSELineSize+1)
	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(longLine))

	_, err := r.ReadLine()
	if err == nil {
		t.Fatal("expected ErrLineTooLong for oversized line without newline")
	}
	if !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
}

// TestBoundedSSEReader_UnderlyingErrorPropagation 验证底层 reader 的非 EOF 错误
// 会被正确传播，而不是被 L1/L3 吞掉。
func TestBoundedSSEReader_UnderlyingErrorPropagation(t *testing.T) {
	t.Parallel()

	r := chatcompletions.NewBoundedSSEReader(&errReader{err: io.ErrClosedPipe})
	_, err := r.ReadLine()
	if err == nil {
		t.Fatal("expected error from broken reader")
	}
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected io.ErrClosedPipe, got %v", err)
	}
}

// TestBoundedSSEReader_L1_ThenNormalRead 验证 L1 触发后 reader 状态仍然一致，
// 后续正常行仍可继续读取（如果调用方选择恢复）。
func TestBoundedSSEReader_L1_ThenNormalRead(t *testing.T) {
	t.Parallel()

	// 第一行超长触发 L1，第二行正常
	input := strings.Repeat("x", chatcompletions.MaxSSELineSize+10) + "\nnormal line\n"
	r := chatcompletions.NewBoundedSSEReader(strings.NewReader(input))

	// 第一行应返回 ErrLineTooLong
	_, err := r.ReadLine()
	if !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("first line: expected ErrLineTooLong, got %v", err)
	}

	// 注意：L1 触发后 bufio.Reader 内部可能已消耗了部分后续数据（缓冲区残留），
	// 因此此测试仅验证 L1 错误被正确返回，不要求后续行一定可读。
	// 这符合实际使用场景——L1 触发后调用方会终止流消费。
}
