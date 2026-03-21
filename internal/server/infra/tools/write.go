package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

type WriteTool struct{}

// Name 返回工具名称。
func (w *WriteTool) Name() string {
	return "write"
}

// Description 返回面向用户的工具说明。
func (w *WriteTool) Description() string {
	return "Write a file to the local filesystem.写入文件"
}

// Run 将给定内容写入目标文件路径。
func (w *WriteTool) Run(params map[string]interface{}) *ToolResult {
	// 主要逻辑
	filePathParam, ok := params["filePath"]
	if !ok {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    "缺少必填参数: filePath",
		}
	}

	filePath, ok := filePathParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    "filePath 必须是字符串",
		}
	}

	contentParam, ok := params["content"]
	if !ok {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    "缺少必填参数: content",
		}
	}

	content, ok := contentParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    "content 必须是字符串",
		}
	}

	// 创建目录（如果不存在）
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    fmt.Sprintf("创建目录失败: %v", err),
		}
	}

	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return &ToolResult{
			ToolName: w.Name(),
			Success:  false,
			Error:    fmt.Sprintf("写入文件失败: %v", err),
		}
	}

	return &ToolResult{
		ToolName: w.Name(),
		Success:  true,
		Output:   fmt.Sprintf("成功写入 %s", filePath),
		Metadata: map[string]interface{}{
			"bytesWritten": len(content),
			"filePath":     filePath,
		},
	}
}
