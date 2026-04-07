package infra

import "github.com/atotto/clipboard"

// clipboardWriteAll 指向实际剪贴板写入函数，便于在测试中替换。
var clipboardWriteAll = clipboard.WriteAll

// CopyText 将文本写入系统剪贴板。
func CopyText(text string) error {
	return clipboardWriteAll(text)
}
