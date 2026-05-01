package decider

import "strings"

// InferTaskKind 通过规则推断任务类型，避免依赖模型分类。
func InferTaskKind(goal string) TaskKind {
	return InferTaskIntent(goal).Hint
}

// InferTaskIntent 基于文本推导弱意图，仅作为后续事实修正前的初始提示。
func InferTaskIntent(goal string) TaskIntent {
	text := strings.ToLower(strings.TrimSpace(goal))
	if text == "" {
		return TaskIntent{Hint: TaskKindChatAnswer, Confidence: 0.2, Reasons: []string{"empty_goal"}}
	}

	hasTodo := containsAny(text, "todo", "待办")
	hasSubAgent := containsAny(text, "subagent", "子代理")
	hasWriteVerb := containsAny(
		text,
		"创建文件", "写入", "修改文件", "编辑文件", "新增文件", "补丁", "修复代码",
		"create file", "write file", "edit file", "update file", "apply patch",
	)
	hasFileTarget := containsAny(text, ".txt", ".go", ".md", ".json", ".yaml", ".yml", ".ts", ".tsx", "readme")
	hasWriteIntentToken := containsAny(text, "创建", "写", "改", "补", "edit", "write", "update", "create", "modify")
	hasWrite := hasWriteVerb || (hasFileTarget && hasWriteIntentToken)
	readSignalText := strings.ReplaceAll(text, "readme", "")
	hasRead := containsAny(
		readSignalText,
		"读取", "查看", "总结", "分析", "检索", "搜索", "审查", "review", "verify", "验证", "校验",
		"read", "grep", "glob", "list", "inspect", "analyze", "summarize", "看看", "bug",
	)
	hasPlan := containsAny(text, "计划", "规划", "plan", "todo 列表", "todo list")
	hasTodoAction := containsAny(text, "创建 todo", "更新 todo", "完成 todo", "标记 todo", "todo")

	intent := TaskIntent{Hint: TaskKindChatAnswer, Confidence: 0.4, Reasons: []string{"default_chat_fallback"}}
	switch {
	case hasTodo && hasTodoAction && !hasSubAgent:
		intent.Hint = TaskKindTodoState
		intent.Confidence = 0.75
		intent.Reasons = []string{"todo_action_priority"}
	case hasSubAgent && hasWrite:
		intent.Hint = TaskKindSubAgent
		intent.Confidence = 0.8
		intent.Reasons = []string{"explicit_subagent", "write_signal"}
	case hasSubAgent:
		intent.Hint = TaskKindSubAgent
		intent.Confidence = 0.85
		intent.Reasons = []string{"explicit_subagent"}
	case hasPlan && hasTodo && !hasWrite:
		intent.Hint = TaskKindTodoState
		intent.Confidence = 0.8
		intent.Reasons = []string{"todo_plan_without_write"}
	case hasWrite && hasRead:
		intent.Hint = TaskKindMixed
		intent.Confidence = 0.75
		intent.Reasons = []string{"write_and_read_signals"}
	case hasWrite:
		intent.Hint = TaskKindWorkspaceWrite
		intent.Confidence = 0.75
		intent.Reasons = []string{"write_signal"}
	case hasRead:
		intent.Hint = TaskKindReadOnly
		intent.Confidence = 0.7
		intent.Reasons = []string{"read_signal"}
	default:
		intent.Hint = TaskKindChatAnswer
		intent.Confidence = 0.45
		intent.Reasons = []string{"no_strong_signal"}
	}
	return intent
}

// containsAny 判断文本是否包含任一关键词。
func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(keyword))) {
			return true
		}
	}
	return false
}
