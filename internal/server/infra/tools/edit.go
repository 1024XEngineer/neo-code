package tools

import (
	"fmt"
	"os"
	"strings"
)

// EditTool 在文件中执行精确的字符串替换。
type EditTool struct{}

func (e *EditTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "edit",
		Description: "在工作区内对文件执行精确字符串替换。默认只替换首次命中，可通过 replaceAll 控制。",
		Parameters: []ToolParamSpec{
			{Name: "filePath", Type: "string", Required: true, Description: "工作区内要修改的文件路径。"},
			{Name: "oldString", Type: "string", Required: true, Description: "要替换的原始文本，必须与文件内容完全匹配。"},
			{Name: "newString", Type: "string", Required: true, Description: "替换后的新文本，必须不同于 oldString。"},
			{Name: "replaceAll", Type: "boolean", Description: "是否替换所有命中，默认 false。"},
		},
	}
}

func (e *EditTool) Run(params map[string]interface{}) *ToolResult {
	filePath, errRes := requiredString(params, "filePath")
	if errRes != nil {
		errRes.ToolName = e.Definition().Name
		return errRes
	}
	filePath, pathErr := ensureWorkspacePath(filePath)
	if pathErr != nil {
		pathErr.ToolName = e.Definition().Name
		return pathErr
	}
	oldString, errRes := requiredString(params, "oldString")
	if errRes != nil {
		errRes.ToolName = e.Definition().Name
		return errRes
	}
	newString, errRes := requiredString(params, "newString")
	if errRes != nil {
		errRes.ToolName = e.Definition().Name
		return errRes
	}
	if oldString == newString {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: "newString 必须不同于 oldString"}
	}
	replaceAll, errRes := optionalBool(params, "replaceAll", false)
	if errRes != nil {
		errRes.ToolName = e.Definition().Name
		return errRes
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("读取文件失败: %v", err)}
	}
	fileContent := string(content)
	if !strings.Contains(fileContent, oldString) {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("未在文件中找到要替换的字符串: %q", oldString)}
	}

	newContent := strings.Replace(fileContent, oldString, newString, 1)
	replacements := 1
	if replaceAll {
		replacements = strings.Count(fileContent, oldString)
		newContent = strings.ReplaceAll(fileContent, oldString, newString)
	}
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("写入文件失败: %v", err)}
	}
	return &ToolResult{ToolName: e.Definition().Name, Success: true, Output: fmt.Sprintf("成功替换 %d 处匹配项", replacements), Metadata: map[string]interface{}{"filePath": filePath, "oldString": oldString, "newString": newString, "replaceAll": replaceAll, "replacements": replacements, "bytesWritten": len(newContent)}}
}
