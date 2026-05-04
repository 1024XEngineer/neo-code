package runtime

import (
	"os"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// fileSnapshot 工具执行前的文件状态快照，用于在执行后计算精确 diff。
type fileSnapshot struct {
	path    string
	content []byte
	existed bool
}

// captureFileSnapshot 读取目标文件当前内容并打包成快照。文件不存在时 existed=false。
func captureFileSnapshot(path string) fileSnapshot {
	snap := fileSnapshot{path: path}
	content, err := os.ReadFile(path)
	if err == nil {
		snap.content = content
		snap.existed = true
	}
	return snap
}

// Diff 对比快照内容和文件当前内容，返回 unified diff。
// 内容未变化或文件仍不存在时返回空字符串。
func (s fileSnapshot) Diff() (string, error) {
	current, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			if !s.existed {
				return "", nil
			}
			return computeUnifiedDiff(string(s.content), "", s.path)
		}
		return "", err
	}
	if s.existed && string(current) == string(s.content) {
		return "", nil
	}
	oldContent := ""
	if s.existed {
		oldContent = string(s.content)
	}
	return computeUnifiedDiff(oldContent, string(current), s.path)
}

// WasNew 判断该文件在 Capture 时是否不存在（agent 新建了该文件）。
func (s fileSnapshot) WasNew() bool {
	return !s.existed
}

// computeUnifiedDiff 计算两段文本的 unified diff，使用 go-difflib 生成标准格式。
func computeUnifiedDiff(oldContent, newContent, label string) (string, error) {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: label,
		ToFile:   label,
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}
