package decider

import (
	"fmt"
	"strings"
)

// Decide 执行最终终态裁决，作为 runtime 的唯一决策入口。
func Decide(input DecisionInput) Decision {
	if input.Todos.Summary.RequiredFailed > 0 {
		return Decision{
			Status:             DecisionFailed,
			StopReason:         "required_todo_failed",
			UserVisibleSummary: "存在 required todo 失败，任务已终止。",
			InternalSummary:    "required todo entered failed terminal state",
		}
	}
	if input.NoProgressExceeded {
		return Decision{
			Status:             DecisionIncomplete,
			StopReason:         "no_progress_after_final_intercept",
			UserVisibleSummary: "连续多轮缺少新事实，任务以未完成结束。",
			InternalSummary:    "no progress exceeded while final intercepted",
		}
	}
	if !input.CompletionPassed {
		return continueWithCompletionReason(input)
	}

	switch input.TaskKind {
	case TaskKindTodoState:
		return decideTodoState(input)
	case TaskKindWorkspaceWrite:
		return decideWorkspaceWrite(input)
	case TaskKindSubAgent:
		return decideSubAgent(input)
	case TaskKindReadOnly:
		return decideReadOnly(input)
	case TaskKindMixed:
		return decideMixed(input)
	case TaskKindChatAnswer:
		fallthrough
	default:
		return Decision{
			Status:             DecisionAccepted,
			StopReason:         "accepted",
			UserVisibleSummary: "任务完成。",
			InternalSummary:    "chat answer accepted by completion gate",
		}
	}
}

// continueWithCompletionReason 把 completion gate 阻塞转成可执行缺失事实提示。
func continueWithCompletionReason(input DecisionInput) Decision {
	reason := strings.TrimSpace(input.CompletionReason)
	switch reason {
	case "pending_todo":
		openTodos := collectOpenRequiredTodos(input.Todos.Items)
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:    "required_todo_terminal",
				Target:  strings.Join(openTodos, ","),
				Details: map[string]any{"open_required_ids": openTodos},
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "set_status",
					"id":     firstOrEmpty(openTodos),
					"status": "completed",
				},
			}},
			UserVisibleSummary: "仍有 required todo 未收敛，需要继续推进 todo 状态。",
			InternalSummary:    "completion blocked by pending_todo",
		}
	case "unverified_write":
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "verification_passed",
				Target: "workspace_write",
			}},
			RequiredNextActions: []RequiredAction{
				{
					Tool: "filesystem_read_file",
					ArgsHint: map[string]any{
						"path":               "<artifact-path>",
						"expect_contains":    []string{"<expected-token>"},
						"verification_scope": "artifact:<path>",
					},
				},
				{
					Tool: "filesystem_glob",
					ArgsHint: map[string]any{
						"pattern":            "<artifact-pattern>",
						"expect_min_matches": 1,
						"verification_scope": "artifact:<pattern>",
					},
				},
			},
			UserVisibleSummary: "写入事实尚未完成验证，需要补充 verification facts。",
			InternalSummary:    "completion blocked by unverified_write",
		}
	case "post_execute_closure_required":
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "post_execute_closure",
				Target: "latest_tool_results",
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "update",
					"id":     "<todo-id>",
				},
			}},
			UserVisibleSummary: "请先基于最新工具结果完成闭环，再尝试最终收尾。",
			InternalSummary:    "completion blocked by post_execute_closure_required",
		}
	default:
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "仍缺少可验证事实，请继续调用工具推进任务。",
			InternalSummary:    "completion gate blocked without classified reason",
		}
	}
}

// decideTodoState 依据 todo 快照判定状态类任务。
func decideTodoState(input DecisionInput) Decision {
	if input.Todos.Summary.Total == 0 && len(input.Facts.Todos.CreatedIDs) == 0 {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "todo_created",
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "add",
					"item": map[string]any{
						"id":      "todo-1",
						"content": "<todo content>",
					},
				},
			}},
			UserVisibleSummary: "尚未创建目标 Todo，请先调用 todo_write。",
			InternalSummary:    "todo_state task missing created todo facts",
		}
	}
	if input.Todos.Summary.RequiredOpen > 0 {
		openIDs := collectOpenRequiredTodos(input.Todos.Items)
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:    "required_todo_terminal",
				Target:  strings.Join(openIDs, ","),
				Details: map[string]any{"open_required_ids": openIDs},
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "set_status",
					"id":     firstOrEmpty(openIDs),
					"status": "completed",
				},
			}},
			UserVisibleSummary: "Todo 已创建但 required 项仍未完成。",
			InternalSummary:    "todo_state task still has open required todos",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "Todo 状态已满足任务目标。",
		InternalSummary:    "todo_state facts satisfied",
	}
}

// decideWorkspaceWrite 依据写入与验证事实判定文件任务。
func decideWorkspaceWrite(input DecisionInput) Decision {
	if len(input.Facts.Files.Written) == 0 {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "file_written",
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "filesystem_write_file",
				ArgsHint: map[string]any{
					"path":    "target.txt",
					"content": "<expected content>",
				},
			}},
			UserVisibleSummary: "还没有写入事实，请先执行文件写入。",
			InternalSummary:    "workspace_write task missing file_written fact",
		}
	}
	if len(input.Facts.Verification.Passed) == 0 {
		target := input.Facts.Files.Written[0].Path
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "verification_passed",
				Target: target,
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "filesystem_read_file",
				ArgsHint: map[string]any{
					"path":               target,
					"expect_contains":    []string{"<expected-token>"},
					"verification_scope": fmt.Sprintf("artifact:%s", target),
				},
			}},
			UserVisibleSummary: "已写入文件但尚未形成通过的验证事实。",
			InternalSummary:    "workspace_write task missing verification passed facts",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "文件写入与验证事实已满足。",
		InternalSummary:    "workspace_write facts satisfied",
	}
}

// decideSubAgent 依据子代理启动/完成事实判定子代理任务。
func decideSubAgent(input DecisionInput) Decision {
	if len(input.Facts.SubAgents.Started) == 0 {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "subagent_started",
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "spawn_subagent",
				ArgsHint: map[string]any{
					"task_type":     "review",
					"role":          "reviewer",
					"content":       "<task instruction>",
					"allowed_paths": []string{"."},
				},
			}},
			UserVisibleSummary: "尚未产生子代理启动事实，请显式调用 spawn_subagent。",
			InternalSummary:    "subagent task missing start fact",
		}
	}
	if len(input.Facts.SubAgents.Failed) > 0 && len(input.Facts.SubAgents.Completed) == 0 {
		return Decision{
			Status:             DecisionFailed,
			StopReason:         "verification_failed",
			UserVisibleSummary: "子代理执行失败，任务终止。",
			InternalSummary:    "subagent task failed without completion fact",
		}
	}
	if len(input.Facts.SubAgents.Completed) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "子代理已启动但尚未完成。",
			InternalSummary:    "subagent task started but no completed fact",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "子代理完成事实已满足。",
		InternalSummary:    "subagent task completed facts satisfied",
	}
}

// decideReadOnly 判定只读任务是否可结束。
func decideReadOnly(input DecisionInput) Decision {
	if len(input.Facts.Files.Exists) == 0 && len(input.Facts.Commands.Executed) == 0 && len(input.LastAssistantText) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "尚无可验证读取事实，请先执行只读工具。",
			InternalSummary:    "read_only task has no read/search facts",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "只读分析任务已完成。",
		InternalSummary:    "read_only facts satisfied",
	}
}

// decideMixed 对混合任务采用保守策略：必须同时具备状态推进与至少一个验证事实。
func decideMixed(input DecisionInput) Decision {
	if len(input.Facts.Verification.Passed) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "混合任务尚未形成验证通过事实。",
			InternalSummary:    "mixed task missing verification passed facts",
		}
	}
	if input.Todos.Summary.RequiredOpen > 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "混合任务 required todo 尚未收敛。",
			InternalSummary:    "mixed task has open required todos",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "混合任务事实已满足。",
		InternalSummary:    "mixed task satisfied by verification + todo closure",
	}
}

// collectOpenRequiredTodos 收集 required 且未终态的 todo id。
func collectOpenRequiredTodos(items []TodoViewItem) []string {
	ids := make([]string, 0)
	for _, item := range items {
		if !item.Required {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "completed", "failed", "canceled":
			continue
		default:
			if id := strings.TrimSpace(item.ID); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// firstOrEmpty 返回首个元素，不存在时返回空串。
func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
