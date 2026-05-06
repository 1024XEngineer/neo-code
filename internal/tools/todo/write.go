package todo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// Tool 是会话级 Todo 读写工具实现。
type Tool struct{}

// New 返回 todo_write 工具实例。
func New() *Tool {
	return &Tool{}
}

// Name 返回工具唯一名称。
func (t *Tool) Name() string {
	return tools.ToolNameTodoWrite
}

// Description 返回工具描述。
func (t *Tool) Description() string {
	return "Write and manage session todos with status transitions, revisions, and dependencies."
}

// Schema 返回 todo_write 工具参数 schema。
func (t *Tool) Schema() map[string]any {
	statusEnum := []string{
		string(agentsession.TodoStatusPending),
		string(agentsession.TodoStatusInProgress),
		string(agentsession.TodoStatusBlocked),
		string(agentsession.TodoStatusCompleted),
		string(agentsession.TodoStatusFailed),
		string(agentsession.TodoStatusCanceled),
	}
	blockedReasonEnum := []string{
		string(agentsession.TodoBlockedReasonInternalDependency),
		string(agentsession.TodoBlockedReasonPermissionWait),
		string(agentsession.TodoBlockedReasonUserInputWait),
		string(agentsession.TodoBlockedReasonExternalResourceWait),
		string(agentsession.TodoBlockedReasonUnknown),
	}

	todoItemSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type": "string",
			},
			"content": map[string]any{
				"type": "string",
			},
			"status": map[string]any{
				"type": "string",
				"enum": statusEnum,
			},
			"required": map[string]any{
				"type": "boolean",
			},
			"blocked_reason": map[string]any{
				"type":        "string",
				"enum":        blockedReasonEnum,
				"description": "仅当 status == \"blocked\" 时填写;其他状态请省略本字段。unknown 仅用于\"已经阻塞但无法给出具体原因\"的场景。",
			},
			"dependencies": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"priority": map[string]any{
				"type": "integer",
			},
			"executor": map[string]any{
				"type": "string",
				"enum": []string{
					"agent",
					"subagent",
				},
			},
			"owner_type": map[string]any{
				"type": "string",
			},
			"owner_id": map[string]any{
				"type": "string",
			},
			"acceptance": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"artifacts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"failure_reason": map[string]any{
				"type": "string",
			},
			"revision": map[string]any{
				"type": "integer",
			},
		},
		"required": []string{"id", "content"},
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					actionPlan,
					actionAdd,
					actionUpdate,
					actionSetStatus,
					actionRemove,
					actionClaim,
					actionComplete,
					actionFail,
				},
			},
			"items": map[string]any{
				"type":  "array",
				"items": todoItemSchema,
			},
			"item": map[string]any{
				"allOf": []any{
					todoItemSchema,
				},
			},
			"id": map[string]any{
				"type": "string",
			},
			"patch": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type": "string",
					},
					"status": map[string]any{
						"type": "string",
						"enum": statusEnum,
					},
					"required": map[string]any{
						"type": "boolean",
					},
					"blocked_reason": map[string]any{
						"type": "string",
						"enum": blockedReasonEnum,
					},
					"dependencies": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
					"priority": map[string]any{
						"type": "integer",
					},
					"executor": map[string]any{
						"type": "string",
						"enum": []string{
							"agent",
							"subagent",
						},
					},
					"owner_type": map[string]any{
						"type": "string",
					},
					"owner_id": map[string]any{
						"type": "string",
					},
					"acceptance": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
					"artifacts": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
					"failure_reason": map[string]any{
						"type": "string",
					},
				},
			},
			"status": map[string]any{
				"type": "string",
				"enum": statusEnum,
			},
			"expected_revision": map[string]any{
				"type": "integer",
			},
			"executor": map[string]any{
				"type": "string",
				"enum": []string{
					"agent",
					"subagent",
				},
			},
			"owner_type": map[string]any{
				"type": "string",
			},
			"owner_id": map[string]any{
				"type": "string",
			},
			"artifacts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
			"reason": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"action"},
		"oneOf": []any{
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionPlan}},
				"required":   []string{"action", "items"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionAdd}},
				"required":   []string{"action", "item"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionUpdate}},
				"required":   []string{"action", "id", "patch"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionSetStatus}},
				"required":   []string{"action", "id", "status"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionRemove}},
				"required":   []string{"action", "id"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionClaim}},
				"required":   []string{"action", "id", "owner_type", "owner_id"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionComplete}},
				"required":   []string{"action", "id"},
			},
			map[string]any{
				"properties": map[string]any{"action": map[string]any{"const": actionFail}},
				"required":   []string{"action", "id"},
			},
		},
	}
}

// MicroCompactPolicy 返回工具微压缩策略。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

// Execute 执行 todo_write 的 action 分发，并把变更写回会话。
func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return errorResult(reasonInvalidArguments, err.Error(), nil), err
	}
	if call.SessionMutator == nil {
		err := errors.New("todo_write: session mutator is unavailable")
		return errorResult(reasonInvalidArguments, err.Error(), nil), err
	}

	input, err := parseInput(call.Arguments)
	if err != nil {
		return errorResult(reasonInvalidArguments, err.Error(), nil), err
	}

	dispatchMeta, resultErr := t.dispatch(call, input)
	if resultErr != nil {
		reason := mapReason(resultErr)
		extra := map[string]any{"action": input.Action}
		if reason == reasonRevisionConflict && input.ID != "" {
			if current, ok := call.SessionMutator.FindTodo(input.ID); ok {
				extra["current_revision"] = current.Revision
				extra["current_status"] = string(current.Status)
			}
		}
		return errorResult(reason, resultErr.Error(), extra), resultErr
	}

	return successResultWithMetadata(input.Action, call.SessionMutator.ListTodos(), dispatchMeta), nil
}

// dispatch 按 action 执行对应 Todo 变更。
func (t *Tool) dispatch(call tools.ToolCallInput, input writeInput) (map[string]any, error) {
	switch input.Action {
	case actionPlan:
		if input.Items == nil {
			return nil, fmt.Errorf("%w: action %q requires items", errTodoInvalidArguments, actionPlan)
		}
		if len(input.Items) == 0 {
			return nil, fmt.Errorf("%w: action %q rejects empty items; mark finished todos via set_status (completed) or remove individual entries via remove — do not clear the plan with an empty array", errTodoInvalidArguments, actionPlan)
		}
		if err := call.SessionMutator.ReplaceTodos(input.Items); err != nil {
			return nil, err
		}
		return map[string]any{"state_fact": "todo_updated"}, nil
	case actionAdd:
		if input.Item == nil {
			return nil, fmt.Errorf("%w: action %q requires item", errTodoInvalidArguments, actionAdd)
		}
		if err := call.SessionMutator.AddTodo(*input.Item); err != nil {
			return nil, err
		}
		return map[string]any{"state_fact": "todo_created"}, nil
	case actionUpdate:
		if input.ID == "" || input.Patch == nil {
			return nil, fmt.Errorf("%w: action %q requires id and patch", errTodoInvalidArguments, actionUpdate)
		}
		if err := call.SessionMutator.UpdateTodo(input.ID, input.Patch.toSessionPatch(), input.ExpectedRevision); err != nil {
			return nil, err
		}
		return map[string]any{"state_fact": "todo_updated"}, nil
	case actionSetStatus:
		if input.ID == "" {
			return nil, fmt.Errorf("%w: action %q requires id", errTodoInvalidArguments, actionSetStatus)
		}
		if !input.Status.Valid() {
			return nil, fmt.Errorf("%w: action %q requires valid status", errTodoInvalidArguments, actionSetStatus)
		}
		if err := call.SessionMutator.SetTodoStatus(input.ID, input.Status, input.ExpectedRevision); err != nil {
			return nil, err
		}
		stateFact := "todo_updated"
		if input.Status == agentsession.TodoStatusCompleted {
			stateFact = "todo_completed"
		} else if input.Status == agentsession.TodoStatusFailed {
			stateFact = "todo_failed"
		}
		return map[string]any{"state_fact": stateFact}, nil
	case actionRemove:
		if input.ID == "" {
			return nil, fmt.Errorf("%w: action %q requires id", errTodoInvalidArguments, actionRemove)
		}
		if err := call.SessionMutator.DeleteTodo(input.ID, input.ExpectedRevision); err != nil {
			return nil, err
		}
		return map[string]any{"state_fact": "todo_updated"}, nil
	case actionClaim:
		if input.ID == "" {
			return nil, fmt.Errorf("%w: action %q requires id", errTodoInvalidArguments, actionClaim)
		}
		if strings.TrimSpace(input.OwnerType) == "" || strings.TrimSpace(input.OwnerID) == "" {
			return nil, fmt.Errorf("%w: action %q requires owner_type and owner_id", errTodoInvalidArguments, actionClaim)
		}
		if err := call.SessionMutator.ClaimTodo(input.ID, input.OwnerType, input.OwnerID, input.ExpectedRevision); err != nil {
			return nil, err
		}
		return map[string]any{"state_fact": "todo_updated"}, nil
	case actionComplete:
		if input.ID == "" {
			return nil, fmt.Errorf("%w: action %q requires id", errTodoInvalidArguments, actionComplete)
		}
		meta, err := completeTodoWithErgonomics(call, input)
		if err != nil {
			return nil, err
		}
		return meta, nil
	case actionFail:
		if input.ID == "" {
			return nil, fmt.Errorf("%w: action %q requires id", errTodoInvalidArguments, actionFail)
		}
		if err := call.SessionMutator.FailTodo(input.ID, input.Reason, input.ExpectedRevision); err != nil {
			return nil, err
		}
		return map[string]any{
			"state_fact":         "todo_failed",
			"terminal_failure":   true,
			"do_not_retry":       true,
			"transition_path":    []string{"failed"},
			"auto_claimed":       false,
			"failure_reason_set": strings.TrimSpace(input.Reason) != "",
		}, nil
	default:
		return nil, fmt.Errorf("todo_write: unsupported action %q", input.Action)
	}
}

// completeTodoWithErgonomics 为 complete 动作提供 pending->in_progress->completed 便捷迁移。
func completeTodoWithErgonomics(call tools.ToolCallInput, input writeInput) (map[string]any, error) {
	current, ok := call.SessionMutator.FindTodo(input.ID)
	if !ok {
		return nil, fmt.Errorf("%w: todo %q", agentsession.ErrTodoNotFound, input.ID)
	}

	switch current.Status {
	case agentsession.TodoStatusCompleted:
		return map[string]any{
			"state_fact":      "todo_completed",
			"no_op":           true,
			"reason_code":     reasonTerminalNoop,
			"auto_claimed":    false,
			"transition_path": []string{"completed"},
		}, nil
	case agentsession.TodoStatusPending:
		if err := call.SessionMutator.SetTodoStatus(input.ID, agentsession.TodoStatusInProgress, input.ExpectedRevision); err != nil {
			return nil, err
		}
		updated, exists := call.SessionMutator.FindTodo(input.ID)
		if !exists {
			return nil, fmt.Errorf("%w: todo %q", agentsession.ErrTodoNotFound, input.ID)
		}
		if err := call.SessionMutator.CompleteTodo(input.ID, input.Artifacts, updated.Revision); err != nil {
			return nil, err
		}
		return map[string]any{
			"state_fact":      "todo_completed",
			"auto_claimed":    true,
			"transition_path": []string{"pending", "in_progress", "completed"},
		}, nil
	default:
		if err := call.SessionMutator.CompleteTodo(input.ID, input.Artifacts, input.ExpectedRevision); err != nil {
			return nil, err
		}
		return map[string]any{
			"state_fact":      "todo_completed",
			"auto_claimed":    false,
			"transition_path": []string{"in_progress", "completed"},
		}, nil
	}
}
