package openai

import (
	"bufio"
	"errors"
	"io"

	"neo-code/internal/provider"
)

// 单行与总量上限，防止恶意或异常数据导致内存无限增长。
const (
	maxSSELineSize     = 256 * 1024 // L1: 单行 256KB
	maxStreamTotalSize = 10 << 20   // L3: 总量 10MB
)

// boundedSSEReader 对 bufio.Reader 包装两级有界检查：
//   - L1: 每次读取的行不超过 maxSSELineSize
//   - L3: 累计读取字节数不超过 maxStreamTotalSize
//
// 纯同步设计，无 goroutine/channel，适用于 SSE 顺序消费场景。
type boundedSSEReader struct {
	reader    *bufio.Reader
	totalRead int64
}

// newBoundedSSEReader 创建有界 SSE 行读取器。
//
// 内部 bufio.Reader 的缓冲区大小设为 maxSSELineSize+1，使得 ReadSlice('\n')
// 在单行超过 L1 上限时立即返回 bufio.ErrBufferFull，避免 ReadString 那样
// 先把整行全部读进内存才做长度检查。
func newBoundedSSEReader(r io.Reader) *boundedSSEReader {
	return &boundedSSEReader{
		reader: bufio.NewReaderSize(r, maxSSELineSize+1),
	}
}

// ReadLine 读取一行（以 \n 分隔），同时执行 L1 和 L3 检查。
// 返回去除尾部 \r\n 的行内容；遇到 io.EOF 时返回空字符串和 nil。
//
// L1 检查通过 bufio.Reader 的缓冲区大小约束实现：如果一行在缓冲区内
// 未找到 \n，ReadSlice 直接返回 bufio.ErrBufferFull，无需先将整行载入内存。
func (r *boundedSSEReader) ReadLine() (string, error) {
	line, err := r.reader.ReadSlice('\n')

	// L1: 缓冲区溢出 → 单行超过 maxSSELineSize（触发在读取过程中，而非读完后）
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", provider.ErrLineTooLong
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	// L1 兜底：行内容长度检查（不含末尾 \n）
	rawLen := len(line)
	if rawLen > 0 && line[rawLen-1] == '\n' {
		rawLen--
	}
	if rawLen > maxSSELineSize {
		return "", provider.ErrLineTooLong
	}

	// L3: 总量检查
	r.totalRead += int64(len(line))
	if r.totalRead > maxStreamTotalSize {
		return "", provider.ErrStreamTooLarge
	}

	// 将 []byte 转为 string（ReadSlice 返回的底层数据在下次读取时会被覆盖）
	return trimLineEnding(string(line)), err
}

// trimLineEnding 移除行尾的 \r\n 或 \n。
func trimLineEnding(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
