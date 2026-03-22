package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

type WriteTool struct{}

func (w *WriteTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write",
		Description: "在工作区内写入整个文件内容。若父目录不存在则自动创建。",
		Parameters: []ToolParamSpec{
			{Name: "filePath", Type: "string", Required: true, Description: "工作区内目标文件路径。"},
			{Name: "content", Type: "string", Required: true, Description: "将完整写入文件的新内容。"},
		},
	}
}

func (w *WriteTool) Run(params map[string]interface{}) *ToolResult {
	filePath, errRes := requiredString(params, "filePath")
	if errRes != nil {
		errRes.ToolName = w.Definition().Name
		return errRes
	}
	filePath, pathErr := ensureWorkspacePath(filePath)
	if pathErr != nil {
		pathErr.ToolName = w.Definition().Name
		return pathErr
	}
	content, errRes := requiredString(params, "content")
	if errRes != nil {
		errRes.ToolName = w.Definition().Name
		return errRes
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return &ToolResult{ToolName: w.Definition().Name, Success: false, Error: fmt.Sprintf("创建目录失败: %v", err)}
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return &ToolResult{ToolName: w.Definition().Name, Success: false, Error: fmt.Sprintf("写入文件失败: %v", err)}
	}
	return &ToolResult{ToolName: w.Definition().Name, Success: true, Output: fmt.Sprintf("成功写入 %s", filePath), Metadata: map[string]interface{}{"filePath": filePath, "bytesWritten": len(content)}}
}
