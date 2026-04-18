package memo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

const (
	rememberToolName = tools.ToolNameMemoRemember
)

// rememberInput 定义 memo_remember 工具的 JSON 入参。
type rememberInput struct {
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Keywords []string `json:"keywords,omitempty"`
}

// RememberTool 让 Agent 主动保存跨会话记忆条目。
type RememberTool struct {
	svc *memo.Service
}

// NewRememberTool 创建 memo_remember 工具，svc 不可为 nil。
func NewRememberTool(svc *memo.Service) *RememberTool {
	return &RememberTool{svc: svc}
}

// Name 返回工具注册名。
func (t *RememberTool) Name() string { return rememberToolName }

// Description 返回工具描述，供模型理解工具用途。
func (t *RememberTool) Description() string {
	return "Save a persistent memory entry that will be available across sessions. " +
		"Use this to remember user preferences, project decisions, feedback, or important facts."
}

// Schema 返回 JSON Schema 描述的工具参数格式。
func (t *RememberTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type": map[string]any{
				"type":        "string",
				"description": "Memory type: user (preferences/profile), feedback (corrections/guidance), project (facts/decisions), reference (external resources)",
				"enum":        []string{"user", "feedback", "project", "reference"},
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Short summary of the memory (~150 chars), displayed in the memory index.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full memory content with relevant details. Include context like 'Why' and 'How to apply' when applicable.",
			},
			"keywords": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional search keywords for later retrieval.",
			},
		},
		"required": []string{"type", "title", "content"},
	}
}

// MicroCompactPolicy 记忆写入结果应保留在上下文中，不参与 micro compact 清理。
func (t *RememberTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 执行 memo_remember 工具调用。调用前须确保 svc 已通过构造函数注入。
func (t *RememberTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var args rememberInput
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return invalidArgumentsError(rememberToolName, err)
	}

	args.Type = strings.TrimSpace(args.Type)
	args.Title = strings.TrimSpace(args.Title)
	args.Content = strings.TrimSpace(args.Content)

	if args.Type == "" || args.Title == "" || args.Content == "" {
		err := fmt.Errorf("%s: type, title, and content are required", rememberToolName)
		return tools.NewErrorResult(rememberToolName, tools.NormalizeErrorReason(rememberToolName, err), "", nil), err
	}

	memoType := memo.Type(args.Type)
	if !memo.IsValidType(memoType) {
		err := fmt.Errorf("%s: invalid type %q, must be one of user/feedback/project/reference", rememberToolName, args.Type)
		return tools.NewErrorResult(rememberToolName, tools.NormalizeErrorReason(rememberToolName, err), "", nil), err
	}
	if t.svc == nil {
		return nilServiceError(rememberToolName)
	}

	title := memo.NormalizeTitle(args.Title)
	entry := memo.Entry{
		Type:     memoType,
		Title:    title,
		Content:  args.Content,
		Keywords: args.Keywords,
		Source:   memo.SourceToolInitiated,
	}

	if err := t.svc.Add(ctx, entry); err != nil {
		return tools.NewErrorResult(rememberToolName, tools.NormalizeErrorReason(rememberToolName, err), "", nil), err
	}

	return tools.ToolResult{
		Name:    rememberToolName,
		Content: fmt.Sprintf("Memory saved: [%s] %s", memoType, title),
	}, nil
}
