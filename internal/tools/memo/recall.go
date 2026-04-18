package memo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

const (
	recallToolName = tools.ToolNameMemoRecall
)

// recallInput 定义 memo_recall 工具的 JSON 入参。
type recallInput struct {
	Keyword string `json:"keyword"`
	Scope   string `json:"scope,omitempty"`
}

// RecallTool 让 Agent 按关键词搜索并加载记忆详情。
type RecallTool struct {
	svc *memo.Service
}

// NewRecallTool 创建 memo_recall 工具，svc 不可为 nil。
func NewRecallTool(svc *memo.Service) *RecallTool {
	return &RecallTool{svc: svc}
}

// Name 返回工具注册名。
func (t *RecallTool) Name() string { return recallToolName }

// Description 返回工具描述，供模型理解工具用途。
func (t *RecallTool) Description() string {
	return "Search and load persistent memory entries by keyword. " +
		"Returns detailed content of matching memory topics. " +
		"Use this to recall user preferences, project decisions, or past feedback."
}

// Schema 返回 JSON Schema 描述的工具参数格式。
func (t *RecallTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"keyword": map[string]any{
				"type":        "string",
				"description": "Search keyword to find matching memory entries (searches title, type, content, and keywords).",
			},
			"scope": memoScopePropertySchema(),
		},
		"required": []string{"keyword"},
	}
}

// MicroCompactPolicy 记忆读取结果应保留在上下文中，不参与 micro compact 清理。
func (t *RecallTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyPreserveHistory
}

// Execute 执行 memo_recall 工具调用。调用前须确保 svc 已通过构造函数注入。
func (t *RecallTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var args recallInput
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return invalidArgumentsError(recallToolName, err)
	}

	args.Keyword = strings.TrimSpace(args.Keyword)
	if args.Keyword == "" {
		err := fmt.Errorf("%s: keyword is required", recallToolName)
		return tools.NewErrorResult(recallToolName, tools.NormalizeErrorReason(recallToolName, err), "", nil), err
	}
	if t.svc == nil {
		return nilServiceError(recallToolName)
	}

	scope, err := parseMemoScope(args.Scope, true)
	if err != nil {
		return tools.NewErrorResult(recallToolName, tools.NormalizeErrorReason(recallToolName, err), "", nil), err
	}

	results, err := t.svc.Recall(ctx, args.Keyword, scope)
	if err != nil {
		return tools.NewErrorResult(recallToolName, tools.NormalizeErrorReason(recallToolName, err), "", nil), err
	}

	if len(results) == 0 {
		return tools.ApplyOutputLimit(tools.ToolResult{
			Name:    recallToolName,
			Content: fmt.Sprintf("No memories found matching %q.", args.Keyword),
		}, tools.DefaultOutputLimitBytes), nil
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "Found %d memory topic(s) matching %q:\n\n", len(results), args.Keyword)
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Scope != results[j].Scope {
			return results[i].Scope < results[j].Scope
		}
		return results[i].Entry.TopicFile < results[j].Entry.TopicFile
	})
	for _, item := range results {
		fmt.Fprintf(&builder, "--- [%s] %s ---\n%s\n\n", item.Scope, item.Entry.TopicFile, item.Content)
	}

	return tools.ApplyOutputLimit(tools.ToolResult{
		Name:    recallToolName,
		Content: builder.String(),
	}, tools.DefaultOutputLimitBytes), nil
}
