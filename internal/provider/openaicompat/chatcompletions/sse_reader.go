package chatcompletions

import (
	"bufio"
	"errors"
	"io"

	"neo-code/internal/provider"
)

// 单行与总量上限，防止恶意或异常数据导致内存无限增长。
const (
	MaxSSELineSize     = 256 * 1024 // L1: 单行 256KB
	MaxStreamTotalSize = 10 << 20   // L3: 总量 10MB
)

// BoundedSSEReader 对 bufio.Reader 包装两级有界检查，
// 适用于 Chat Completions SSE 的顺序消费场景。
type BoundedSSEReader struct {
	reader    *bufio.Reader
	totalRead int64
}

// NewBoundedSSEReader 创建有界 SSE 行读取器。
func NewBoundedSSEReader(r io.Reader) *BoundedSSEReader {
	return &BoundedSSEReader{
		reader: bufio.NewReaderSize(r, MaxSSELineSize+1),
	}
}

// ReadLine 读取一行（以 \n 分隔），同时执行 L1 与 L3 检查。
func (r *BoundedSSEReader) ReadLine() (string, error) {
	line, err := r.reader.ReadSlice('\n')

	if errors.Is(err, bufio.ErrBufferFull) {
		return "", provider.ErrLineTooLong
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	rawLen := len(line)
	if rawLen > 0 && line[rawLen-1] == '\n' {
		rawLen--
	}
	if rawLen > MaxSSELineSize {
		return "", provider.ErrLineTooLong
	}

	r.totalRead += int64(len(line))
	if r.totalRead > MaxStreamTotalSize {
		return "", provider.ErrStreamTooLarge
	}

	return TrimLineEnding(string(line)), err
}

// TrimLineEnding 移除行尾的 \r\n 或 \n。
func TrimLineEnding(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
