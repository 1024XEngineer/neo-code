package tools

import (
	"bufio"
	"fmt"
	"os"
)

// ReadTool 读取文件内容，支持可选的行范围参数。
type ReadTool struct{}

func (r *ReadTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read",
		Description: "读取工作区内的文件或目录。读取文件时支持 offset/limit 分页，读取目录时返回目录项列表。",
		Parameters: []ToolParamSpec{
			{Name: "filePath", Type: "string", Required: true, Description: "工作区内的目标文件或目录路径。支持相对路径。"},
			{Name: "offset", Type: "integer", Description: "读取文件时的起始行号，1 开始，默认 1。"},
			{Name: "limit", Type: "integer", Description: "读取文件时的最大返回行数，默认 2000。"},
		},
	}
}

// Run 执行读取工具。
func (r *ReadTool) Run(params map[string]interface{}) *ToolResult {
	filePath, errRes := requiredString(params, "filePath")
	if errRes != nil {
		errRes.ToolName = r.Definition().Name
		return errRes
	}
	filePath, pathErr := ensureWorkspacePath(filePath)
	if pathErr != nil {
		pathErr.ToolName = r.Definition().Name
		return pathErr
	}

	offset, errRes := optionalInt(params, "offset", 1)
	if errRes != nil {
		errRes.ToolName = r.Definition().Name
		return errRes
	}
	limit, errRes := optionalInt(params, "limit", 2000)
	if errRes != nil {
		errRes.ToolName = r.Definition().Name
		return errRes
	}
	if offset < 1 {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: "offset 必须 >= 1"}
	}
	if limit < 1 {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: "limit 必须 >= 1"}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("打开文件失败: %v", err)}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("获取文件状态失败: %v", err)}
	}

	if info.IsDir() {
		entries, err := os.ReadDir(filePath)
		if err != nil {
			return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("读取目录失败: %v", err)}
		}
		output := ""
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			output += name + "\n"
		}
		return &ToolResult{ToolName: r.Definition().Name, Success: true, Output: output, Metadata: map[string]interface{}{"filePath": filePath, "entryCount": len(entries), "kind": "directory"}}
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	currentLine := 1
	for scanner.Scan() && currentLine < offset {
		currentLine++
	}
	for scanner.Scan() && len(lines) < limit {
		lines = append(lines, scanner.Text())
		currentLine++
	}
	if err := scanner.Err(); err != nil {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("读取文件错误: %v", err)}
	}

	output := ""
	for i, line := range lines {
		output += fmt.Sprintf("%d: %s\n", offset+i, line)
	}
	return &ToolResult{ToolName: r.Definition().Name, Success: true, Output: output, Metadata: map[string]interface{}{"filePath": filePath, "offset": offset, "limit": limit, "linesReturned": len(lines), "kind": "file"}}
}
