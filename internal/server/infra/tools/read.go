package tools

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
)

// ReadTool 读取文件内容，支持可选的行范围参数。
type ReadTool struct{}

// Name 返回工具名称。
func (r *ReadTool) Name() string {
	return "read"
}

// Description 返回工具描述。
func (r *ReadTool) Description() string {
	return "从本地文件系统读取文件或目录。支持读取特定行范围。"
}

// Run 执行读取工具，使用给定的参数。
// 期望的参数：
//   - filePath: 要读取的文件或目录的绝对路径（必需）
//   - offset: 开始读取的行号（1-indexed，可选，默认：1）
//   - limit: 最大读取行数（可选，默认：2000）
func (r *ReadTool) Run(params map[string]interface{}) *ToolResult {
	// 验证必需参数
	filePathParam, ok := params["filePath"]
	if !ok {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    "缺少必需参数: filePath",
		}
	}

	filePath, ok := filePathParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    "filePath 必须是字符串",
		}
	}

	// 解析带有默认值的可选参数
	offset := 1
	if offsetParam, ok := params["offset"]; ok {
		switch v := offsetParam.(type) {
		case float64:
			offset = int(v)
		case int:
			offset = v
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				offset = parsed
			} else {
				return &ToolResult{
					ToolName: r.Name(),
					Success:  false,
					Error:    "offset 必须是数字",
				}
			}
		default:
			return &ToolResult{
				ToolName: r.Name(),
				Success:  false,
				Error:    "offset 必须是数字",
			}
		}
	}

	limit := 2000
	if limitParam, ok := params["limit"]; ok {
		switch v := limitParam.(type) {
		case float64:
			limit = int(v)
		case int:
			limit = v
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				limit = parsed
			} else {
				return &ToolResult{
					ToolName: r.Name(),
					Success:  false,
					Error:    "limit 必须是数字",
				}
			}
		default:
			return &ToolResult{
				ToolName: r.Name(),
				Success:  false,
				Error:    "limit 必须是数字",
			}
		}
	}

	// 验证参数
	if offset < 1 {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    "offset 必须 >= 1",
		}
	}

	if limit < 1 {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    "limit 必须 >= 1",
		}
	}

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    fmt.Sprintf("打开文件失败: %v", err),
		}
	}
	defer file.Close()

	// 获取文件信息以检查是否为目录
	info, err := file.Stat()
	if err != nil {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    fmt.Sprintf("获取文件状态失败: %v", err),
		}
	}

	var output string

	if info.IsDir() {
		// 读取目录内容
		files, err := os.ReadDir(filePath)
		if err != nil {
			return &ToolResult{
				ToolName: r.Name(),
				Success:  false,
				Error:    fmt.Sprintf("读取目录失败: %v", err),
			}
		}

		// 格式化目录列表
		for _, f := range files {
			if f.IsDir() {
				output += f.Name() + "/\n"
			} else {
				output += f.Name() + "\n"
			}
		}

		return &ToolResult{
			ToolName: r.Name(),
			Success:  true,
			Output:   output,
		}
	}

	// 按行范围读取文件内容
	var lines []string
	scanner := bufio.NewScanner(file)
	currentLine := 1

	// 跳过直到达到偏移量的行
	for scanner.Scan() && currentLine < offset {
		currentLine++
	}

	// 读取最多limit行
	for scanner.Scan() && len(lines) < limit {
		lines = append(lines, scanner.Text())
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		return &ToolResult{
			ToolName: r.Name(),
			Success:  false,
			Error:    fmt.Sprintf("读取文件错误: %v", err),
		}
	}

	// 格式化输出，包含行号
	for i, line := range lines {
		lineNum := offset + i
		output += fmt.Sprintf("%d: %s\n", lineNum, line)
	}

	return &ToolResult{
		ToolName: r.Name(),
		Success:  true,
		Output:   output,
		Metadata: map[string]interface{}{
			"linesReturned": len(lines),
			"offset":        offset,
			"limit":         limit,
		},
	}
}
