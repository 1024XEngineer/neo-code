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
		Description: "Read a file or directory in the workspace. Supports offset/limit pagination for files, returns directory entries for directories.",
		Parameters: []ToolParamSpec{
			{Name: "filePath", Type: "string", Required: true, Description: "Path to the target file or directory in the workspace. Supports relative paths."},
			{Name: "offset", Type: "integer", Description: "Starting line number for reading a file, 1-based.", DefaultValue: 1},
			{Name: "limit", Type: "integer", Description: "Maximum number of lines to return for a file.", DefaultValue: 2000},
		},
	}
}

// Run executes the read tool.
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
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: "offset must be >= 1"}
	}
	if limit < 1 {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: "limit must be >= 1"}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("failed to open file: %v", err)}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("failed to get file status: %v", err)}
	}

	if info.IsDir() {
		entries, err := os.ReadDir(filePath)
		if err != nil {
			return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("failed to read directory: %v", err)}
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
		return &ToolResult{ToolName: r.Definition().Name, Success: false, Error: fmt.Sprintf("error reading file: %v", err)}
	}

	output := ""
	for i, line := range lines {
		output += fmt.Sprintf("%d: %s\n", offset+i, line)
	}
	return &ToolResult{ToolName: r.Definition().Name, Success: true, Output: output, Metadata: map[string]interface{}{"filePath": filePath, "offset": offset, "limit": limit, "linesReturned": len(lines), "kind": "file"}}
}
