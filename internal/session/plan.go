package session

import (
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
	Goal          string     `json:"goal"`
	Steps         []string   `json:"steps,omitempty"`
	Constraints   []string   `json:"constraints,omitempty"`
	Verify        []string   `json:"verify,omitempty"`
	Todos         []TodoItem `json:"todos,omitempty"`
	OpenQuestions []string   `json:"open_questions,omitempty"`
}

// SummaryView is the compact projection derived from PlanSpec.
type SummaryView struct {
	Goal          string   `json:"goal"`
	KeySteps      []string `json:"key_steps,omitempty"`
	Constraints   []string `json:"constraints,omitempty"`
	Verify        []string `json:"verify,omitempty"`
	ActiveTodoIDs []string `json:"active_todo_ids,omitempty"`
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
	p.Verify = append([]string(nil), p.Verify...)
	p.OpenQuestions = append([]string(nil), p.OpenQuestions...)
	p.Todos = cloneTodoItems(p.Todos)
	return p
}

// Clone returns a deep copy of the summary view.
func (s SummaryView) Clone() SummaryView {
	s.Goal = strings.TrimSpace(s.Goal)
	s.KeySteps = append([]string(nil), s.KeySteps...)
	s.Constraints = append([]string(nil), s.Constraints...)
	s.Verify = append([]string(nil), s.Verify...)
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
	spec.Verify = normalizeTodoTextList(spec.Verify)
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
	normalized.Verify = normalizeTodoTextList(normalized.Verify)
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
		Verify:        clampStringList(spec.Verify, maxSummaryVerify),
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
		sections = append(sections, "验证\n"+renderBulletList(spec.Verify))
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
