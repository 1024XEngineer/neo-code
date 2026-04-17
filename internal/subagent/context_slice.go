package subagent

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	agentsession "neo-code/internal/session"
)

const (
	defaultTaskContextMaxChars               = 3200
	defaultTaskContextMaxTodoFragments       = 6
	defaultTaskContextMaxDependencyArtifacts = 8
	defaultTaskContextMaxRelatedFiles        = 6
)

// TaskContextFileSummary 描述任务相关文件的稳定摘要信息。
type TaskContextFileSummary struct {
	Path    string
	Summary string
}

// TaskTodoFragment 描述注入子代理的 Todo 片段。
type TaskTodoFragment struct {
	ID       string
	Status   agentsession.TodoStatus
	Content  string
	Priority int
}

// TaskContextDescriptor 描述可重建 Task Context Slice 的最小引用集合。
type TaskContextDescriptor struct {
	TaskID            string
	DependencyTaskIDs []string
	TodoFragmentIDs   []string
	ArtifactRefs      []string
	RelatedFilePaths  []string
	SkillIDs          []string
}

// normalize 规整 descriptor 字段，保证 compact 后重建输入稳定。
func (d TaskContextDescriptor) normalize() TaskContextDescriptor {
	d.TaskID = strings.TrimSpace(d.TaskID)
	d.DependencyTaskIDs = normalizeDescriptorOptionalSlice(d.DependencyTaskIDs)
	d.TodoFragmentIDs = normalizeDescriptorOptionalSlice(d.TodoFragmentIDs)
	d.ArtifactRefs = normalizeDescriptorOptionalSlice(d.ArtifactRefs)
	d.RelatedFilePaths = normalizeDescriptorOptionalSlice(d.RelatedFilePaths)
	d.SkillIDs = normalizeDescriptorOptionalSlice(d.SkillIDs)
	return d
}

// TaskContextSlice 描述子代理任务可消费的最小必要上下文。
type TaskContextSlice struct {
	TaskID              string
	Goal                string
	Acceptance          []string
	DependencyArtifacts []string
	RelatedFiles        []TaskContextFileSummary
	ActivatedSkills     []string
	TodoFragment        []TaskTodoFragment
	Descriptor          TaskContextDescriptor
	BudgetChars         int
	Truncated           bool
}

// Render 以稳定顺序渲染切片，供执行引擎直接消费。
func (s TaskContextSlice) Render() string {
	var b strings.Builder
	if taskID := sanitizeRenderValue(s.TaskID); taskID != "" {
		b.WriteString("task_id: ")
		b.WriteString(taskID)
		b.WriteString("\n")
	}
	if goal := sanitizeRenderValue(s.Goal); goal != "" {
		b.WriteString("goal: ")
		b.WriteString(goal)
		b.WriteString("\n")
	}
	writeBulletSection(&b, "acceptance", s.Acceptance)
	writeBulletSection(&b, "dependency_artifacts", s.DependencyArtifacts)
	writeFileSummarySection(&b, "related_files", s.RelatedFiles)
	writeBulletSection(&b, "activated_skills", s.ActivatedSkills)
	writeTodoFragmentSection(&b, "todo_fragment", s.TodoFragment)
	return strings.TrimSpace(b.String())
}

// TaskContextSliceInput 描述构建任务上下文切片所需的输入。
type TaskContextSliceInput struct {
	Task                   agentsession.TodoItem
	Todos                  map[string]agentsession.TodoItem
	ReadOnlyTodos          bool
	ActivatedSkills        []string
	RelatedFiles           []TaskContextFileSummary
	MaxChars               int
	MaxTodoFragments       int
	MaxDependencyArtifacts int
	MaxRelatedFiles        int
}

// TaskContextRebuildInput 描述通过 descriptor 重建切片所需输入。
type TaskContextRebuildInput struct {
	Descriptor             TaskContextDescriptor
	Todos                  map[string]agentsession.TodoItem
	ActivatedSkills        []string
	RelatedFiles           []TaskContextFileSummary
	MaxChars               int
	MaxTodoFragments       int
	MaxDependencyArtifacts int
	MaxRelatedFiles        int
}

// BuildTaskContextSlice 基于 Todo DAG 快照构建子代理最小上下文切片。
func BuildTaskContextSlice(input TaskContextSliceInput) TaskContextSlice {
	in := normalizeTaskContextSliceInput(input)
	taskID := strings.TrimSpace(in.Task.ID)
	dependencyIDs := dedupeAndTrim(in.Task.Dependencies)
	slice := TaskContextSlice{
		TaskID:      taskID,
		Goal:        strings.TrimSpace(in.Task.Content),
		Acceptance:  dedupeAndTrim(in.Task.Acceptance),
		BudgetChars: in.MaxChars,
	}

	primaryFragments, additionalFragments := collectTodoFragments(
		in.Task,
		in.Todos,
		dependencyIDs,
		in.MaxTodoFragments,
	)
	slice.TodoFragment = append(slice.TodoFragment, primaryFragments...)
	slice.DependencyArtifacts = collectDependencyArtifacts(in.Todos, dependencyIDs, in.MaxDependencyArtifacts)
	slice.RelatedFiles = mergeRelatedFiles(in.RelatedFiles, slice.DependencyArtifacts, in.MaxRelatedFiles)
	slice.ActivatedSkills = append(slice.ActivatedSkills, in.ActivatedSkills...)
	slice.TodoFragment = append(slice.TodoFragment, additionalFragments...)

	slice.Truncated = enforceTaskContextBudget(&slice, in.MaxChars)
	slice.Descriptor = buildTaskContextDescriptor(slice, dependencyIDs)
	return slice
}

// RebuildTaskContextSlice 仅基于 descriptor 与当前快照重建上下文切片。
func RebuildTaskContextSlice(input TaskContextRebuildInput) (TaskContextSlice, error) {
	descriptor := input.Descriptor.normalize()
	if descriptor.TaskID == "" {
		return TaskContextSlice{}, errorsf("task context descriptor task id is required")
	}
	task, ok := input.Todos[descriptor.TaskID]
	if !ok {
		return TaskContextSlice{}, fmt.Errorf("subagent: task context descriptor task %q not found", descriptor.TaskID)
	}
	task.Dependencies = filterByAllowlist(task.Dependencies, descriptor.DependencyTaskIDs)
	relatedFiles := filterRelatedFilesByPath(input.RelatedFiles, descriptor.RelatedFilePaths)
	activatedSkills := filterByAllowlist(input.ActivatedSkills, descriptor.SkillIDs)

	slice := BuildTaskContextSlice(TaskContextSliceInput{
		Task:                   task,
		Todos:                  input.Todos,
		ReadOnlyTodos:          true,
		ActivatedSkills:        activatedSkills,
		RelatedFiles:           relatedFiles,
		MaxChars:               input.MaxChars,
		MaxTodoFragments:       input.MaxTodoFragments,
		MaxDependencyArtifacts: input.MaxDependencyArtifacts,
		MaxRelatedFiles:        input.MaxRelatedFiles,
	})

	slice.DependencyArtifacts = filterByAllowlist(slice.DependencyArtifacts, descriptor.ArtifactRefs)
	slice.RelatedFiles = filterRelatedFilesByPath(slice.RelatedFiles, descriptor.RelatedFilePaths)
	slice.TodoFragment = filterTodoFragmentsByID(slice.TodoFragment, descriptor.TodoFragmentIDs)
	slice.Truncated = enforceTaskContextBudget(&slice, slice.BudgetChars) || slice.Truncated
	slice.Descriptor = buildTaskContextDescriptor(slice, descriptor.DependencyTaskIDs)
	return slice, nil
}

// normalizeTaskContextSliceInput 规整调用参数并注入默认预算。
func normalizeTaskContextSliceInput(input TaskContextSliceInput) TaskContextSliceInput {
	out := input
	if out.MaxChars <= 0 {
		out.MaxChars = defaultTaskContextMaxChars
	}
	if out.MaxTodoFragments <= 0 {
		out.MaxTodoFragments = defaultTaskContextMaxTodoFragments
	}
	if out.MaxDependencyArtifacts <= 0 {
		out.MaxDependencyArtifacts = defaultTaskContextMaxDependencyArtifacts
	}
	if out.MaxRelatedFiles <= 0 {
		out.MaxRelatedFiles = defaultTaskContextMaxRelatedFiles
	}
	if out.ReadOnlyTodos {
		if out.Todos == nil {
			out.Todos = make(map[string]agentsession.TodoItem)
		}
	} else {
		out.Todos = cloneTodoMap(out.Todos)
	}
	out.ActivatedSkills = dedupeAndTrim(out.ActivatedSkills)
	out.RelatedFiles = normalizeContextFileSummaries(out.RelatedFiles)
	return out
}

// collectTodoFragments 提取当前任务、关键依赖和额外运行中 Todo 片段。
func collectTodoFragments(
	task agentsession.TodoItem,
	todos map[string]agentsession.TodoItem,
	dependencyIDs []string,
	maxFragments int,
) ([]TaskTodoFragment, []TaskTodoFragment) {
	if maxFragments <= 0 {
		return nil, nil
	}
	primary := make([]TaskTodoFragment, 0, maxFragments)
	seen := make(map[string]struct{}, maxFragments)
	pushPrimary := func(item agentsession.TodoItem) {
		if len(primary) >= maxFragments {
			return
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		primary = append(primary, toTaskTodoFragment(item))
	}

	pushPrimary(task)
	for _, depID := range dependencyIDs {
		if dep, ok := todos[depID]; ok {
			pushPrimary(dep)
		}
	}

	extras := collectAdditionalTodoFragments(task.ID, todos, seen, maxFragments-len(primary))
	return primary, extras
}

// collectAdditionalTodoFragments 追加高优先级的进行中/阻塞任务片段，保证顺序稳定。
func collectAdditionalTodoFragments(
	currentTaskID string,
	todos map[string]agentsession.TodoItem,
	existing map[string]struct{},
	limit int,
) []TaskTodoFragment {
	if limit <= 0 {
		return nil
	}
	candidates := make([]agentsession.TodoItem, 0, len(todos))
	for id, item := range todos {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(currentTaskID)) {
			continue
		}
		if _, ok := existing[strings.TrimSpace(id)]; ok {
			continue
		}
		if item.Status.IsTerminal() {
			continue
		}
		candidates = append(candidates, item.Clone())
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		lp := todoStatusRank(left.Status)
		rp := todoStatusRank(right.Status)
		if lp != rp {
			return lp < rp
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})

	result := make([]TaskTodoFragment, 0, limit)
	for _, item := range candidates {
		if len(result) >= limit {
			break
		}
		result = append(result, toTaskTodoFragment(item))
	}
	return result
}

// collectDependencyArtifacts 读取依赖任务产物引用，并按预算裁剪。
func collectDependencyArtifacts(
	todos map[string]agentsession.TodoItem,
	dependencyIDs []string,
	limit int,
) []string {
	if limit <= 0 {
		return nil
	}
	artifacts := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, depID := range dependencyIDs {
		dependency, ok := todos[depID]
		if !ok {
			continue
		}
		for _, raw := range dependency.Artifacts {
			item := strings.TrimSpace(raw)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			artifacts = append(artifacts, item)
			if len(artifacts) >= limit {
				return artifacts
			}
		}
	}
	return artifacts
}

// mergeRelatedFiles 合并输入文件摘要和依赖产物路径，生成稳定文件摘要集合。
func mergeRelatedFiles(
	related []TaskContextFileSummary,
	dependencyArtifacts []string,
	limit int,
) []TaskContextFileSummary {
	if limit <= 0 {
		return nil
	}
	normalized := normalizeContextFileSummaries(related)
	fileByPath := make(map[string]TaskContextFileSummary, len(normalized))
	result := make([]TaskContextFileSummary, 0, limit)
	for _, item := range normalized {
		pathKey := strings.ToLower(strings.TrimSpace(item.Path))
		if pathKey == "" {
			continue
		}
		fileByPath[pathKey] = item
		result = append(result, item)
		if len(result) >= limit {
			return result
		}
	}

	for _, artifact := range dependencyArtifacts {
		if len(result) >= limit {
			break
		}
		path := strings.TrimSpace(artifact)
		if path == "" {
			continue
		}
		pathKey := strings.ToLower(path)
		if _, exists := fileByPath[pathKey]; exists {
			continue
		}
		fileByPath[pathKey] = TaskContextFileSummary{
			Path:    path,
			Summary: "来自依赖产物引用",
		}
		result = append(result, fileByPath[pathKey])
	}
	return result
}

// enforceTaskContextBudget 按预算裁剪低优先级信息，保证关键信息优先保留。
func enforceTaskContextBudget(slice *TaskContextSlice, maxChars int) bool {
	if slice == nil || maxChars <= 0 {
		return false
	}
	currentChars := contextSliceRuneCount(*slice)
	if currentChars <= maxChars {
		return false
	}
	truncated := false
	refreshChars := func() {
		currentChars = contextSliceRuneCount(*slice)
	}
	trimList := func(list *[]string, minKeep int) {
		for len(*list) > minKeep && currentChars > maxChars {
			*list = (*list)[:len(*list)-1]
			truncated = true
			refreshChars()
		}
	}
	trimFiles := func(minKeep int) {
		for len(slice.RelatedFiles) > minKeep && currentChars > maxChars {
			slice.RelatedFiles = slice.RelatedFiles[:len(slice.RelatedFiles)-1]
			truncated = true
			refreshChars()
		}
	}
	trimTodos := func(minKeep int) {
		for len(slice.TodoFragment) > minKeep && currentChars > maxChars {
			slice.TodoFragment = slice.TodoFragment[:len(slice.TodoFragment)-1]
			truncated = true
			refreshChars()
		}
	}

	trimList(&slice.ActivatedSkills, 0)
	trimFiles(0)
	trimTodos(1)
	trimList(&slice.DependencyArtifacts, 0)
	trimList(&slice.Acceptance, 1)
	if currentChars <= maxChars {
		return truncated
	}

	goalLimit := maxChars / 2
	if goalLimit < 1 {
		goalLimit = 1
	}
	slice.Goal = truncateRunes(slice.Goal, goalLimit)
	if len(slice.Acceptance) > 0 {
		acceptanceLimit := maxChars / 6
		if acceptanceLimit < 1 {
			acceptanceLimit = 1
		}
		for idx := range slice.Acceptance {
			slice.Acceptance[idx] = truncateRunes(slice.Acceptance[idx], acceptanceLimit)
		}
	}
	if len(slice.TodoFragment) > 0 {
		todoLimit := maxChars / 3
		if todoLimit < 1 {
			todoLimit = 1
		}
		slice.TodoFragment[0].Content = truncateRunes(slice.TodoFragment[0].Content, todoLimit)
	}
	refreshChars()
	if currentChars <= maxChars {
		return true
	}

	slice.DependencyArtifacts = nil
	slice.RelatedFiles = nil
	slice.ActivatedSkills = nil
	if len(slice.Acceptance) > 1 {
		slice.Acceptance = slice.Acceptance[:1]
	}
	if len(slice.TodoFragment) > 1 {
		slice.TodoFragment = slice.TodoFragment[:1]
	}
	refreshChars()
	if currentChars <= maxChars {
		return true
	}

	for currentChars > maxChars {
		switch {
		case len(slice.TodoFragment) > 0 && len([]rune(slice.TodoFragment[0].Content)) > 0:
			slice.TodoFragment[0].Content = trimOneRune(slice.TodoFragment[0].Content)
		case len(slice.Acceptance) > 0 && len([]rune(slice.Acceptance[0])) > 1:
			slice.Acceptance[0] = trimOneRune(slice.Acceptance[0])
		case len([]rune(slice.Goal)) > 1:
			slice.Goal = trimOneRune(slice.Goal)
		case len(slice.Acceptance) > 1:
			slice.Acceptance = slice.Acceptance[:1]
		default:
			switch {
			case len(slice.Acceptance) > 0:
				slice.Acceptance = nil
			case len(slice.TodoFragment) > 0:
				slice.TodoFragment = nil
			case strings.TrimSpace(slice.Goal) == "":
				slice.Goal = "…"
			default:
				return true
			}
		}
		refreshChars()
		if strings.TrimSpace(slice.Goal) == "" {
			slice.Goal = "…"
			refreshChars()
		}
		if currentChars > maxChars && len(slice.Acceptance) == 0 && len(slice.TodoFragment) == 0 &&
			len([]rune(strings.TrimSpace(slice.Goal))) <= 1 {
			if forceFitTaskContextBudget(slice, maxChars, &currentChars) {
				truncated = true
			}
			return true
		}
	}
	return true
}

// forceFitTaskContextBudget 在极端预算下执行硬性收缩，确保渲染长度不超过预算。
func forceFitTaskContextBudget(slice *TaskContextSlice, maxChars int, currentChars *int) bool {
	if slice == nil || currentChars == nil || maxChars < 0 || *currentChars <= maxChars {
		return false
	}
	changed := false
	refreshChars := func() {
		*currentChars = contextSliceRuneCount(*slice)
	}
	for *currentChars > maxChars {
		switch {
		case strings.TrimSpace(slice.TaskID) != "":
			slice.TaskID = trimOneRune(slice.TaskID)
		case len(slice.Acceptance) > 0:
			slice.Acceptance = nil
		case len(slice.TodoFragment) > 0:
			slice.TodoFragment = nil
		case len(slice.DependencyArtifacts) > 0:
			slice.DependencyArtifacts = nil
		case len(slice.RelatedFiles) > 0:
			slice.RelatedFiles = nil
		case len(slice.ActivatedSkills) > 0:
			slice.ActivatedSkills = nil
		case strings.TrimSpace(slice.Goal) != "":
			slice.Goal = trimOneRune(slice.Goal)
		default:
			return changed
		}
		changed = true
		refreshChars()
	}
	return changed
}

// buildTaskContextDescriptor 生成可用于 compact 后重建的 descriptor。
func buildTaskContextDescriptor(slice TaskContextSlice, dependencyIDs []string) TaskContextDescriptor {
	fragmentIDs := make([]string, 0, len(slice.TodoFragment))
	for _, fragment := range slice.TodoFragment {
		id := strings.TrimSpace(fragment.ID)
		if id == "" {
			continue
		}
		fragmentIDs = append(fragmentIDs, id)
	}
	relatedPaths := make([]string, 0, len(slice.RelatedFiles))
	for _, file := range slice.RelatedFiles {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			continue
		}
		relatedPaths = append(relatedPaths, path)
	}
	return TaskContextDescriptor{
		TaskID:            strings.TrimSpace(slice.TaskID),
		DependencyTaskIDs: dedupeAndTrim(dependencyIDs),
		TodoFragmentIDs:   dedupeAndTrim(fragmentIDs),
		ArtifactRefs:      dedupeAndTrim(slice.DependencyArtifacts),
		RelatedFilePaths:  dedupeAndTrim(relatedPaths),
		SkillIDs:          dedupeAndTrim(slice.ActivatedSkills),
	}.normalize()
}

// normalizeContextFileSummaries 规整文件摘要输入并保持稳定顺序。
func normalizeContextFileSummaries(items []TaskContextFileSummary) []TaskContextFileSummary {
	if len(items) == 0 {
		return nil
	}
	result := make([]TaskContextFileSummary, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, TaskContextFileSummary{
			Path:    path,
			Summary: strings.TrimSpace(item.Summary),
		})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// filterRelatedFilesByPath 根据路径白名单过滤文件摘要，保持输入顺序。
func filterRelatedFilesByPath(items []TaskContextFileSummary, allow []string) []TaskContextFileSummary {
	if allow == nil {
		return normalizeContextFileSummaries(items)
	}
	allowed := dedupeAndTrim(allow)
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		set[strings.ToLower(strings.TrimSpace(path))] = struct{}{}
	}
	result := make([]TaskContextFileSummary, 0, len(items))
	for _, item := range normalizeContextFileSummaries(items) {
		if _, ok := set[strings.ToLower(strings.TrimSpace(item.Path))]; !ok {
			continue
		}
		result = append(result, item)
	}
	return result
}

// filterByAllowlist 根据白名单过滤字符串切片，并保持原始顺序。
func filterByAllowlist(items []string, allow []string) []string {
	if allow == nil {
		return dedupeAndTrim(items)
	}
	allowed := dedupeAndTrim(allow)
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		set[strings.ToLower(strings.TrimSpace(item))] = struct{}{}
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := set[key]; !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// filterTodoFragmentsByID 根据 Todo ID 白名单过滤片段。
func filterTodoFragmentsByID(fragments []TaskTodoFragment, allow []string) []TaskTodoFragment {
	if allow == nil {
		result := make([]TaskTodoFragment, len(fragments))
		copy(result, fragments)
		return result
	}
	allowed := dedupeAndTrim(allow)
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		set[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	result := make([]TaskTodoFragment, 0, len(fragments))
	for _, item := range fragments {
		if _, ok := set[strings.ToLower(strings.TrimSpace(item.ID))]; !ok {
			continue
		}
		result = append(result, item)
	}
	return result
}

// cloneTodoMap 深拷贝 Todo 映射，避免上层引用被修改。
func cloneTodoMap(src map[string]agentsession.TodoItem) map[string]agentsession.TodoItem {
	if len(src) == 0 {
		return make(map[string]agentsession.TodoItem)
	}
	dst := make(map[string]agentsession.TodoItem, len(src))
	for id, item := range src {
		dst[strings.TrimSpace(id)] = item.Clone()
	}
	return dst
}

// toTaskTodoFragment 将 session Todo 转换为稳定的上下文片段结构。
func toTaskTodoFragment(item agentsession.TodoItem) TaskTodoFragment {
	return TaskTodoFragment{
		ID:       strings.TrimSpace(item.ID),
		Status:   item.Status,
		Content:  strings.TrimSpace(item.Content),
		Priority: item.Priority,
	}
}

// todoStatusRank 定义 Todo 状态在上下文中的优先级。
func todoStatusRank(status agentsession.TodoStatus) int {
	switch status {
	case agentsession.TodoStatusInProgress:
		return 0
	case agentsession.TodoStatusBlocked:
		return 1
	case agentsession.TodoStatusPending:
		return 2
	default:
		return 3
	}
}

// contextSliceRuneCount 计算渲染后上下文切片字符数，作为预算近似指标。
func contextSliceRuneCount(slice TaskContextSlice) int {
	lines := make([]string, 0, 16)
	if taskID := sanitizeRenderValue(slice.TaskID); taskID != "" {
		lines = append(lines, "task_id: "+taskID)
	}
	if goal := sanitizeRenderValue(slice.Goal); goal != "" {
		lines = append(lines, "goal: "+goal)
	}
	lines = append(lines, renderedBulletLines("acceptance", slice.Acceptance)...)
	lines = append(lines, renderedBulletLines("dependency_artifacts", slice.DependencyArtifacts)...)
	lines = append(lines, renderedFileSummaryLines("related_files", slice.RelatedFiles)...)
	lines = append(lines, renderedBulletLines("activated_skills", slice.ActivatedSkills)...)
	lines = append(lines, renderedTodoLines("todo_fragment", slice.TodoFragment)...)
	if len(lines) == 0 {
		return 0
	}
	total := len(lines) - 1
	for _, line := range lines {
		total += len([]rune(line))
	}
	return total
}

// truncateRunes 按 rune 长度裁剪文本并保留省略标记。
func truncateRunes(text string, max int) string {
	trimmed := strings.TrimSpace(text)
	if max <= 0 || trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= max {
		return trimmed
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// trimOneRune 删除字符串末尾一个 rune，用于预算极限场景的渐进收缩。
func trimOneRune(text string) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) <= 1 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

// writeBulletSection 以统一 bullet 结构渲染字符串切片。
func writeBulletSection(builder *strings.Builder, title string, items []string) {
	if builder == nil || len(items) == 0 {
		return
	}
	for _, line := range renderedBulletLines(title, items) {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// writeFileSummarySection 渲染相关文件摘要列表。
func writeFileSummarySection(builder *strings.Builder, title string, items []TaskContextFileSummary) {
	if builder == nil || len(items) == 0 {
		return
	}
	for _, line := range renderedFileSummaryLines(title, items) {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// writeTodoFragmentSection 渲染 Todo 片段列表，便于模型识别优先级和状态。
func writeTodoFragmentSection(builder *strings.Builder, title string, items []TaskTodoFragment) {
	if builder == nil || len(items) == 0 {
		return
	}
	for _, line := range renderedTodoLines(title, items) {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// renderedBulletLines 生成 bullet section 的稳定渲染行。
func renderedBulletLines(title string, items []string) []string {
	rows := make([]string, 0, len(items)+1)
	for _, item := range items {
		sanitized := sanitizeRenderValue(item)
		if sanitized == "" {
			continue
		}
		if len(rows) == 0 {
			rows = append(rows, title+":")
		}
		rows = append(rows, "- "+sanitized)
	}
	return rows
}

// renderedFileSummaryLines 生成相关文件 section 的稳定渲染行。
func renderedFileSummaryLines(title string, items []TaskContextFileSummary) []string {
	rows := make([]string, 0, len(items)+1)
	for _, file := range items {
		path := sanitizeRenderValue(file.Path)
		if path == "" {
			continue
		}
		line := "- path: " + path
		if summary := sanitizeRenderValue(file.Summary); summary != "" {
			line += " | summary: " + summary
		}
		if len(rows) == 0 {
			rows = append(rows, title+":")
		}
		rows = append(rows, line)
	}
	return rows
}

// renderedTodoLines 生成 Todo 片段 section 的稳定渲染行。
func renderedTodoLines(title string, items []TaskTodoFragment) []string {
	rows := make([]string, 0, len(items)+1)
	for _, item := range items {
		id := sanitizeRenderValue(item.ID)
		if id == "" {
			continue
		}
		line := "- " + id + " [" + sanitizeRenderValue(string(item.Status)) + "]"
		if item.Priority != 0 {
			line += " p=" + fmt.Sprintf("%d", item.Priority)
		}
		if content := sanitizeRenderValue(item.Content); content != "" {
			line += ": " + content
		}
		if len(rows) == 0 {
			rows = append(rows, title+":")
		}
		rows = append(rows, line)
	}
	return rows
}

// sanitizeRenderValue 把换行和控制字符规整为空格，避免渲染结构被注入。
func sanitizeRenderValue(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(trimmed))
	lastSpace := false
	for _, r := range trimmed {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			r = ' '
		case unicode.IsControl(r):
			continue
		}
		if unicode.IsSpace(r) {
			if lastSpace {
				continue
			}
			lastSpace = true
			builder.WriteRune(' ')
			continue
		}
		lastSpace = false
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

// normalizeDescriptorOptionalSlice 规整 descriptor 列表字段并保留“显式空列表”语义。
func normalizeDescriptorOptionalSlice(items []string) []string {
	if items == nil {
		return nil
	}
	normalized := dedupeAndTrim(items)
	if normalized == nil {
		return []string{}
	}
	return normalized
}
