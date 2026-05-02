package decider

import (
	"strings"

	runtimefacts "neo-code/internal/runtime/facts"
)

// InferTaskKind 通过规则推断任务类型，避免依赖模型分类。
func InferTaskKind(goal string) TaskKind {
	return InferTaskIntent(goal).Hint
}

// InferTaskIntent 基于用户文本推断弱意图，仅作 hint，不作为强验收依据。
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
	hasFileTarget := containsAny(text, ".txt", ".go", ".md", ".json", ".yaml", ".yml", ".ts", ".tsx")
	hasNamedWriteTarget := containsAny(text, "readme", "package.json")
	hasWriteIntentToken := containsAny(text, "创建", "写", "改", "补", "edit", "write", "update", "create", "modify")
	hasWrite := hasWriteVerb || ((hasFileTarget || hasNamedWriteTarget) && hasWriteIntentToken)
	hasRead := containsAny(
		text,
		"读取", "查看", "看看", "总结", "分析", "检索", "搜索", "审查", "review", "verify", "验证", "校验", "怎么修",
		"read", "grep", "glob", "list", "inspect", "analyze", "summarize",
	)
	hasReviewIntent := containsAny(text, "review", "审查", "总结", "分析", "analyze", "summarize")
	hasPlan := containsAny(text, "计划", "规划", "plan", "todo 列表", "todo list")
	hasTodoAction := containsAny(text, "创建 todo", "更新 todo", "完成 todo", "标记 todo", "todo")

	intent := TaskIntent{Hint: TaskKindChatAnswer, Confidence: 0.35, Reasons: []string{"fallback_chat"}}
	switch {
	case hasSubAgent && hasWrite:
		intent.Hint = TaskKindSubAgent
		intent.Confidence = 0.9
		intent.Reasons = []string{"subagent_keyword", "write_intent"}
	case hasSubAgent:
		intent.Hint = TaskKindSubAgent
		intent.Confidence = 0.82
		intent.Reasons = []string{"subagent_keyword"}
	case hasTodo && hasTodoAction:
		intent.Hint = TaskKindTodoState
		intent.Confidence = 0.78
		intent.Reasons = []string{"todo_action_keyword"}
	case hasPlan && hasTodo && !hasWrite:
		intent.Hint = TaskKindTodoState
		intent.Confidence = 0.84
		intent.Reasons = []string{"todo_keyword", "plan_keyword"}
	case hasWrite && hasReviewIntent:
		intent.Hint = TaskKindMixed
		intent.Confidence = 0.75
		intent.Reasons = []string{"write_intent", "read_intent"}
	case hasWrite:
		intent.Hint = TaskKindWorkspaceWrite
		intent.Confidence = 0.72
		intent.Reasons = []string{"write_intent"}
	case hasRead:
		intent.Hint = TaskKindReadOnly
		intent.Confidence = 0.7
		intent.Reasons = []string{"read_intent"}
	}
	return intent
}

// DeriveEffectiveTaskKind 基于运行事实修正任务类型；文本 hint 仅作回退。
func DeriveEffectiveTaskKind(hint TaskKind, allFacts runtimefacts.RuntimeFacts, todos TodoSnapshot) TaskKind {
	hasWrite := len(allFacts.Files.Written) > 0 || len(allFacts.Files.ContentMatch) > 0
	if !hasWrite && hint == TaskKindWorkspaceWrite && hasArtifactVerificationPassed(allFacts) {
		hasWrite = true
	}
	hasVerification := len(allFacts.Verification.Passed) > 0
	hasSubAgent := len(allFacts.SubAgents.Started) > 0 || len(allFacts.SubAgents.Completed) > 0 || len(allFacts.SubAgents.Failed) > 0
	hasTodo := todos.Summary.Total > 0 || len(allFacts.Todos.CreatedIDs) > 0 || len(allFacts.Todos.CompletedIDs) > 0 || len(allFacts.Todos.FailedIDs) > 0
	hasRead := len(allFacts.Files.Exists) > 0 || len(allFacts.Commands.Executed) > 0

	switch {
	case hasSubAgent && (hasWrite || hasTodo || hasVerification):
		return TaskKindMixed
	case hasSubAgent:
		return TaskKindSubAgent
	case hasWrite && (hasTodo || hasVerification || hasRead):
		return TaskKindWorkspaceWrite
	case hasWrite:
		return TaskKindWorkspaceWrite
	case hasTodo && !hasWrite:
		return TaskKindTodoState
	case hasRead || hasVerification:
		return TaskKindReadOnly
	case strings.TrimSpace(string(hint)) != "":
		return hint
	default:
		return TaskKindChatAnswer
	}
}

// hasArtifactVerificationPassed 判断是否存在与产物路径绑定的通过验证事实。
func hasArtifactVerificationPassed(allFacts runtimefacts.RuntimeFacts) bool {
	for _, fact := range allFacts.Verification.Passed {
		scope := strings.TrimSpace(fact.Scope)
		if strings.HasPrefix(strings.ToLower(scope), "artifact:") {
			return true
		}
	}
	return false
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
