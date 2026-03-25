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
		Description: "List directory contents in the workspace. One entry per line, subdirectories suffixed with '/'.",
		Parameters:  []ToolParamSpec{{Name: "path", Type: "string", Description: "Directory within the workspace to list. Defaults to the workspace root.", DefaultValue: "."}},
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
		return &ToolResult{ToolName: l.Definition().Name, Success: false, Error: fmt.Sprintf("failed to open directory: %v", err)}
	}
	defer file.Close()
	entries, err := file.Readdir(-1)
	if err != nil {
		return &ToolResult{ToolName: l.Definition().Name, Success: false, Error: fmt.Sprintf("failed to read directory: %v", err)}
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
