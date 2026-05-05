package ptyproxy

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// UTF8RingBuffer 提供按字节上限滚动缓存，并在截断时保持 UTF-8 边界安全。
type UTF8RingBuffer struct {
	capacity int

	mu   sync.Mutex
	data []byte
}

// NewUTF8RingBuffer 创建指定容量的 UTF-8 安全 Ring Buffer；非正容量时退化为 1 字节。
func NewUTF8RingBuffer(capacity int) *UTF8RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &UTF8RingBuffer{
		capacity: capacity,
		data:     make([]byte, 0, capacity),
	}
}

// Write 将新输出追加到缓冲区，并在超限时保留末尾窗口且修正 UTF-8 边界。
func (b *UTF8RingBuffer) Write(payload []byte) (int, error) {
	if b == nil {
		return len(payload), nil
	}
	if len(payload) == 0 {
		return 0, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, payload...)
	if len(b.data) > b.capacity {
		b.data = b.data[len(b.data)-b.capacity:]
	}
	b.data = normalizeUTF8Window(b.data)
	return len(payload), nil
}

// SnapshotBytes 返回当前缓冲区的 UTF-8 安全快照副本。
func (b *UTF8RingBuffer) SnapshotBytes() []byte {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	safe := normalizeUTF8Window(b.data)
	cloned := make([]byte, len(safe))
	copy(cloned, safe)
	return cloned
}

// SnapshotString 返回当前缓冲区的 UTF-8 安全文本快照。
func (b *UTF8RingBuffer) SnapshotString() string {
	if b == nil {
		return ""
	}
	return strings.TrimSpace(string(b.SnapshotBytes()))
}

// Reset 清空缓冲区内容但保留当前容量配置，便于会话边界快速复位。
func (b *UTF8RingBuffer) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
}

// Capacity 返回当前缓冲区容量配置（字节数）。
func (b *UTF8RingBuffer) Capacity() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capacity
}

// Resize 调整缓冲区容量并保持 UTF-8 边界安全，必要时裁剪历史窗口。
func (b *UTF8RingBuffer) Resize(capacity int) {
	if b == nil {
		return
	}
	if capacity <= 0 {
		capacity = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.capacity = capacity
	if len(b.data) > b.capacity {
		b.data = b.data[len(b.data)-b.capacity:]
	}
	b.data = normalizeUTF8Window(b.data)
}

// normalizeUTF8Window 将窗口裁剪为合法 UTF-8 字节序列，避免多字节字符被截断产生乱码。
func normalizeUTF8Window(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	trimmed := trimUTF8ContinuationPrefix(raw)
	if len(trimmed) == 0 {
		return trimmed
	}
	if utf8.Valid(trimmed) {
		return trimmed
	}

	// 尾部只可能出现被截断的多字节字符，逐字节回退直到合法为止。
	for end := len(trimmed); end > 0; end-- {
		candidate := trimmed[:end]
		if utf8.Valid(candidate) {
			return candidate
		}
	}
	return nil
}

// trimUTF8ContinuationPrefix 去掉开头残留的 continuation byte，避免窗口从字符中间起始。
func trimUTF8ContinuationPrefix(raw []byte) []byte {
	start := 0
	for start < len(raw) && start < utf8.UTFMax-1 {
		if raw[start]&0xC0 != 0x80 {
			break
		}
		start++
	}
	return raw[start:]
}
