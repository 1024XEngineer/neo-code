package todo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	reasonTerminalNoop        = "terminal_noop"
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
	Executor         string                  `json:"executor,omitempty"`
	OwnerType        string                  `json:"owner_type,omitempty"`
	OwnerID          string                  `json:"owner_id,omitempty"`
	Artifacts        []string                `json:"artifacts,omitempty"`
	Reason           string                  `json:"reason,omitempty"`
}

type todoPatchInput struct {
	Content       *string                         `json:"content,omitempty"`
	Status        *agentsession.TodoStatus        `json:"status,omitempty"`
	Required      *bool                           `json:"required,omitempty"`
	BlockedReason *agentsession.TodoBlockedReason `json:"blocked_reason,omitempty"`
	Dependencies  *[]string                       `json:"dependencies,omitempty"`
	Priority      *int                            `json:"priority,omitempty"`
	Executor      *string                         `json:"executor,omitempty"`
	OwnerType     *string                         `json:"owner_type,omitempty"`
	OwnerID       *string                         `json:"owner_id,omitempty"`
	Acceptance    *[]string                       `json:"acceptance,omitempty"`
	Artifacts     *[]string                       `json:"artifacts,omitempty"`
	FailureReason *string                         `json:"failure_reason,omitempty"`
}

func (p *todoPatchInput) toSessionPatch() agentsession.TodoPatch {
	if p == nil {
		return agentsession.TodoPatch{}
	}
	return agentsession.TodoPatch{
		Content:       p.Content,
		Status:        p.Status,
		Required:      p.Required,
		BlockedReason: p.BlockedReason,
		Dependencies:  p.Dependencies,
		Priority:      p.Priority,
		Executor:      p.Executor,
		OwnerType:     p.OwnerType,
		OwnerID:       p.OwnerID,
		Acceptance:    p.Acceptance,
		Artifacts:     p.Artifacts,
		FailureReason: p.FailureReason,
	}
}

func parseInput(raw []byte) (writeInput, error) {
	if len(raw) > maxTodoWriteArgumentsBytes {
		return writeInput{}, fmt.Errorf(
			"%w: arguments payload exceeds %d bytes",
			errTodoInvalidArguments,
			maxTodoWriteArgumentsBytes,
		)
	}

	normalizedRaw, err := normalizeWriteInputArguments(raw)
	if err != nil {
		return writeInput{}, err
	}

	var input writeInput
	if err := json.Unmarshal(normalizedRaw, &input); err != nil {
		return writeInput{}, fmt.Errorf("todo_write: parse arguments: %w", err)
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.ID = strings.TrimSpace(input.ID)
	input.Executor = strings.TrimSpace(input.Executor)
	input.OwnerType = strings.TrimSpace(input.OwnerType)
	input.OwnerID = strings.TrimSpace(input.OwnerID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Status = normalizeTodoStatus(input.Status)
	normalizeInputStatuses(&input)
	if err := validateInputLimits(input); err != nil {
		return writeInput{}, err
	}
	return input, nil
}

// normalizeWriteInputArguments 预处理 todo_write 原始 JSON，兼容数字 id 与字符串数组中的标量类型。
func normalizeWriteInputArguments(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("todo_write: parse arguments: %w", err)
	}
	if err := ensureNoLegacyTodoTitleField(payload); err != nil {
		return nil, err
	}
	normalizeWriteInputObject(payload)
	normalizedRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("todo_write: normalize arguments: %w", err)
	}
	return normalizedRaw, nil
}

// normalizeWriteInputObject 递归规范化顶层 todo_write 参数对象，降低模型输出变体导致的解析失败。
func normalizeWriteInputObject(payload map[string]any) {
	normalizeStringField(payload, "action")
	normalizeStringField(payload, "id")
	normalizeStringField(payload, "executor")
	normalizeStringField(payload, "owner_type")
	normalizeStringField(payload, "owner_id")
	normalizeStringField(payload, "reason")
	normalizeStringField(payload, "status")
	normalizeStringArrayField(payload, "artifacts")

	if patch, ok := payload["patch"].(map[string]any); ok {
		normalizeTodoPatchObject(patch)
	}
	if item, ok := payload["item"].(map[string]any); ok {
		normalizeTodoItemObject(item)
	}
	if items, ok := payload["items"].([]any); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			normalizeTodoItemObject(item)
		}
	}
}

// normalizeTodoPatchObject 规范化 patch 内的字符串与字符串数组字段。
func normalizeTodoPatchObject(payload map[string]any) {
	normalizeStringField(payload, "content")
	normalizeStringField(payload, "status")
	normalizeStringField(payload, "blocked_reason")
	normalizeStringField(payload, "executor")
	normalizeStringField(payload, "owner_type")
	normalizeStringField(payload, "owner_id")
	normalizeStringField(payload, "failure_reason")
	normalizeStringArrayField(payload, "dependencies")
	normalizeStringArrayField(payload, "acceptance")
	normalizeStringArrayField(payload, "artifacts")
}

// normalizeTodoItemObject 规范化 todo item 对象，确保 id/dependency 等字段稳定为字符串。
func normalizeTodoItemObject(payload map[string]any) {
	normalizeStringField(payload, "id")
	normalizeStringField(payload, "content")
	normalizeStringField(payload, "status")
	normalizeStringField(payload, "blocked_reason")
	normalizeStringField(payload, "executor")
	normalizeStringField(payload, "owner_type")
	normalizeStringField(payload, "owner_id")
	normalizeStringField(payload, "failure_reason")
	normalizeStringArrayField(payload, "dependencies")
	normalizeStringArrayField(payload, "acceptance")
	normalizeStringArrayField(payload, "artifacts")
}

// ensureNoLegacyTodoTitleField 显式拒绝 legacy title 字段，避免静默兼容掩盖协议升级问题。
func ensureNoLegacyTodoTitleField(payload map[string]any) error {
	if payload == nil {
		return nil
	}
	if item, ok := payload["item"].(map[string]any); ok {
		if _, exists := item["title"]; exists {
			return fmt.Errorf("%w: legacy field \"item.title\" is no longer supported, use \"item.content\" instead", errTodoInvalidArguments)
		}
	}
	if items, ok := payload["items"].([]any); ok {
		for idx, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := item["title"]; exists {
				return fmt.Errorf(
					"%w: legacy field \"items[%d].title\" is no longer supported, use \"items[%d].content\" instead",
					errTodoInvalidArguments,
					idx,
					idx,
				)
			}
		}
	}
	return nil
}

// normalizeStringArrayField 将数组中的标量统一转换为字符串并裁掉首尾空白。
func normalizeStringArrayField(payload map[string]any, field string) {
	raw, ok := payload[field]
	if !ok {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		s, ok := stringifyScalar(value)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	payload[field] = out
}

// normalizeStringField 把 JSON 标量转换为字符串，兼容模型输出的数字 id 等常见变体。
func normalizeStringField(payload map[string]any, field string) {
	raw, ok := payload[field]
	if !ok {
		return
	}
	s, ok := stringifyScalar(raw)
	if !ok {
		return
	}
	payload[field] = strings.TrimSpace(s)
}

// stringifyScalar 将 JSON 标量转换成字符串，非标量（object/array/null）返回 false。
func stringifyScalar(raw any) (string, bool) {
	switch value := raw.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return "", false
	}
}

// normalizeInputStatuses 统一规整输入中的 status 字段，兼容常见别名和分隔符差异。
func normalizeInputStatuses(input *writeInput) {
	if input == nil {
		return
	}
	for idx := range input.Items {
		input.Items[idx].Status = normalizeTodoStatus(input.Items[idx].Status)
	}
	if input.Item != nil {
		input.Item.Status = normalizeTodoStatus(input.Item.Status)
	}
	if input.Patch != nil && input.Patch.Status != nil {
		status := normalizeTodoStatus(*input.Patch.Status)
		input.Patch.Status = &status
	}
}

// normalizeTodoStatus 将状态值转换为规范枚举格式，兼容 in-progress/done/cancelled 等别名。
func normalizeTodoStatus(status agentsession.TodoStatus) agentsession.TodoStatus {
	raw := strings.ToLower(strings.TrimSpace(string(status)))
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = strings.ReplaceAll(raw, " ", "_")
	raw = strings.ReplaceAll(raw, "__", "_")

	switch raw {
	case "inprogress", "doing", "running":
		raw = string(agentsession.TodoStatusInProgress)
	case "done":
		raw = string(agentsession.TodoStatusCompleted)
	case "cancelled":
		raw = string(agentsession.TodoStatusCanceled)
	}
	return agentsession.TodoStatus(raw)
}

// validateInputLimits 校验 todo_write 入参的字符串与数组规模，避免放大 token/内存占用。
func validateInputLimits(input writeInput) error {
	if input.ExpectedRevision < 0 {
		return fmt.Errorf("%w: expected_revision must be >= 0", errTodoInvalidArguments)
	}
	if err := ensureTodoWriteTextLength("id", input.ID); err != nil {
		return err
	}
	if err := ensureTodoWriteTextLength("executor", input.Executor); err != nil {
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
		{field: field + ".blocked_reason", value: string(item.BlockedReason)},
		{field: field + ".executor", value: item.Executor},
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
	if patch.BlockedReason != nil {
		if err := ensureTodoWriteTextLength("patch.blocked_reason", string(*patch.BlockedReason)); err != nil {
			return err
		}
	}
	if patch.OwnerType != nil {
		if err := ensureTodoWriteTextLength("patch.owner_type", *patch.OwnerType); err != nil {
			return err
		}
	}
	if patch.Executor != nil {
		if err := ensureTodoWriteTextLength("patch.executor", *patch.Executor); err != nil {
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
	return successResultWithMetadata(action, items, nil)
}

func successResultWithMetadata(action string, items []agentsession.TodoItem, extra map[string]any) tools.ToolResult {
	content := renderTodos(action, items)
	snapshotItems, summary := buildTodoSnapshotMetadata(items)
	result := tools.ToolResult{
		Name:    tools.ToolNameTodoWrite,
		Content: content,
		Metadata: map[string]any{
			"action":        strings.TrimSpace(action),
			"todo_count":    len(items),
			"todo_snapshot": snapshotItems,
			"todo_summary":  summary,
		},
	}
	for key, value := range extra {
		result.Metadata[key] = value
	}
	return tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
}

func buildTodoSnapshotMetadata(items []agentsession.TodoItem) ([]map[string]any, map[string]any) {
	if len(items) == 0 {
		return nil, map[string]any{
			"total":              0,
			"required_total":     0,
			"required_completed": 0,
			"required_failed":    0,
			"required_open":      0,
		}
	}

	viewItems := make([]map[string]any, 0, len(items))
	requiredTotal := 0
	requiredCompleted := 0
	requiredFailed := 0
	requiredOpen := 0
	for _, item := range items {
		required := item.RequiredValue()
		if required {
			requiredTotal++
			switch item.Status {
			case agentsession.TodoStatusCompleted:
				requiredCompleted++
			case agentsession.TodoStatusFailed:
				requiredFailed++
			case agentsession.TodoStatusCanceled:
				// canceled 不计入 open。
			default:
				requiredOpen++
			}
		}
		viewItems = append(viewItems, map[string]any{
			"id":             strings.TrimSpace(item.ID),
			"content":        strings.TrimSpace(item.Content),
			"status":         strings.TrimSpace(string(item.Status)),
			"required":       required,
			"artifacts":      append([]string(nil), item.Artifacts...),
			"failure_reason": strings.TrimSpace(item.FailureReason),
			"blocked_reason": strings.TrimSpace(string(item.BlockedReason)),
			"revision":       item.Revision,
		})
	}

	return viewItems, map[string]any{
		"total":              len(items),
		"required_total":     requiredTotal,
		"required_completed": requiredCompleted,
		"required_failed":    requiredFailed,
		"required_open":      requiredOpen,
	}
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
			fmt.Sprintf(
				"- [%s] %s (rev=%d, p=%d, executor=%s) %s",
				item.Status,
				item.ID,
				item.Revision,
				item.Priority,
				strings.TrimSpace(item.Executor),
				item.Content,
			),
		)
	}
	return strings.Join(lines, "\n")
}
