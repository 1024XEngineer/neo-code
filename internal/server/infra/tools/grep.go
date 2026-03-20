package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// GrepTool 使用正则表达式搜索文件内容。
type GrepTool struct{}

// Name 返回工具名称。
func (g *GrepTool) Name() string {
	return "grep"
}

// Description 返回工具描述。
func (g *GrepTool) Description() string {
	return "使用正则表达式搜索文件内容。返回至少有一个匹配项的文件路径和行号。"
}

// Run 执行grep工具，使用给定的参数。
// 期望的参数：
//   - pattern: 要在文件内容中搜索的正则表达式模式（必需）
//   - path: 要搜索的目录（可选，默认：当前工作目录）
//   - include: 要包含在搜索中的文件模式（例如："*.js"，"*.{ts,tsx}"）（可选）
func (g *GrepTool) Run(params map[string]interface{}) *ToolResult {
	// 验证必需参数
	patternParam, ok := params["pattern"]
	if !ok {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  false,
			Error:    "缺少必需参数: pattern",
		}
	}

	pattern, ok := patternParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  false,
			Error:    "pattern 必须是字符串",
		}
	}

	// 编译正则表达式模式
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  false,
			Error:    fmt.Sprintf("无效的正则表达式模式: %v", err),
		}
	}

	// 解析可选的path参数
	searchPath := "." // 默认为当前目录
	if pathParam, ok := params["path"]; ok {
		searchPath, ok = pathParam.(string)
		if !ok {
			return &ToolResult{
				ToolName: g.Name(),
				Success:  false,
				Error:    "path 必须是字符串",
			}
		}
	}

	// 解析可选的include参数
	var includePattern string
	if includeParam, ok := params["include"]; ok {
		includePattern, ok = includeParam.(string)
		if !ok {
			return &ToolResult{
				ToolName: g.Name(),
				Success:  false,
				Error:    "include 必须是字符串",
			}
		}
	}

	// 遍历文件系统
	var results string
	var walkErr error
	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			walkErr = err
			return filepath.SkipDir
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 检查文件是否匹配include模式（使用filepath.Match进行glob模式匹配）
		if includePattern != "" {
			matched, err := filepath.Match(includePattern, filepath.Base(path))
			if err != nil {
				walkErr = err
				return filepath.SkipDir
			}
			if !matched {
				return nil
			}
		}

		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			// 跳过我们无法读取的文件
			return nil
		}

		// 搜索匹配项
		matches := regex.FindAllIndex(content, -1)
		if len(matches) == 0 {
			return nil
		}

		// 格式化结果，包含行号
		lines := regexp.MustCompile("\r?\n").Split(string(content), -1)
		lineCount := 0
		var lineNum int
		foundInFile := false

		for i, line := range lines {
			lineCount += len(line) + 1     // +1 for newline
			if lineCount > matches[0][0] { // 我们已经超过了第一个匹配位置
				lineNum = i + 1
				foundInFile = true
				break
			}
		}

		if foundInFile {
			results += fmt.Sprintf("%s:%d\n", path, lineNum)
		}

		return nil
	})

	// 检查我们在遍历过程中是否遇到了错误
	if walkErr != nil {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  false,
			Error:    walkErr.Error(),
		}
	}

	if err != nil {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  false,
			Error:    err.Error(),
		}
	}

	// 如果没有找到结果
	if results == "" {
		return &ToolResult{
			ToolName: g.Name(),
			Success:  true,
			Output:   "未找到匹配项。",
			Metadata: map[string]interface{}{
				"pattern": pattern,
				"path":    searchPath,
				"include": includePattern,
			},
		}
	}

	return &ToolResult{
		ToolName: g.Name(),
		Success:  true,
		Output:   results,
		Metadata: map[string]interface{}{
			"pattern": pattern,
			"path":    searchPath,
			"include": includePattern,
			"matches": len(results),
		},
	}
}
