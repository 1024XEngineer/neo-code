package todo

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const (
	actionPlan      = "plan"
	actionAdd       = "add"
	actionUpdate    = "update"
	actionSetStatus = "set_status"
	actionRemove    = "remove"
	actionClaim     = "claim"
	actionComplete  = "complete"
	actionFail      = "fail"
)

const (
	reasonInvalidAction       = "invalid_action"
	reasonInvalidArguments    = "invalid_arguments"
	reasonTodoNotFound        = "todo_not_found"
	reasonInvalidTransition   = "invalid_transition"
	reasonDependencyViolation = "dependency_violation"
	reasonRevisionConflict    = "revision_conflict"
)

const (
	maxTodoWriteArgumentsBytes = 64 * 1024
	maxTodoWriteItems          = 64
	maxTodoWriteTextLen        = 1024
	maxTodoWriteListItems      = 64
)

var errTodoInvalidArguments = errors.New("todo_write: invalid arguments")

type writeInput struct {
	Action           string                  `json:"action"`
	Items            []agentsession.TodoItem `json:"items,omitempty"`
	Item             *agentsession.TodoItem  `json:"item,omitempty"`
	ID               string                  `json:"id,omitempty"`
	Patch            *todoPatchInput         `json:"patch,omitempty"`
	Status           agentsession.TodoStatus `json:"status,omitempty"`
	ExpectedRevision int64                   `json:"expected_revision,omitempty"`
	OwnerType        string                  `json:"owner_type,omitempty"`
	OwnerID          string                  `json:"owner_id,omitempty"`
	Artifacts        []string                `json:"artifacts,omitempty"`
	Reason           string                  `json:"reason,omitempty"`
}

type legacyWriteInput struct {
	Items []json.RawMessage `json:"items,omitempty"`
	Item  json.RawMessage   `json:"item,omitempty"`
}

type todoPatchInput struct {
	Content       *string                  `json:"content,omitempty"`
	Status        *agentsession.TodoStatus `json:"status,omitempty"`
	Dependencies  *[]string                `json:"dependencies,omitempty"`
	Priority      *int                     `json:"priority,omitempty"`
	OwnerType     *string                  `json:"owner_type,omitempty"`
	OwnerID       *string                  `json:"owner_id,omitempty"`
	Acceptance    *[]string                `json:"acceptance,omitempty"`
	Artifacts     *[]string                `json:"artifacts,omitempty"`
	FailureReason *string                  `json:"failure_reason,omitempty"`
}

func (p *todoPatchInput) toSessionPatch() agentsession.TodoPatch {
	if p == nil {
		return agentsession.TodoPatch{}
	}
	return agentsession.TodoPatch{
		Content:       p.Content,
		Status:        p.Status,
		Dependencies:  p.Dependencies,
		Priority:      p.Priority,
		OwnerType:     p.OwnerType,
		OwnerID:       p.OwnerID,
		Acceptance:    p.Acceptance,
		Artifacts:     p.Artifacts,
		FailureReason: p.FailureReason,
	}
}

// todoWireItem 表示 todo_write 入参中的单个 todo 项，并兼容 legacy title 字段。
type todoWireItem struct {
	ID            string                  `json:"id,omitempty"`
	Content       string                  `json:"content,omitempty"`
	Title         string                  `json:"title,omitempty"`
	Status        agentsession.TodoStatus `json:"status,omitempty"`
	Dependencies  []string                `json:"dependencies,omitempty"`
	Priority      int                     `json:"priority,omitempty"`
	OwnerType     string                  `json:"owner_type,omitempty"`
	OwnerID       string                  `json:"owner_id,omitempty"`
	Acceptance    []string                `json:"acceptance,omitempty"`
	Artifacts     []string                `json:"artifacts,omitempty"`
	FailureReason string                  `json:"failure_reason,omitempty"`
	Revision      int64                   `json:"revision,omitempty"`
}

func parseInput(raw []byte) (writeInput, error) {
	if len(raw) > maxTodoWriteArgumentsBytes {
		return writeInput{}, fmt.Errorf(
			"%w: arguments payload exceeds %d bytes",
			errTodoInvalidArguments,
			maxTodoWriteArgumentsBytes,
		)
	}

	var input writeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return writeInput{}, fmt.Errorf("todo_write: parse arguments: %w", err)
	}
	if err := applyLegacyTitleCompat(raw, &input); err != nil {
		return writeInput{}, err
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.ID = strings.TrimSpace(input.ID)
	input.OwnerType = strings.TrimSpace(input.OwnerType)
	input.OwnerID = strings.TrimSpace(input.OwnerID)
	input.Reason = strings.TrimSpace(input.Reason)
	if err := validateInputLimits(input); err != nil {
		return writeInput{}, err
	}
	return input, nil
}

// applyLegacyTitleCompat 兼容旧参数里的 title 字段，统一映射到 content。
func applyLegacyTitleCompat(raw []byte, input *writeInput) error {
	if input == nil {
		return fmt.Errorf("%w: invalid input payload", errTodoInvalidArguments)
	}

	var legacy legacyWriteInput
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fmt.Errorf("todo_write: parse arguments: %w", err)
	}

	if len(legacy.Items) > 0 {
		items, err := decodeLegacyItems(legacy.Items)
		if err != nil {
			return err
		}
		input.Items = items
	}
	if len(legacy.Item) > 0 && string(legacy.Item) != "null" {
		item, err := decodeLegacyItem(legacy.Item)
		if err != nil {
			return err
		}
		input.Item = &item
	}
	return nil
}

// decodeLegacyItems 解码 items 数组并处理 title/content 兼容映射。
func decodeLegacyItems(rawItems []json.RawMessage) ([]agentsession.TodoItem, error) {
	items := make([]agentsession.TodoItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, err := decodeLegacyItem(rawItem)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// decodeLegacyItem 解码单个 todo 项并将 legacy title 字段映射到 content。
func decodeLegacyItem(rawItem json.RawMessage) (agentsession.TodoItem, error) {
	var wire todoWireItem
	if err := json.Unmarshal(rawItem, &wire); err != nil {
		return agentsession.TodoItem{}, fmt.Errorf("todo_write: parse arguments: %w", err)
	}
	content := strings.TrimSpace(wire.Content)
	if content == "" {
		content = strings.TrimSpace(wire.Title)
	}
	return agentsession.TodoItem{
		ID:            wire.ID,
		Content:       content,
		Status:        wire.Status,
		Dependencies:  wire.Dependencies,
		Priority:      wire.Priority,
		OwnerType:     wire.OwnerType,
		OwnerID:       wire.OwnerID,
		Acceptance:    wire.Acceptance,
		Artifacts:     wire.Artifacts,
		FailureReason: wire.FailureReason,
		Revision:      wire.Revision,
	}, nil
}

// validateInputLimits 校验 todo_write 入参的字符串与数组规模，避免放大 token/内存占用。
func validateInputLimits(input writeInput) error {
	if input.ExpectedRevision < 0 {
		return fmt.Errorf("%w: expected_revision must be >= 0", errTodoInvalidArguments)
	}
	if err := ensureTodoWriteTextLength("id", input.ID); err != nil {
		return err
	}
	if err := ensureTodoWriteTextLength("owner_type", input.OwnerType); err != nil {
		return err
	}
	if err := ensureTodoWriteTextLength("owner_id", input.OwnerID); err != nil {
		return err
	}
	if err := ensureTodoWriteTextLength("reason", input.Reason); err != nil {
		return err
	}
	if err := ensureTodoWriteItemsLength("items", input.Items); err != nil {
		return err
	}
	if input.Item != nil {
		if err := ensureTodoWriteItemLength("item", *input.Item); err != nil {
			return err
		}
	}
	if input.Patch != nil {
		if err := ensureTodoWritePatchLength(*input.Patch); err != nil {
			return err
		}
	}
	if err := ensureTodoWriteStringSliceLength("artifacts", input.Artifacts); err != nil {
		return err
	}
	return nil
}

// ensureTodoWriteItemsLength 校验 todo 列表长度，并递归校验每个 Todo 项字段长度。
func ensureTodoWriteItemsLength(field string, items []agentsession.TodoItem) error {
	if len(items) > maxTodoWriteItems {
		return fmt.Errorf("%w: %s exceeds max length %d", errTodoInvalidArguments, field, maxTodoWriteItems)
	}
	for idx, item := range items {
		if err := ensureTodoWriteItemLength(fmt.Sprintf("%s[%d]", field, idx), item); err != nil {
			return err
		}
	}
	return nil
}

// ensureTodoWriteItemLength 校验单个 Todo 输入项中可控文本和列表字段长度。
func ensureTodoWriteItemLength(field string, item agentsession.TodoItem) error {
	checks := []struct {
		field string
		value string
	}{
		{field: field + ".id", value: item.ID},
		{field: field + ".content", value: item.Content},
		{field: field + ".owner_type", value: item.OwnerType},
		{field: field + ".owner_id", value: item.OwnerID},
		{field: field + ".failure_reason", value: item.FailureReason},
	}
	for _, check := range checks {
		if err := ensureTodoWriteTextLength(check.field, check.value); err != nil {
			return err
		}
	}
	if err := ensureTodoWriteStringSliceLength(field+".dependencies", item.Dependencies); err != nil {
		return err
	}
	if err := ensureTodoWriteStringSliceLength(field+".acceptance", item.Acceptance); err != nil {
		return err
	}
	if err := ensureTodoWriteStringSliceLength(field+".artifacts", item.Artifacts); err != nil {
		return err
	}
	return nil
}

// ensureTodoWritePatchLength 校验 patch 中可选字段，避免 patch 输入绕过长度约束。
func ensureTodoWritePatchLength(patch todoPatchInput) error {
	if patch.Content != nil {
		if err := ensureTodoWriteTextLength("patch.content", *patch.Content); err != nil {
			return err
		}
	}
	if patch.OwnerType != nil {
		if err := ensureTodoWriteTextLength("patch.owner_type", *patch.OwnerType); err != nil {
			return err
		}
	}
	if patch.OwnerID != nil {
		if err := ensureTodoWriteTextLength("patch.owner_id", *patch.OwnerID); err != nil {
			return err
		}
	}
	if patch.FailureReason != nil {
		if err := ensureTodoWriteTextLength("patch.failure_reason", *patch.FailureReason); err != nil {
			return err
		}
	}
	if patch.Dependencies != nil {
		if err := ensureTodoWriteStringSliceLength("patch.dependencies", *patch.Dependencies); err != nil {
			return err
		}
	}
	if patch.Acceptance != nil {
		if err := ensureTodoWriteStringSliceLength("patch.acceptance", *patch.Acceptance); err != nil {
			return err
		}
	}
	if patch.Artifacts != nil {
		if err := ensureTodoWriteStringSliceLength("patch.artifacts", *patch.Artifacts); err != nil {
			return err
		}
	}
	return nil
}

// ensureTodoWriteStringSliceLength 校验字符串列表项数量和元素长度。
func ensureTodoWriteStringSliceLength(field string, values []string) error {
	if len(values) > maxTodoWriteListItems {
		return fmt.Errorf("%w: %s exceeds max items %d", errTodoInvalidArguments, field, maxTodoWriteListItems)
	}
	for idx, value := range values {
		if err := ensureTodoWriteTextLength(fmt.Sprintf("%s[%d]", field, idx), value); err != nil {
			return err
		}
	}
	return nil
}

// ensureTodoWriteTextLength 校验字符串字段长度上限，超限时返回 invalid_arguments。
func ensureTodoWriteTextLength(field string, value string) error {
	if len(value) <= maxTodoWriteTextLen {
		return nil
	}
	return fmt.Errorf("%w: %s exceeds max length %d", errTodoInvalidArguments, field, maxTodoWriteTextLen)
}

func mapReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errTodoInvalidArguments):
		return reasonInvalidArguments
	case strings.Contains(strings.ToLower(err.Error()), "unsupported action"):
		return reasonInvalidAction
	case strings.Contains(err.Error(), agentsession.ErrTodoNotFound.Error()):
		return reasonTodoNotFound
	case strings.Contains(err.Error(), agentsession.ErrInvalidTransition.Error()):
		return reasonInvalidTransition
	case strings.Contains(err.Error(), agentsession.ErrDependencyViolation.Error()):
		return reasonDependencyViolation
	case strings.Contains(err.Error(), agentsession.ErrRevisionConflict.Error()):
		return reasonRevisionConflict
	default:
		return tools.NormalizeErrorReason(tools.ToolNameTodoWrite, err)
	}
}

func errorResult(reason string, details string, extra map[string]any) tools.ToolResult {
	metadata := map[string]any{
		"reason_code": strings.TrimSpace(reason),
	}
	for key, value := range extra {
		metadata[key] = value
	}
	result := tools.NewErrorResult(tools.ToolNameTodoWrite, strings.TrimSpace(reason), strings.TrimSpace(details), metadata)
	return tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
}

func successResult(action string, items []agentsession.TodoItem) tools.ToolResult {
	content := renderTodos(action, items)
	result := tools.ToolResult{
		Name:    tools.ToolNameTodoWrite,
		Content: content,
		Metadata: map[string]any{
			"action":     strings.TrimSpace(action),
			"todo_count": len(items),
		},
	}
	return tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
}

func renderTodos(action string, items []agentsession.TodoItem) string {
	lines := []string{
		"todo write result",
		"action: " + strings.TrimSpace(action),
		fmt.Sprintf("count: %d", len(items)),
	}
	if len(items) == 0 {
		return strings.Join(lines, "\n")
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		if items[i].Status != items[j].Status {
			return string(items[i].Status) < string(items[j].Status)
		}
		return items[i].ID < items[j].ID
	})

	lines = append(lines, "todos:")
	for _, item := range items {
		lines = append(lines,
			fmt.Sprintf("- [%s] %s (rev=%d, p=%d) %s", item.Status, item.ID, item.Revision, item.Priority, item.Content),
		)
	}
	return strings.Join(lines, "\n")
}
