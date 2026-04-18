package memo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

const listToolName = tools.ToolNameMemoList

type listInput struct {
	Scope string `json:"scope,omitempty"`
}

// ListTool 让调用方按层列出持久记忆目录。
type ListTool struct {
	svc *memo.Service
}

// NewListTool 创建 memo_list 工具。
func NewListTool(svc *memo.Service) *ListTool {
	return &ListTool{svc: svc}
}

// Name 返回工具注册名。
func (t *ListTool) Name() string { return listToolName }

// Description 返回工具描述。
func (t *ListTool) Description() string {
	return "List persistent memories grouped by scope. Use this to inspect saved user or project memories."
}

// Schema 返回 JSON Schema 描述的工具参数格式。
func (t *ListTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope": memoScopePropertySchema(),
		},
	}
}

// MicroCompactPolicy 记忆目录结果应保留在上下文中，不参与 micro compact 清理。
func (t *ListTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 执行 memo_list 工具调用。
func (t *ListTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	if t.svc == nil {
		return nilServiceError(listToolName)
	}

	var args listInput
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return invalidArgumentsError(listToolName, err)
		}
	}

	scope, err := parseMemoScope(args.Scope, true)
	if err != nil {
		return tools.NewErrorResult(listToolName, tools.NormalizeErrorReason(listToolName, err), "", nil), err
	}
	entries, err := t.svc.List(ctx, scope)
	if err != nil {
		return tools.NewErrorResult(listToolName, tools.NormalizeErrorReason(listToolName, err), "", nil), err
	}
	if len(entries) == 0 {
		return tools.ToolResult{
			Name:    listToolName,
			Content: "No memos stored yet.",
		}, nil
	}

	var userLines []string
	var projectLines []string
	for _, entry := range entries {
		line := fmt.Sprintf("- [%s] %s", entry.Type, entry.Title)
		if memo.ScopeForType(entry.Type) == memo.ScopeUser {
			userLines = append(userLines, line)
			continue
		}
		projectLines = append(projectLines, line)
	}

	var builder strings.Builder
	if scope == memo.ScopeAll || scope == memo.ScopeUser {
		builder.WriteString("User Memo:\n")
		if len(userLines) == 0 {
			builder.WriteString("- <empty>\n")
		} else {
			builder.WriteString(strings.Join(userLines, "\n"))
			builder.WriteString("\n")
		}
	}
	if scope == memo.ScopeAll || scope == memo.ScopeProject {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("Project Memo:\n")
		if len(projectLines) == 0 {
			builder.WriteString("- <empty>\n")
		} else {
			builder.WriteString(strings.Join(projectLines, "\n"))
			builder.WriteString("\n")
		}
	}

	return tools.ToolResult{
		Name:    listToolName,
		Content: strings.TrimSpace(builder.String()),
	}, nil
}
