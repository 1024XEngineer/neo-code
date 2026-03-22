package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepTool 使用正则表达式搜索文件内容。
type GrepTool struct{}

func (g *GrepTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "grep",
		Description: "在工作区内递归搜索文件内容并返回所有命中的文件、行号和行文本。",
		Parameters: []ToolParamSpec{
			{Name: "pattern", Type: "string", Required: true, Description: "要搜索的正则表达式。"},
			{Name: "path", Type: "string", Description: "工作区内的搜索根目录，默认当前工作区。"},
			{Name: "include", Type: "string", Description: "可选的文件名 glob 过滤，例如 '*.go'。"},
		},
	}
}

func (g *GrepTool) Run(params map[string]interface{}) *ToolResult {
	pattern, errRes := requiredString(params, "pattern")
	if errRes != nil {
		errRes.ToolName = g.Definition().Name
		return errRes
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return &ToolResult{ToolName: g.Definition().Name, Success: false, Error: fmt.Sprintf("无效的正则表达式模式: %v", err)}
	}
	searchPath, errRes := optionalString(params, "path", ".")
	if errRes != nil {
		errRes.ToolName = g.Definition().Name
		return errRes
	}
	searchPath, pathErr := ensureWorkspacePath(searchPath)
	if pathErr != nil {
		pathErr.ToolName = g.Definition().Name
		return pathErr
	}
	includePattern, errRes := optionalString(params, "include", "")
	if errRes != nil {
		errRes.ToolName = g.Definition().Name
		return errRes
	}

	var lines []string
	matchCount := 0
	walkErr := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if includePattern != "" {
			matched, matchErr := filepath.Match(includePattern, filepath.Base(path))
			if matchErr != nil {
				return matchErr
			}
			if !matched {
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fileLines := regexp.MustCompile("\\r?\\n").Split(string(content), -1)
		for idx, line := range fileLines {
			if regex.MatchString(line) {
				matchCount++
				lines = append(lines, fmt.Sprintf("%s:%d:%s", path, idx+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if walkErr != nil {
		return &ToolResult{ToolName: g.Definition().Name, Success: false, Error: walkErr.Error()}
	}
	if matchCount == 0 {
		return &ToolResult{ToolName: g.Definition().Name, Success: true, Output: "未找到匹配项。", Metadata: map[string]interface{}{"pattern": pattern, "path": searchPath, "include": includePattern, "matches": 0}}
	}
	return &ToolResult{ToolName: g.Definition().Name, Success: true, Output: strings.Join(lines, "\n") + "\n", Metadata: map[string]interface{}{"pattern": pattern, "path": searchPath, "include": includePattern, "matches": matchCount}}
}
