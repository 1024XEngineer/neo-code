package session

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CurrentTodoVersion 表示当前 Todo 结构版本。
const CurrentTodoVersion = 7

// TodoStatus 表示 Todo 项的状态枚举。
type TodoStatus string

const (
	// TodoStatusPending 表示尚未开始。
	TodoStatusPending TodoStatus = "pending"
	// TodoStatusInProgress 表示执行中。
	TodoStatusInProgress TodoStatus = "in_progress"
	// TodoStatusBlocked 表示被阻塞。
	TodoStatusBlocked TodoStatus = "blocked"
	// TodoStatusCompleted 表示已完成。
	TodoStatusCompleted TodoStatus = "completed"
	// TodoStatusFailed 表示执行失败。
	TodoStatusFailed TodoStatus = "failed"
	// TodoStatusCanceled 表示已取消。
	TodoStatusCanceled TodoStatus = "canceled"
)

// TodoBlockedReason 表示 blocked todo 的结构化阻塞原因。
type TodoBlockedReason string

const (
	// TodoBlockedReasonInternalDependency 表示被内部依赖阻塞。
	TodoBlockedReasonInternalDependency TodoBlockedReason = "internal_dependency"
	// TodoBlockedReasonPermissionWait 表示等待权限审批。
	TodoBlockedReasonPermissionWait TodoBlockedReason = "permission_wait"
	// TodoBlockedReasonUserInputWait 表示等待用户补充输入。
	TodoBlockedReasonUserInputWait TodoBlockedReason = "user_input_wait"
	// TodoBlockedReasonExternalResourceWait 表示等待外部资源就绪。
	TodoBlockedReasonExternalResourceWait TodoBlockedReason = "external_resource_wait"
	// TodoBlockedReasonUnknown 表示未知阻塞原因或旧数据缺省值。
	TodoBlockedReasonUnknown TodoBlockedReason = "unknown"
)

const (
	// TodoOwnerTypeUser 表示任务归属用户。
	TodoOwnerTypeUser = "user"
	// TodoOwnerTypeAgent 表示任务归属主 Agent。
	TodoOwnerTypeAgent = "agent"
	// TodoOwnerTypeSubAgent 表示任务归属 SubAgent。
	TodoOwnerTypeSubAgent = "subagent"
)

const (
	// TodoExecutorAgent 表示任务由主 Agent 执行。
	TodoExecutorAgent = "agent"
	// TodoExecutorSubAgent 表示任务由 SubAgent 调度执行。
	TodoExecutorSubAgent = "subagent"
)

// TodoItem 表示会话级结构化待办项。
type TodoItem struct {
	ID            string             `json:"id"`
	Content       string             `json:"content"`
	Status        TodoStatus         `json:"status"`
	Required      *bool              `json:"required,omitempty"`
	BlockedReason TodoBlockedReason  `json:"blocked_reason,omitempty"`
	Dependencies  []string           `json:"dependencies,omitempty"`
	Priority      int                `json:"priority,omitempty"`
	Executor      string             `json:"executor,omitempty"`
	OwnerType     string             `json:"owner_type,omitempty"`
	OwnerID       string             `json:"owner_id,omitempty"`
	Acceptance    []string           `json:"acceptance,omitempty"`
	Artifacts     []string           `json:"artifacts,omitempty"`
	Supersedes    []string           `json:"supersedes,omitempty"`
	ContentChecks []TodoContentCheck `json:"content_checks,omitempty"`
	FailureReason string             `json:"failure_reason,omitempty"`
	RetryCount    int                `json:"retry_count,omitempty"`
	RetryLimit    int                `json:"retry_limit,omitempty"`
	NextRetryAt   time.Time          `json:"next_retry_at,omitempty"`
	Revision      int64              `json:"revision"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// TodoPatch 表示对 Todo 可选字段的更新补丁。
type TodoPatch struct {
	Content       *string
	Status        *TodoStatus
	Required      *bool
	BlockedReason *TodoBlockedReason
	Dependencies  *[]string
	Priority      *int
	Executor      *string
	OwnerType     *string
	OwnerID       *string
	Acceptance    *[]string
	Artifacts     *[]string
	Supersedes    *[]string
	ContentChecks *[]TodoContentCheck
	FailureReason *string
	RetryCount    *int
	RetryLimit    *int
	NextRetryAt   *time.Time
}

// Clone 返回 Todo 项的深拷贝，避免切片共享底层内存。
func (i TodoItem) Clone() TodoItem {
	i.Dependencies = append([]string(nil), i.Dependencies...)
	i.Acceptance = append([]string(nil), i.Acceptance...)
	i.Artifacts = append([]string(nil), i.Artifacts...)
	i.Supersedes = append([]string(nil), i.Supersedes...)
	if len(i.ContentChecks) > 0 {
		i.ContentChecks = cloneTodoContentChecks(i.ContentChecks)
	}
	if i.Required != nil {
		required := *i.Required
		i.Required = &required
	}
	return i
}

// RequiredValue 返回 todo 是否为 required，兼容旧数据缺省值 true。
func (i TodoItem) RequiredValue() bool {
	if i.Required == nil {
		return true
	}
	return *i.Required
}

// Valid 判断当前状态值是否为受支持状态。
func (s TodoStatus) Valid() bool {
	switch s {
	case TodoStatusPending, TodoStatusInProgress, TodoStatusBlocked, TodoStatusCompleted, TodoStatusFailed, TodoStatusCanceled:
		return true
	default:
		return false
	}
}

// IsTerminal 判断状态是否为终态。
func (s TodoStatus) IsTerminal() bool {
	switch s {
	case TodoStatusCompleted, TodoStatusFailed, TodoStatusCanceled:
		return true
	default:
		return false
	}
}

// ValidTransition 判断状态迁移是否合法。
func (from TodoStatus) ValidTransition(to TodoStatus) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case TodoStatusPending:
		return to == TodoStatusInProgress || to == TodoStatusBlocked || to == TodoStatusFailed || to == TodoStatusCanceled
	case TodoStatusInProgress:
		return to == TodoStatusCompleted || to == TodoStatusFailed || to == TodoStatusBlocked || to == TodoStatusCanceled
	case TodoStatusBlocked:
		return to == TodoStatusPending || to == TodoStatusInProgress || to == TodoStatusFailed || to == TodoStatusCanceled
	default:
		return false
	}
}

// ListTodos 返回当前会话 Todos 的深拷贝列表。
func (s Session) ListTodos() []TodoItem {
	if len(s.Todos) == 0 {
		return nil
	}
	items := make([]TodoItem, len(s.Todos))
	for idx, item := range s.Todos {
		items[idx] = item.Clone()
	}
	return items
}

// FindTodo 按 ID 查询 Todo 项，并返回拷贝副本。
func (s Session) FindTodo(id string) (TodoItem, bool) {
	id, err := ensureTodoID(id)
	if err != nil {
		return TodoItem{}, false
	}

	for _, item := range s.Todos {
		if item.ID == id {
			return item.Clone(), true
		}
	}
	return TodoItem{}, false
}

// GetTodoByID 按 ID 查询 Todo 项，并返回拷贝指针。
func (s Session) GetTodoByID(id string) (*TodoItem, error) {
	item, ok := s.FindTodo(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTodoNotFound, strings.TrimSpace(id))
	}
	return &item, nil
}

// ReplaceTodos 用于整批替换当前 Todos（plan 场景）。空集合会被拒绝;清空意图请走 DeleteTodo / SetTodoStatus。
func (s *Session) ReplaceTodos(items []TodoItem) error {
	if s == nil {
		return errors.New("session: session is nil")
	}
	if len(items) == 0 {
		return errors.New("session: ReplaceTodos requires non-empty items; use DeleteTodo to remove individual entries or transition status instead")
	}
	normalized, err := normalizeAndValidateTodos(append([]TodoItem(nil), items...))
	if err != nil {
		return err
	}
	s.Todos = normalized
	s.TodoVersion = CurrentTodoVersion
	return nil
}

// AddTodo 向会话添加一个新的 Todo 项。
func (s *Session) AddTodo(item TodoItem) error {
	if s == nil {
		return errors.New("session: session is nil")
	}

	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = item.CreatedAt
	}
	if item.Revision <= 0 {
		item.Revision = 1
	}

	normalized, err := normalizeAndValidateTodos(append(append([]TodoItem(nil), s.Todos...), item))
	if err != nil {
		return err
	}
	s.Todos = normalized
	s.TodoVersion = CurrentTodoVersion
	return nil
}

// SetTodoStatus 按 ID 更新 Todo 状态并执行 revision 检查。
func (s *Session) SetTodoStatus(id string, status TodoStatus, expectedRevision int64) error {
	patch := TodoPatch{
		Status: &status,
	}
	return s.UpdateTodo(id, patch, expectedRevision)
}

// UpdateTodo 按补丁更新 Todo 并执行状态机、依赖与 revision 校验。
func (s *Session) UpdateTodo(id string, patch TodoPatch, expectedRevision int64) error {
	if s == nil {
		return errors.New("session: session is nil")
	}

	var err error
	id, err = ensureTodoID(id)
	if err != nil {
		return err
	}

	items := append([]TodoItem(nil), s.Todos...)
	for idx := range items {
		if items[idx].ID != id {
			continue
		}

		if err := ensureTodoRevision(items[idx], expectedRevision); err != nil {
			return err
		}

		next, err := applyTodoPatch(items[idx], patch)
		if err != nil {
			return err
		}
		next.UpdatedAt = time.Now()
		next.Revision = items[idx].Revision + 1
		items[idx] = next

		normalized, err := normalizeAndValidateTodos(items)
		if err != nil {
			return err
		}
		if err := ensureTodoDependenciesForStatus(normalized, normalized[idx]); err != nil {
			return err
		}

		s.Todos = normalized
		s.TodoVersion = CurrentTodoVersion
		return nil
	}

	return fmt.Errorf("%w: %q", ErrTodoNotFound, id)
}

// ClaimTodo 用于 SubAgent 领取 Todo，并设置 owner 与执行中状态。
func (s *Session) ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error {
	status := TodoStatusInProgress
	failureReason := ""
	blockedReason := TodoBlockedReason("")
	nextRetryAt := time.Time{}
	patch := TodoPatch{
		Status:        &status,
		OwnerType:     &ownerType,
		OwnerID:       &ownerID,
		BlockedReason: &blockedReason,
		FailureReason: &failureReason,
		NextRetryAt:   &nextRetryAt,
	}
	return s.UpdateTodo(id, patch, expectedRevision)
}

// CompleteTodo 将 Todo 标记为完成并写入产物列表。
func (s *Session) CompleteTodo(id string, artifacts []string, expectedRevision int64) error {
	status := TodoStatusCompleted
	failureReason := ""
	blockedReason := TodoBlockedReason("")
	retryCount := 0
	nextRetryAt := time.Time{}
	patch := TodoPatch{
		Status:        &status,
		Artifacts:     &artifacts,
		BlockedReason: &blockedReason,
		FailureReason: &failureReason,
		RetryCount:    &retryCount,
		NextRetryAt:   &nextRetryAt,
	}
	return s.UpdateTodo(id, patch, expectedRevision)
}

// FailTodo 将 Todo 标记为失败并记录失败原因。
func (s *Session) FailTodo(id string, reason string, expectedRevision int64) error {
	status := TodoStatusFailed
	blockedReason := TodoBlockedReason("")
	nextRetryAt := time.Time{}
	patch := TodoPatch{
		Status:        &status,
		BlockedReason: &blockedReason,
		FailureReason: &reason,
		NextRetryAt:   &nextRetryAt,
	}
	return s.UpdateTodo(id, patch, expectedRevision)
}

// DeleteTodo 按 ID 删除 Todo，删除前会检查 revision 与反向依赖。
func (s *Session) DeleteTodo(id string, expectedRevision int64) error {
	if s == nil {
		return errors.New("session: session is nil")
	}

	var err error
	id, err = ensureTodoID(id)
	if err != nil {
		return err
	}
	items := make([]TodoItem, 0, len(s.Todos))
	found := false
	for _, item := range s.Todos {
		if item.ID == id {
			if err := ensureTodoRevision(item, expectedRevision); err != nil {
				return err
			}
			if dependents := findTodoDependents(s.Todos, id); len(dependents) > 0 {
				return fmt.Errorf("%w: todo %q is still required by %s", ErrDependencyViolation, id, strings.Join(dependents, ", "))
			}
			found = true
			continue
		}
		items = append(items, item)
	}
	if !found {
		return fmt.Errorf("%w: %q", ErrTodoNotFound, id)
	}

	normalized, err := normalizeAndValidateTodos(items)
	if err != nil {
		return err
	}
	s.Todos = normalized
	s.TodoVersion = CurrentTodoVersion
	return nil
}

// findTodoDependents 返回依赖指定 Todo 的所有 Todo ID，并保持原顺序。
func findTodoDependents(items []TodoItem, id string) []string {
	if len(items) == 0 || strings.TrimSpace(id) == "" {
		return nil
	}

	dependents := make([]string, 0)
	for _, item := range items {
		for _, dependency := range item.Dependencies {
			if dependency == id {
				dependents = append(dependents, item.ID)
				break
			}
		}
	}
	if len(dependents) == 0 {
		return nil
	}
	return dependents
}

// normalizeAndValidateTodos 统一收敛 Todo 的文本、状态与依赖合法性。
func normalizeAndValidateTodos(items []TodoItem) ([]TodoItem, error) {
	if len(items) == 0 {
		return nil, nil
	}

	normalized := make([]TodoItem, 0, len(items))
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		next, err := normalizeTodoItem(item)
		if err != nil {
			return nil, err
		}
		if _, exists := ids[next.ID]; exists {
			return nil, fmt.Errorf("session: duplicate todo id %q", next.ID)
		}
		ids[next.ID] = struct{}{}
		normalized = append(normalized, next)
	}

	for _, item := range normalized {
		for _, dependency := range item.Dependencies {
			if dependency == item.ID {
				return nil, fmt.Errorf("session: todo %q cannot depend on itself", item.ID)
			}
			if _, exists := ids[dependency]; !exists {
				return nil, fmt.Errorf("session: todo %q references unknown dependency %q", item.ID, dependency)
			}
		}
		for _, superseded := range item.Supersedes {
			if superseded == item.ID {
				return nil, fmt.Errorf("session: todo %q cannot supersede itself", item.ID)
			}
			if _, exists := ids[superseded]; !exists {
				return nil, fmt.Errorf("session: todo %q references unknown superseded todo %q", item.ID, superseded)
			}
		}
	}
	if err := ensureCanceledRequiredTodosHaveReplacement(normalized); err != nil {
		return nil, err
	}
	if err := detectCyclicDependencies(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// normalizeTodoItem 规范化单个 Todo 项，确保持久化结构稳定。
func normalizeTodoItem(item TodoItem) (TodoItem, error) {
	item = item.Clone()
	item.ID = strings.TrimSpace(item.ID)
	item.Content = strings.TrimSpace(item.Content)
	item.Dependencies = normalizeTodoDependencies(item.Dependencies)
	item.Executor = normalizeTodoExecutor(item.Executor)
	if item.Executor == "" {
		item.Executor = TodoExecutorAgent
	}
	item.BlockedReason = normalizeTodoBlockedReason(item.BlockedReason)
	if item.Required != nil {
		required := *item.Required
		item.Required = &required
	}
	item.OwnerType = normalizeTodoOwnerType(item.OwnerType)
	item.OwnerID = strings.TrimSpace(item.OwnerID)
	item.Acceptance = normalizeTodoTextList(item.Acceptance)
	item.Artifacts = normalizeTodoTextList(item.Artifacts)
	item.Supersedes = normalizeTodoTextList(item.Supersedes)
	item.ContentChecks = normalizeTodoContentChecks(item.ContentChecks)
	item.FailureReason = strings.TrimSpace(item.FailureReason)
	if item.RetryCount < 0 {
		item.RetryCount = 0
	}
	if item.RetryLimit < 0 {
		item.RetryLimit = 0
	}
	if !item.NextRetryAt.IsZero() {
		item.NextRetryAt = item.NextRetryAt.UTC()
	}
	if item.Status == "" {
		item.Status = TodoStatusPending
	}
	if item.Revision <= 0 {
		item.Revision = 1
	}

	switch {
	case item.ID == "":
		return TodoItem{}, errors.New("session: todo id is empty")
	case item.Content == "":
		return TodoItem{}, fmt.Errorf("session: todo %q content is empty", item.ID)
	case !item.Status.Valid():
		return TodoItem{}, fmt.Errorf("session: invalid todo status %q", item.Status)
	case !isValidTodoBlockedReason(item.BlockedReason):
		return TodoItem{}, fmt.Errorf("session: invalid todo blocked_reason %q", item.BlockedReason)
	case !isValidTodoExecutor(item.Executor):
		return TodoItem{}, fmt.Errorf("session: invalid todo executor %q", item.Executor)
	case !isValidTodoOwnerType(item.OwnerType):
		return TodoItem{}, fmt.Errorf("session: invalid todo owner_type %q", item.OwnerType)
	}

	if item.Status != TodoStatusFailed && item.Status != TodoStatusPending && item.Status != TodoStatusBlocked {
		item.FailureReason = ""
	}
	if item.Status != TodoStatusBlocked {
		item.BlockedReason = ""
	}
	if item.Required == nil {
		defaultRequired := true
		item.Required = &defaultRequired
	}
	if item.Status != TodoStatusPending && item.Status != TodoStatusFailed {
		item.NextRetryAt = time.Time{}
	}
	return item, nil
}

// normalizeTodoDependencies 对依赖列表做去空白、去重并保持顺序。
func normalizeTodoDependencies(dependencies []string) []string {
	return normalizeTodoTextList(dependencies)
}

// normalizeTodoTextList 对文本列表做去空白、去重并保持顺序。
func normalizeTodoTextList(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ensureTodoID 统一校验并返回规范化后的 Todo ID。
func ensureTodoID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("session: todo id is empty")
	}
	return id, nil
}

// ensureTodoDependenciesForStatus 校验目标状态下依赖是否满足。
func ensureTodoDependenciesForStatus(items []TodoItem, item TodoItem) error {
	if item.Status != TodoStatusInProgress && item.Status != TodoStatusCompleted {
		return nil
	}
	if len(item.Dependencies) == 0 {
		return nil
	}

	statuses := make(map[string]TodoStatus, len(items))
	for _, current := range items {
		statuses[current.ID] = current.Status
	}

	blocking := make([]string, 0)
	for _, dependency := range item.Dependencies {
		status, ok := statuses[dependency]
		if !ok || status != TodoStatusCompleted {
			blocking = append(blocking, dependency)
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	sort.Strings(blocking)
	return fmt.Errorf("%w: todo %q blocked by %s", ErrDependencyViolation, item.ID, strings.Join(blocking, ", "))
}

// ensureTodoRevision 校验更新请求携带的 expected revision。
func ensureTodoRevision(item TodoItem, expectedRevision int64) error {
	if expectedRevision <= 0 {
		return nil
	}
	if item.Revision == expectedRevision {
		return nil
	}
	return fmt.Errorf("%w: todo %q expected %d actual %d", ErrRevisionConflict, item.ID, expectedRevision, item.Revision)
}

// applyTodoPatch 把补丁应用到 Todo 上，并校验状态机迁移与字段合法性。
func applyTodoPatch(item TodoItem, patch TodoPatch) (TodoItem, error) {
	next := item.Clone()

	if patch.Content != nil {
		next.Content = strings.TrimSpace(*patch.Content)
	}
	if patch.Dependencies != nil {
		next.Dependencies = normalizeTodoDependencies(*patch.Dependencies)
	}
	if patch.Required != nil {
		required := *patch.Required
		next.Required = &required
	}
	if patch.BlockedReason != nil {
		reason := normalizeTodoBlockedReason(*patch.BlockedReason)
		next.BlockedReason = reason
	}
	if patch.Priority != nil {
		next.Priority = *patch.Priority
	}
	if patch.Executor != nil {
		next.Executor = normalizeTodoExecutor(*patch.Executor)
	}
	if patch.OwnerType != nil {
		next.OwnerType = normalizeTodoOwnerType(*patch.OwnerType)
	}
	if patch.OwnerID != nil {
		next.OwnerID = strings.TrimSpace(*patch.OwnerID)
	}
	if patch.Acceptance != nil {
		next.Acceptance = normalizeTodoTextList(*patch.Acceptance)
	}
	if patch.Artifacts != nil {
		next.Artifacts = normalizeTodoTextList(*patch.Artifacts)
	}
	if patch.Supersedes != nil {
		next.Supersedes = normalizeTodoTextList(*patch.Supersedes)
	}
	if patch.ContentChecks != nil {
		next.ContentChecks = cloneTodoContentChecks(*patch.ContentChecks)
	}
	if patch.FailureReason != nil {
		next.FailureReason = strings.TrimSpace(*patch.FailureReason)
	}
	if patch.RetryCount != nil {
		next.RetryCount = *patch.RetryCount
	}
	if patch.RetryLimit != nil {
		next.RetryLimit = *patch.RetryLimit
	}
	if patch.NextRetryAt != nil {
		next.NextRetryAt = *patch.NextRetryAt
	}
	if patch.Status != nil {
		target := *patch.Status
		if !target.Valid() {
			return TodoItem{}, fmt.Errorf("session: invalid todo status %q", target)
		}
		if !item.Status.ValidTransition(target) {
			return TodoItem{}, fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, item.Status, target)
		}
		next.Status = target
	}

	normalized, err := normalizeTodoItem(next)
	if err != nil {
		return TodoItem{}, err
	}
	return normalized, nil
}

// normalizeTodoBlockedReason 规整 blocked_reason 字段为小写、去空白;空字符串保持空(语义="未阻塞")。
// status 与字段不一致时由 normalizeTodoItem 强制清空。
func normalizeTodoBlockedReason(reason TodoBlockedReason) TodoBlockedReason {
	return TodoBlockedReason(strings.ToLower(strings.TrimSpace(string(reason))))
}

// cloneTodoContentChecks 深拷贝内容校验规则，避免切片共享底层内存。
func cloneTodoContentChecks(items []TodoContentCheck) []TodoContentCheck {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TodoContentCheck, len(items))
	for idx, item := range items {
		cloned[idx] = TodoContentCheck{
			Artifact: strings.TrimSpace(item.Artifact),
			Contains: append([]string(nil), item.Contains...),
		}
	}
	return cloned
}

// normalizeTodoContentChecks 统一收敛内容校验规则，避免空规则和重复 token 漂入持久化状态。
func normalizeTodoContentChecks(items []TodoContentCheck) []TodoContentCheck {
	if len(items) == 0 {
		return nil
	}
	normalized := make([]TodoContentCheck, 0, len(items))
	for _, item := range items {
		artifact := strings.TrimSpace(item.Artifact)
		if artifact == "" {
			continue
		}
		contains := normalizeTodoTextList(item.Contains)
		normalized = append(normalized, TodoContentCheck{
			Artifact: artifact,
			Contains: contains,
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// ensureCanceledRequiredTodosHaveReplacement 收紧 required todo 的取消语义，要求显式 replacement todo。
func ensureCanceledRequiredTodosHaveReplacement(items []TodoItem) error {
	if len(items) == 0 {
		return nil
	}
	replacements := make(map[string]struct{})
	for _, item := range items {
		if !item.RequiredValue() || item.Status == TodoStatusCanceled {
			continue
		}
		for _, superseded := range item.Supersedes {
			replacements[superseded] = struct{}{}
		}
	}
	for _, item := range items {
		if !item.RequiredValue() || item.Status != TodoStatusCanceled {
			continue
		}
		if _, ok := replacements[item.ID]; ok {
			continue
		}
		return fmt.Errorf("session: required canceled todo %q must declare an explicit replacement", item.ID)
	}
	return nil
}

// isValidTodoBlockedReason 判断 blocked_reason 是否受支持。
// 空字符串合法(代表"未阻塞");其余必须是 5 个 enum 值之一。
func isValidTodoBlockedReason(reason TodoBlockedReason) bool {
	switch normalizeTodoBlockedReason(reason) {
	case "",
		TodoBlockedReasonInternalDependency,
		TodoBlockedReasonPermissionWait,
		TodoBlockedReasonUserInputWait,
		TodoBlockedReasonExternalResourceWait,
		TodoBlockedReasonUnknown:
		return true
	default:
		return false
	}
}

// normalizeTodoOwnerType 规范化 owner_type 字段。
func normalizeTodoOwnerType(ownerType string) string {
	return strings.ToLower(strings.TrimSpace(ownerType))
}

// normalizeTodoExecutor 规范化 executor 字段。
func normalizeTodoExecutor(executor string) string {
	normalized := strings.ToLower(strings.TrimSpace(executor))
	if normalized == "" {
		return TodoExecutorAgent
	}
	return normalized
}

// isValidTodoExecutor 判断 executor 是否受支持。
func isValidTodoExecutor(executor string) bool {
	switch normalizeTodoExecutor(executor) {
	case TodoExecutorAgent, TodoExecutorSubAgent:
		return true
	default:
		return false
	}
}

// isValidTodoOwnerType 判断 owner_type 是否受支持。
func isValidTodoOwnerType(ownerType string) bool {
	switch normalizeTodoOwnerType(ownerType) {
	case "", TodoOwnerTypeUser, TodoOwnerTypeAgent, TodoOwnerTypeSubAgent:
		return true
	default:
		return false
	}
}

// detectCyclicDependencies 使用 DFS 检测依赖图中的环。
func detectCyclicDependencies(items []TodoItem) error {
	if len(items) == 0 {
		return nil
	}

	graph := make(map[string][]string, len(items))
	for _, item := range items {
		graph[item.ID] = append([]string(nil), item.Dependencies...)
	}

	const (
		nodeUnvisited = 0
		nodeVisiting  = 1
		nodeVisited   = 2
	)

	state := make(map[string]int, len(items))
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case nodeVisiting:
			return fmt.Errorf("%w: %q", ErrCyclicDependency, id)
		case nodeVisited:
			return nil
		}

		state[id] = nodeVisiting
		for _, dependency := range graph[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = nodeVisited
		return nil
	}

	for id := range graph {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// TodoContentCheck 描述单个交付物的最小内容校验约束。
type TodoContentCheck struct {
	Artifact string   `json:"artifact,omitempty"`
	Contains []string `json:"contains,omitempty"`
}
