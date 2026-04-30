package decider

import "strings"

// InferTaskKind 通过规则推断任务类型，避免依赖模型分类。
func InferTaskKind(goal string) TaskKind {
	text := strings.ToLower(strings.TrimSpace(goal))
	if text == "" {
		return TaskKindChatAnswer
	}

	hasTodo := containsAny(text, "todo", "待办")
	hasSubAgent := containsAny(text, "subagent", "子代理")
	hasWrite := containsAny(text, "创建文件", "写入", "修改文件", "create file", ".txt", ".go", ".md")
	hasRead := containsAny(text, "读取", "查看", "总结", "分析", "read", "grep", "glob", "list")
	hasTodoAction := containsAny(text, "创建 todo", "更新 todo", "完成 todo", "标记 todo", "todo")

	switch {
	case hasSubAgent && hasWrite:
		return TaskKindSubAgent
	case hasSubAgent:
		return TaskKindSubAgent
	case hasTodo && hasTodoAction && !hasWrite:
		return TaskKindTodoState
	case hasWrite && hasRead:
		return TaskKindMixed
	case hasWrite:
		return TaskKindWorkspaceWrite
	case hasRead:
		return TaskKindReadOnly
	default:
		return TaskKindChatAnswer
	}
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
