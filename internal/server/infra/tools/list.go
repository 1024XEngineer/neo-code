package tools

import (
	"fmt"
	"os"
)

// ListTool 列出目录内容。
type ListTool struct{}

func (l *ListTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "list",
		Description: "列出工作区内目录内容。每行一个条目，子目录后缀为 '/'.",
		Parameters:  []ToolParamSpec{{Name: "path", Type: "string", Description: "工作区内待列出的目录，默认当前工作区根目录。"}},
	}
}

func (l *ListTool) Run(params map[string]interface{}) *ToolResult {
	path, errRes := optionalString(params, "path", ".")
	if errRes != nil {
		errRes.ToolName = l.Definition().Name
		return errRes
	}
	path, pathErr := ensureWorkspacePath(path)
	if pathErr != nil {
		pathErr.ToolName = l.Definition().Name
		return pathErr
	}

	file, err := os.Open(path)
	if err != nil {
		return &ToolResult{ToolName: l.Definition().Name, Success: false, Error: fmt.Sprintf("打开目录失败: %v", err)}
	}
	defer file.Close()
	entries, err := file.Readdir(-1)
	if err != nil {
		return &ToolResult{ToolName: l.Definition().Name, Success: false, Error: fmt.Sprintf("读取目录失败: %v", err)}
	}
	output := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		output += name + "\n"
	}
	return &ToolResult{ToolName: l.Definition().Name, Success: true, Output: output, Metadata: map[string]interface{}{"path": path, "count": len(entries)}}
}
