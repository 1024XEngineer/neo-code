package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentMode identifies the session's working mode.
type AgentMode string

const (
	AgentModePlan  AgentMode = "plan"
	AgentModeBuild AgentMode = "build"
)

// PlanStatus tracks the lifecycle of the current plan artifact.
type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "draft"
	PlanStatusApproved  PlanStatus = "approved"
	PlanStatusCompleted PlanStatus = "completed"
)

const (
	maxSummaryKeySteps    = 5
	maxSummaryConstraints = 5
	maxSummaryVerify      = 5
	maxSummaryTodoIDs     = 20
)

const (
	// AcceptCheckOutputOnly 表示仅需要最终回复文本作为交付物。
	AcceptCheckOutputOnly = "output_only"
	// AcceptCheckWorkspaceChange 表示需要运行期观测到 agent 产生工作区变更。
	AcceptCheckWorkspaceChange = "workspace_change"
	// AcceptCheckCommandSuccess 表示需要运行期命令成功事实。
	AcceptCheckCommandSuccess = "command_success"
	// AcceptCheckFileExists 表示需要运行期文件存在或写入事实。
	AcceptCheckFileExists = "file_exists"
	// AcceptCheckContentContains 表示需要运行期内容匹配事实。
	AcceptCheckContentContains = "content_contains"
	// AcceptCheckToolFact 表示需要运行期工具验证事实。
	AcceptCheckToolFact = "tool_fact"
)

// AcceptCheck 声明 plan 阶段模型提出的机器可检查验收项。
type AcceptCheck struct {
	ID       string            `json:"id,omitempty"`
	Kind     string            `json:"kind"`
	Target   string            `json:"target,omitempty"`
	Match    string            `json:"match,omitempty"`
	Required bool              `json:"required,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
}

// AcceptChecks 保存 plan 级验收项，并兼容读取旧的 []string 格式。
type AcceptChecks []AcceptCheck

// PlanArtifact stores the current plan persisted in the session.
type PlanArtifact struct {
	ID        string      `json:"id"`
	Revision  int         `json:"revision"`
	Status    PlanStatus  `json:"status"`
	Spec      PlanSpec    `json:"spec"`
	Summary   SummaryView `json:"summary"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// PlanSpec is the source of truth for the current plan.
type PlanSpec struct {
	Goal          string       `json:"goal"`
	Steps         []string     `json:"steps,omitempty"`
	Constraints   []string     `json:"constraints,omitempty"`
	Verify        AcceptChecks `json:"verify,omitempty"`
	Todos         []TodoItem   `json:"todos,omitempty"`
	OpenQuestions []string     `json:"open_questions,omitempty"`
}

// SummaryView is the compact projection derived from PlanSpec.
type SummaryView struct {
	Goal          string       `json:"goal"`
	KeySteps      []string     `json:"key_steps,omitempty"`
	Constraints   []string     `json:"constraints,omitempty"`
	Verify        AcceptChecks `json:"verify,omitempty"`
	ActiveTodoIDs []string     `json:"active_todo_ids,omitempty"`
}

// Clone returns a deep copy of the plan artifact.
func (p *PlanArtifact) Clone() *PlanArtifact {
	if p == nil {
		return nil
	}
	cloned := *p
	cloned.Spec = p.Spec.Clone()
	cloned.Summary = p.Summary.Clone()
	return &cloned
}

// Clone returns a deep copy of the plan spec.
func (p PlanSpec) Clone() PlanSpec {
	p.Goal = strings.TrimSpace(p.Goal)
	p.Steps = append([]string(nil), p.Steps...)
	p.Constraints = append([]string(nil), p.Constraints...)
	p.Verify = p.Verify.Clone()
	p.OpenQuestions = append([]string(nil), p.OpenQuestions...)
	p.Todos = cloneTodoItems(p.Todos)
	return p
}

// Clone returns a deep copy of the summary view.
func (s SummaryView) Clone() SummaryView {
	s.Goal = strings.TrimSpace(s.Goal)
	s.KeySteps = append([]string(nil), s.KeySteps...)
	s.Constraints = append([]string(nil), s.Constraints...)
	s.Verify = s.Verify.Clone()
	s.ActiveTodoIDs = append([]string(nil), s.ActiveTodoIDs...)
	return s
}

// NormalizeAgentMode normalizes empty and invalid values to build.
func NormalizeAgentMode(mode AgentMode) AgentMode {
	switch AgentMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case AgentModePlan:
		return AgentModePlan
	case AgentModeBuild:
		return AgentModeBuild
	default:
		return AgentModeBuild
	}
}

// NormalizePlanStatus normalizes empty and invalid values to draft.
func NormalizePlanStatus(status PlanStatus) PlanStatus {
	switch PlanStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case PlanStatusDraft:
		return PlanStatusDraft
	case PlanStatusApproved:
		return PlanStatusApproved
	case PlanStatusCompleted:
		return PlanStatusCompleted
	default:
		return PlanStatusDraft
	}
}

// NormalizePlanArtifact normalizes and validates the persisted plan artifact.
func NormalizePlanArtifact(plan *PlanArtifact) (*PlanArtifact, error) {
	if plan == nil {
		return nil, nil
	}
	cloned := plan.Clone()
	if cloned == nil {
		return nil, nil
	}
	cloned.ID = strings.TrimSpace(cloned.ID)
	if cloned.Revision <= 0 {
		cloned.Revision = 1
	}
	cloned.Status = NormalizePlanStatus(cloned.Status)
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = time.Now().UTC()
	}
	if cloned.UpdatedAt.IsZero() {
		cloned.UpdatedAt = cloned.CreatedAt
	} else {
		cloned.UpdatedAt = cloned.UpdatedAt.UTC()
	}

	spec, err := NormalizePlanSpec(cloned.Spec)
	if err != nil {
		return nil, err
	}
	cloned.Spec = spec
	if cloned.ID == "" {
		return nil, fmt.Errorf("session: plan id is empty")
	}
	cloned.Summary = NormalizeSummaryView(cloned.Summary, cloned.Spec)
	return cloned, nil
}

// NormalizePlanSpec normalizes a plan spec for persistence and later reuse.
func NormalizePlanSpec(spec PlanSpec) (PlanSpec, error) {
	spec = spec.Clone()
	spec.Goal = strings.TrimSpace(spec.Goal)
	spec.Steps = normalizeTodoTextList(spec.Steps)
	spec.Constraints = normalizeTodoTextList(spec.Constraints)
	spec.Verify = spec.Verify.Normalize()
	spec.OpenQuestions = normalizeTodoTextList(spec.OpenQuestions)

	todos, err := normalizeAndValidateTodos(spec.Todos)
	if err != nil {
		return PlanSpec{}, err
	}
	spec.Todos = todos

	if spec.Goal == "" {
		return PlanSpec{}, fmt.Errorf("session: plan goal is empty")
	}
	return spec, nil
}

// NormalizeSummaryView falls back to a built summary when needed.
func NormalizeSummaryView(summary SummaryView, spec PlanSpec) SummaryView {
	normalized := summary.Clone()
	normalized.Goal = strings.TrimSpace(normalized.Goal)
	normalized.KeySteps = normalizeTodoTextList(normalized.KeySteps)
	normalized.Constraints = normalizeTodoTextList(normalized.Constraints)
	normalized.Verify = normalized.Verify.Normalize()
	normalized.ActiveTodoIDs = normalizeTodoTextList(normalized.ActiveTodoIDs)
	if !summaryViewStructurallyValid(normalized, spec) {
		return BuildSummaryView(spec)
	}
	return normalized
}

// BuildSummaryView 从完整的方案规格文档，生成一份稳定、精炼的摘要
func BuildSummaryView(spec PlanSpec) SummaryView {
	spec, err := NormalizePlanSpec(spec)
	if err != nil {
		return SummaryView{}
	}
	return SummaryView{
		Goal:          spec.Goal,
		KeySteps:      clampStringList(spec.Steps, maxSummaryKeySteps),
		Constraints:   clampStringList(spec.Constraints, maxSummaryConstraints),
		Verify:        clampAcceptChecks(spec.Verify, maxSummaryVerify),
		ActiveTodoIDs: collectActiveTodoIDs(spec.Todos, maxSummaryTodoIDs),
	}
}

// RenderPlanContent renders the full plan text view for model context and logs.
func RenderPlanContent(spec PlanSpec) string {
	spec, err := NormalizePlanSpec(spec)
	if err != nil {
		return ""
	}

	sections := make([]string, 0, 6)
	sections = append(sections, "目标\n"+spec.Goal)
	if len(spec.Steps) > 0 {
		sections = append(sections, "实施步骤\n"+renderBulletList(spec.Steps))
	}
	if len(spec.Constraints) > 0 {
		sections = append(sections, "约束\n"+renderBulletList(spec.Constraints))
	}
	if len(spec.Verify) > 0 {
		sections = append(sections, "验证\n"+renderBulletList(spec.Verify.RenderLines()))
	}
	activeTodos := collectActiveTodoLines(spec.Todos)
	if len(activeTodos) > 0 {
		sections = append(sections, "当前待办\n"+renderBulletList(activeTodos))
	}
	if len(spec.OpenQuestions) > 0 {
		sections = append(sections, "未决问题\n"+renderBulletList(spec.OpenQuestions))
	}
	return strings.Join(sections, "\n\n")
}

func summaryViewStructurallyValid(summary SummaryView, spec PlanSpec) bool {
	if strings.TrimSpace(summary.Goal) == "" {
		return false
	}
	if len(summary.KeySteps) == 0 || len(summary.Verify) == 0 {
		return false
	}
	if len(summary.ActiveTodoIDs) == 0 {
		return len(spec.Todos) == 0
	}
	knownTodoIDs := make(map[string]struct{}, len(spec.Todos))
	for _, item := range spec.Todos {
		knownTodoIDs[item.ID] = struct{}{}
	}
	for _, id := range summary.ActiveTodoIDs {
		if _, ok := knownTodoIDs[id]; !ok {
			return false
		}
	}
	return true
}

func clampStringList(items []string, maxItems int) []string {
	normalized := normalizeTodoTextList(items)
	if len(normalized) <= maxItems || maxItems <= 0 {
		return normalized
	}
	return append([]string(nil), normalized[:maxItems]...)
}

// UnmarshalJSON 兼容读取新 AcceptCheck 对象数组与旧字符串数组。
func (checks *AcceptChecks) UnmarshalJSON(data []byte) error {
	var structured []AcceptCheck
	if err := json.Unmarshal(data, &structured); err == nil {
		*checks = AcceptChecks(structured).Normalize()
		return nil
	}
	var legacy []string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	migrated := make(AcceptChecks, 0, len(legacy))
	for _, item := range normalizeTodoTextList(legacy) {
		migrated = append(migrated, migrateLegacyAcceptCheck(item))
	}
	*checks = migrated.Normalize()
	return nil
}

// Clone 返回验收项深拷贝，避免调用方共享 Params map。
func (checks AcceptChecks) Clone() AcceptChecks {
	if len(checks) == 0 {
		return nil
	}
	out := make(AcceptChecks, 0, len(checks))
	for _, check := range checks {
		cloned := check
		cloned.ID = strings.TrimSpace(cloned.ID)
		cloned.Kind = strings.TrimSpace(cloned.Kind)
		cloned.Target = strings.TrimSpace(cloned.Target)
		cloned.Match = strings.TrimSpace(cloned.Match)
		if len(check.Params) > 0 {
			cloned.Params = make(map[string]string, len(check.Params))
			for key, value := range check.Params {
				key = strings.TrimSpace(key)
				value = strings.TrimSpace(value)
				if key == "" && value == "" {
					continue
				}
				cloned.Params[key] = value
			}
		}
		out = append(out, cloned)
	}
	return out
}

// Normalize 规范化验收项文本字段并迁移旧 kind 名称。
func (checks AcceptChecks) Normalize() AcceptChecks {
	if len(checks) == 0 {
		return nil
	}
	out := make(AcceptChecks, 0, len(checks))
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks.Clone() {
		check.ID = strings.TrimSpace(check.ID)
		check.Kind = normalizeAcceptCheckKind(check.Kind)
		check.Target = strings.TrimSpace(check.Target)
		check.Match = strings.TrimSpace(check.Match)
		key := check.Kind + "\x00" + check.Target + "\x00" + check.Match
		if key == "\x00\x00" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, check)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RenderLines 返回面向计划正文的稳定验收项文本。
func (checks AcceptChecks) RenderLines() []string {
	normalized := checks.Normalize()
	if len(normalized) == 0 {
		return nil
	}
	lines := make([]string, 0, len(normalized))
	for _, check := range normalized {
		label := check.Kind
		if check.Target != "" {
			label += ": " + check.Target
		}
		lines = append(lines, label)
	}
	return lines
}

func clampAcceptChecks(items AcceptChecks, maxItems int) AcceptChecks {
	normalized := items.Normalize()
	if len(normalized) <= maxItems || maxItems <= 0 {
		return normalized
	}
	return normalized[:maxItems].Clone()
}

func migrateLegacyAcceptCheck(value string) AcceptCheck {
	kind := AcceptCheckOutputOnly
	switch {
	case looksLikeCommand(value):
		kind = AcceptCheckCommandSuccess
	case looksLikePath(value):
		kind = AcceptCheckFileExists
	}
	return AcceptCheck{Kind: kind, Target: strings.TrimSpace(value), Required: true}
}

func normalizeAcceptCheckKind(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	switch normalized {
	case "command":
		return AcceptCheckCommandSuccess
	default:
		return normalized
	}
}

func looksLikeCommand(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return false
	}
	prefixes := []string{
		"go ", "go\t", "npm ", "pnpm ", "yarn ", "make", "cargo ", "python ", "pytest", "ruff ",
		"eslint", "tsc", "golangci-lint", "git ", "powershell ", "pwsh ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return strings.Contains(trimmed, " test ") || strings.Contains(trimmed, " build ")
}

func looksLikePath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, " ") {
		return false
	}
	return strings.Contains(trimmed, "/") ||
		strings.Contains(trimmed, "\\") ||
		strings.Contains(strings.TrimPrefix(trimmed, "."), ".")
}

func collectActiveTodoIDs(items []TodoItem, limit int) []string {
	if len(items) == 0 || limit <= 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status.IsTerminal() {
			continue
		}
		result = append(result, item.ID)
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func collectActiveTodoLines(items []TodoItem) []string {
	if len(items) == 0 {
		return nil
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status.IsTerminal() {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] %s (id=%s)", item.Status, item.Content, item.ID))
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func renderBulletList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- "+trimmed)
	}
	return strings.Join(lines, "\n")
}
