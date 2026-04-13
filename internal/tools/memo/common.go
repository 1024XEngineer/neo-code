package memo

import (
	"fmt"

	"neo-code/internal/tools"
)

// nilServiceError 构造 memo 工具缺少 service 依赖时的统一错误结果。
func nilServiceError(toolName string) (tools.ToolResult, error) {
	err := fmt.Errorf("%s: service is nil", toolName)
	return tools.NewErrorResult(toolName, tools.NormalizeErrorReason(toolName, err), "", nil), err
}
