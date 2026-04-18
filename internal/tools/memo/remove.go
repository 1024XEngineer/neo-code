package memo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

const removeToolName = tools.ToolNameMemoRemove

type removeInput struct {
	Keyword string `json:"keyword"`
	Scope   string `json:"scope,omitempty"`
}

// RemoveTool 让调用方按关键词删除持久记忆。
type RemoveTool struct {
	svc *memo.Service
}

// NewRemoveTool 创建 memo_remove 工具。
func NewRemoveTool(svc *memo.Service) *RemoveTool {
	return &RemoveTool{svc: svc}
}

// Name 返回工具注册名。
func (t *RemoveTool) Name() string { return removeToolName }

// Description 返回工具描述。
func (t *RemoveTool) Description() string {
	return "Remove persistent memories by keyword. Optionally limit deletion scope to user or project memories."
}

// Schema 返回 JSON Schema 描述的工具参数格式。
func (t *RemoveTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"keyword": map[string]any{
				"type":        "string",
				"description": "Keyword to match against memory title, type, keywords, or content.",
			},
			"scope": memoScopePropertySchema(),
		},
		"required": []string{"keyword"},
	}
}

// MicroCompactPolicy 删除结果应保留在上下文中，不参与 micro compact 清理。
func (t *RemoveTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 执行 memo_remove 工具调用。
func (t *RemoveTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var args removeInput
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return invalidArgumentsError(removeToolName, err)
	}
	if t.svc == nil {
		return nilServiceError(removeToolName)
	}

	args.Keyword = strings.TrimSpace(args.Keyword)
	if args.Keyword == "" {
		err := fmt.Errorf("%s: keyword is required", removeToolName)
		return tools.NewErrorResult(removeToolName, tools.NormalizeErrorReason(removeToolName, err), "", nil), err
	}

	scope, err := parseMemoScope(args.Scope, true)
	if err != nil {
		return tools.NewErrorResult(removeToolName, tools.NormalizeErrorReason(removeToolName, err), "", nil), err
	}

	removed, err := t.svc.Remove(ctx, args.Keyword, scope)
	if err != nil {
		return tools.NewErrorResult(removeToolName, tools.NormalizeErrorReason(removeToolName, err), "", nil), err
	}
	if removed == 0 {
		return tools.ToolResult{
			Name:    removeToolName,
			Content: fmt.Sprintf("No memos matching %q.", args.Keyword),
		}, nil
	}
	return tools.ToolResult{
		Name:    removeToolName,
		Content: fmt.Sprintf("Removed %d memo(s) matching %q.", removed, args.Keyword),
	}, nil
}
