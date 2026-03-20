package tools

import (
	"fmt"
	"os"
	"strings"
)

// EditTool 在文件中执行精确的字符串替换。
type EditTool struct{}

// Name 返回工具名称。
func (e *EditTool) Name() string {
	return "edit"
}

// Description 返回工具描述。
func (e *EditTool) Description() string {
	return "在文件中执行精确的字符串替换。工具会自动读取文件内容，执行替换操作，并写回文件。"
}

// Run 执行编辑工具，使用给定的参数。
// 期望的参数：
//   - filePath: 要修改的文件的绝对路径（必需）
//   - oldString: 要替换的文本（必需）
//   - newString: 替换后的文本（必须不同于oldString，必需）
//   - replaceAll: 是否替换所有匹配项（可选，默认：false）
func (e *EditTool) Run(params map[string]interface{}) *ToolResult {
	// 验证必需参数
	filePathParam, ok := params["filePath"]
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "缺少必需参数: filePath",
		}
	}

	filePath, ok := filePathParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "filePath 必须是字符串",
		}
	}

	oldStringParam, ok := params["oldString"]
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "缺少必需参数: oldString",
		}
	}

	oldString, ok := oldStringParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "oldString 必须是字符串",
		}
	}

	newStringParam, ok := params["newString"]
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "缺少必需参数: newString",
		}
	}

	newString, ok := newStringParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "newString 必须是字符串",
		}
	}

	if oldString == newString {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    "newString 必须不同于 oldString",
		}
	}

	// 解析可选的replaceAll参数
	replaceAll := false
	if replaceAllParam, ok := params["replaceAll"]; ok {
		switch v := replaceAllParam.(type) {
		case bool:
			replaceAll = v
		case string:
			if v == "true" || v == "1" {
				replaceAll = true
			} else if v == "false" || v == "0" {
				replaceAll = false
			} else {
				return &ToolResult{
					ToolName: e.Name(),
					Success:  false,
					Error:    "replaceAll 必须是布尔值",
				}
			}
		default:
			return &ToolResult{
				ToolName: e.Name(),
				Success:  false,
				Error:    "replaceAll 必须是布尔值",
			}
		}
	}

	// 读取文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    fmt.Sprintf("读取文件失败: %v", err),
		}
	}

	// 转换为字符串
	fileContent := string(content)

	// 检查oldString是否存在于文件中
	if !strings.Contains(fileContent, oldString) {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    fmt.Sprintf("未在文件中找到要替换的字符串: %q", oldString),
		}
	}

	// 执行替换操作
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(fileContent, oldString, newString)
	} else {
		newContent = strings.Replace(fileContent, oldString, newString, 1) // 只替换第一个匹配项
	}

	// 写回文件
	err = os.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return &ToolResult{
			ToolName: e.Name(),
			Success:  false,
			Error:    fmt.Sprintf("写入文件失败: %v", err),
		}
	}

	// 计算替换了多少次
	var count int
	if replaceAll {
		count = strings.Count(fileContent, oldString)
	} else {
		// 只替换第一个匹配项
		if strings.Contains(fileContent, oldString) {
			count = 1
		} else {
			count = 0
		}
	}

	return &ToolResult{
		ToolName: e.Name(),
		Success:  true,
		Output:   fmt.Sprintf("成功替换 %d 处匹配项", count),
		Metadata: map[string]interface{}{
			"filePath":     filePath,
			"oldString":    oldString,
			"newString":    newString,
			"replaceAll":   replaceAll,
			"replacements": count,
			"bytesWritten": len(newContent),
		},
	}
}
