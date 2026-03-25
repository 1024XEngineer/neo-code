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
		Description: "Perform precise string replacement in a file within the workspace. By default, replaces only the first occurrence, controlled by replaceAll.",
		Parameters: []ToolParamSpec{
			{Name: "filePath", Type: "string", Required: true, Description: "Path to the file to modify within the workspace."},
			{Name: "oldString", Type: "string", Required: true, Description: "The original text to replace, must match file content exactly."},
			{Name: "newString", Type: "string", Required: true, Description: "The new text to replace with, must be different from oldString."},
			{Name: "replaceAll", Type: "boolean", Description: "Whether to replace all occurrences.", DefaultValue: false},
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
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: "newString must be different from oldString"}
	}
	replaceAll, errRes := optionalBool(params, "replaceAll", false)
	if errRes != nil {
		errRes.ToolName = e.Definition().Name
		return errRes
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("failed to read file: %v", err)}
	}
	fileContent := string(content)
	if !strings.Contains(fileContent, oldString) {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("oldString not found in file: %q", oldString)}
	}

	newContent := strings.Replace(fileContent, oldString, newString, 1)
	replacements := 1
	if replaceAll {
		replacements = strings.Count(fileContent, oldString)
		newContent = strings.ReplaceAll(fileContent, oldString, newString)
	}
	if err := AtomicWrite(filePath, []byte(newContent)); err != nil {
		return &ToolResult{ToolName: e.Definition().Name, Success: false, Error: fmt.Sprintf("failed to write file: %v", err)}
	}
	return &ToolResult{ToolName: e.Definition().Name, Success: true, Output: fmt.Sprintf("Successfully replaced %d match(es)", replacements), Metadata: map[string]interface{}{"filePath": filePath, "oldString": oldString, "newString": newString, "replaceAll": replaceAll, "replacements": replacements, "bytesWritten": len(newContent)}}
}
