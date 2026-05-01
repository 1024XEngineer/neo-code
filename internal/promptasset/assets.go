package promptasset

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed templates/**/*.md templates/**/*.txt
var templateFS embed.FS

// Section 表示主会话 system prompt 中的一个静态 section 模板。
type Section struct {
	Title   string
	Content string
}

const (
	compactTaskStateContractPlaceholder     = "{{TASK_STATE_JSON_CONTRACT}}"
	compactSummaryFormatTemplatePlaceholder = "{{DISPLAY_SUMMARY_FORMAT_TEMPLATE}}"
)

var coreSections = loadCoreSections()

var noProgressReminder = mustReadTemplate("templates/runtime/self_healing_no_progress.txt")

var repeatCycleReminder = mustReadTemplate("templates/runtime/self_healing_repeat_cycle.txt")

var compactSystemPromptTemplate = mustReadTemplate("templates/context/compact_system_prompt.md")

var planModePlanPrompt = mustReadTemplate("templates/context/plan_mode_plan.md")

var planModeBuildExecutePrompt = mustReadTemplate("templates/context/plan_mode_build_execute.md")

var researcherRolePrompt = mustReadTemplate("templates/subagent/researcher.md")

var coderRolePrompt = mustReadTemplate("templates/subagent/coder.md")

var reviewerRolePrompt = mustReadTemplate("templates/subagent/reviewer.md")

// CoreSections 返回主会话固定核心 prompt sections 的有序副本。
func CoreSections() []Section {
	return append([]Section(nil), coreSections...)
}

// NoProgressReminder 返回 runtime 无进展自愈提醒文案。
func NoProgressReminder() string {
	return noProgressReminder
}

// RepeatCycleReminder 返回 runtime 重复同参工具调用自愈提醒文案。
func RepeatCycleReminder() string {
	return repeatCycleReminder
}

// CompactSystemPrompt 返回 compact 场景使用的静态 system prompt。
func CompactSystemPrompt(taskStateContract string, summaryFormat string) string {
	replacer := strings.NewReplacer(
		compactTaskStateContractPlaceholder, strings.TrimSpace(taskStateContract),
		compactSummaryFormatTemplatePlaceholder, strings.TrimSpace(summaryFormat),
	)
	return strings.TrimSpace(replacer.Replace(compactSystemPromptTemplate))
}

// PlanModePrompt 返回 plan/build 不同阶段对应的静态提示模板，供 context source 注入动态字段前复用。
func PlanModePrompt(stage string) string {
	switch strings.TrimSpace(stage) {
	case "plan":
		return planModePlanPrompt
	case "build_execute":
		return planModeBuildExecutePrompt
	default:
		return ""
	}
}

// ResearcherRolePrompt 返回 researcher 子代理基础 prompt。
func ResearcherRolePrompt() string {
	return researcherRolePrompt
}

// CoderRolePrompt 返回 coder 子代理基础 prompt。
func CoderRolePrompt() string {
	return coderRolePrompt
}

// ReviewerRolePrompt 返回 reviewer 子代理基础 prompt。
func ReviewerRolePrompt() string {
	return reviewerRolePrompt
}

// loadCoreSections 按固定顺序加载主会话核心 section 模板。
func loadCoreSections() []Section {
	return []Section{
		{
			Title:   "Agent Identity",
			Content: mustReadTemplate("templates/core/agent_identity.md"),
		},
		{
			Title:   "Tool Usage",
			Content: mustReadTemplate("templates/core/tool_usage.md"),
		},
		{
			Title:   "Failure Recovery",
			Content: mustReadTemplate("templates/core/failure_recovery.md"),
		},
		{
			Title:   "Response Style",
			Content: mustReadTemplate("templates/core/response_style.md"),
		},
		{
			Title:   "Security Boundaries",
			Content: mustReadTemplate("templates/core/security_boundaries.md"),
		},
		{
			Title:   "Context Management",
			Content: mustReadTemplate("templates/core/context_management.md"),
		},
	}
}

// mustReadTemplate 从嵌入模板集中读取指定文件，缺失时直接 panic，避免静默退化。
func mustReadTemplate(path string) string {
	data, err := templateFS.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("promptasset: read %s: %v", path, err))
	}
	return strings.TrimSpace(string(data))
}
