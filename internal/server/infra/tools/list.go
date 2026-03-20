package tools

import (
	"fmt"
	"os"
)

// ListTool 列出目录内容。
type ListTool struct{}

// Name 返回工具名称。
func (l *ListTool) Name() string {
	return "list"
}

// Description 返回工具描述。
func (l *ListTool) Description() string {
	return "列出指定路径中的文件和目录。每行返回一个条目，子目录后跟 '/'。"
}

// Run 执行列表工具，使用给定的参数。
// 期望的参数：
//   - path: 要列出的目录（可选，默认：当前目录）
func (l *ListTool) Run(params map[string]interface{}) *ToolResult {
	// 解析可选的path参数
	path := "." // 默认为当前目录
	if pathParam, ok := params["path"]; ok {
		path, ok = pathParam.(string)
		if !ok {
			return &ToolResult{
				ToolName: l.Name(),
				Success:  false,
				Error:    "path 必须是字符串",
			}
		}
	}

	// 打开目录
	file, err := os.Open(path)
	if err != nil {
		return &ToolResult{
			ToolName: l.Name(),
			Success:  false,
			Error:    fmt.Sprintf("打开目录失败: %v", err),
		}
	}
	defer file.Close()

	// 读取目录内容
	entries, err := file.Readdir(-1)
	if err != nil {
		return &ToolResult{
			ToolName: l.Name(),
			Success:  false,
			Error:    fmt.Sprintf("读取目录失败: %v", err),
		}
	}

	// 格式化输出
	var output string
	for _, entry := range entries {
		if entry.IsDir() {
			output += entry.Name() + "/\n"
		} else {
			output += entry.Name() + "\n"
		}
	}

	return &ToolResult{
		ToolName: l.Name(),
		Success:  true,
		Output:   output,
		Metadata: map[string]interface{}{
			"path":  path,
			"count": len(entries),
		},
	}
}
