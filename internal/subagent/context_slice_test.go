package subagent

import (
	"slices"
	"strings"
	"testing"
	"time"

	agentsession "neo-code/internal/session"
)

func TestBuildTaskContextSliceIncludesExpectedSections(t *testing.T) {
	t.Parallel()

	now := time.Unix(1710000000, 0).UTC()
	task := agentsession.TodoItem{
		ID:           "task-main",
		Content:      "实现子代理任务调度",
		Status:       agentsession.TodoStatusInProgress,
		Priority:     4,
		Dependencies: []string{"dep-a", "dep-b"},
		Acceptance:   []string{"通过单元测试", "通过集成测试"},
		CreatedAt:    now,
	}
	todos := map[string]agentsession.TodoItem{
		"dep-a": {
			ID:        "dep-a",
			Content:   "补齐权限能力",
			Status:    agentsession.TodoStatusCompleted,
			Priority:  2,
			Artifacts: []string{"docs/security.md", "artifacts/security-report.json"},
			CreatedAt: now.Add(-2 * time.Hour),
		},
		"dep-b": {
			ID:        "dep-b",
			Content:   "补齐 TODO 模型",
			Status:    agentsession.TodoStatusCompleted,
			Priority:  3,
			Artifacts: []string{"docs/todo.md"},
			CreatedAt: now.Add(-time.Hour),
		},
		"peer": {
			ID:        "peer",
			Content:   "并行修复覆盖率",
			Status:    agentsession.TodoStatusInProgress,
			Priority:  5,
			CreatedAt: now.Add(-30 * time.Minute),
		},
	}

	slice := BuildTaskContextSlice(TaskContextSliceInput{
		Task:            task,
		Todos:           todos,
		ActivatedSkills: []string{"go-review", "go-review", "todo-planner"},
		RelatedFiles: []TaskContextFileSummary{
			{Path: "internal/subagent/scheduler.go", Summary: "调度主循环"},
		},
		MaxChars: 6000,
	})

	if slice.TaskID != "task-main" {
		t.Fatalf("TaskID = %q, want task-main", slice.TaskID)
	}
	if slice.Goal != "实现子代理任务调度" {
		t.Fatalf("Goal = %q, want 实现子代理任务调度", slice.Goal)
	}
	if len(slice.DependencyArtifacts) != 3 {
		t.Fatalf("DependencyArtifacts len = %d, want 3", len(slice.DependencyArtifacts))
	}
	if len(slice.ActivatedSkills) != 2 {
		t.Fatalf("ActivatedSkills len = %d, want 2", len(slice.ActivatedSkills))
	}
	if len(slice.TodoFragment) < 3 {
		t.Fatalf("TodoFragment len = %d, want >= 3", len(slice.TodoFragment))
	}
	if slice.TodoFragment[0].ID != "task-main" {
		t.Fatalf("TodoFragment[0].ID = %q, want task-main", slice.TodoFragment[0].ID)
	}
	if slice.Descriptor.TaskID != "task-main" {
		t.Fatalf("Descriptor.TaskID = %q, want task-main", slice.Descriptor.TaskID)
	}
	if !slices.Equal(slice.Descriptor.DependencyTaskIDs, []string{"dep-a", "dep-b"}) {
		t.Fatalf("Descriptor.DependencyTaskIDs = %v, want [dep-a dep-b]", slice.Descriptor.DependencyTaskIDs)
	}
	if rendered := slice.Render(); rendered == "" {
		t.Fatalf("Render() should not be empty")
	}
}

func TestBuildTaskContextSliceBudgetTrimming(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-budget",
		Content:      "这是一个很长很长的任务目标，需要在预算内裁剪但保持关键信息",
		Status:       agentsession.TodoStatusInProgress,
		Priority:     3,
		Dependencies: []string{"dep"},
		Acceptance:   []string{"验收条件一", "验收条件二", "验收条件三"},
	}
	todos := map[string]agentsession.TodoItem{
		"dep": {
			ID:        "dep",
			Content:   "依赖任务",
			Status:    agentsession.TodoStatusCompleted,
			Priority:  1,
			Artifacts: []string{"very/long/path/that/consumes/context/budget.md"},
		},
		"extra": {
			ID:       "extra",
			Content:  "额外任务应在裁剪时优先被移除",
			Status:   agentsession.TodoStatusPending,
			Priority: 1,
		},
	}

	cases := []struct {
		name              string
		maxChars          int
		requireAcceptance bool
	}{
		{name: "tight budget", maxChars: 140, requireAcceptance: true},
		{name: "very tight budget", maxChars: 90, requireAcceptance: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slice := BuildTaskContextSlice(TaskContextSliceInput{
				Task:            task,
				Todos:           todos,
				MaxChars:        tc.maxChars,
				MaxRelatedFiles: 4,
			})

			if !slice.Truncated {
				t.Fatalf("Truncated = false, want true")
			}
			if len([]rune(slice.Render())) > tc.maxChars {
				t.Fatalf("rendered chars exceed budget: got %d, budget %d", len([]rune(slice.Render())), tc.maxChars)
			}
			if slice.Goal == "" {
				t.Fatalf("Goal should remain non-empty after trimming")
			}
			if len(slice.TodoFragment) == 0 || slice.TodoFragment[0].ID != "task-budget" {
				t.Fatalf("first todo fragment should keep current task, got %+v", slice.TodoFragment)
			}
			if tc.requireAcceptance && len(slice.Acceptance) == 0 {
				t.Fatalf("Acceptance should keep at least one item")
			}
		})
	}
}

func TestRebuildTaskContextSliceFromDescriptor(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-rebuild",
		Content:      "重建上下文",
		Status:       agentsession.TodoStatusInProgress,
		Dependencies: []string{"dep-a"},
		Acceptance:   []string{"保留核心上下文"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-rebuild": task,
		"dep-a": {
			ID:        "dep-a",
			Content:   "先完成依赖",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/dep-a.txt"},
		},
	}

	origin := BuildTaskContextSlice(TaskContextSliceInput{
		Task:            task,
		Todos:           todos,
		ActivatedSkills: []string{"skill-a"},
		RelatedFiles: []TaskContextFileSummary{
			{Path: "internal/subagent/context_slice.go", Summary: "上下文切片实现"},
		},
		MaxChars: 4000,
	})

	rebuilt, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor:      origin.Descriptor,
		Todos:           todos,
		ActivatedSkills: []string{"skill-a", "skill-b"},
		RelatedFiles: []TaskContextFileSummary{
			{Path: "internal/subagent/context_slice.go", Summary: "上下文切片实现"},
			{Path: "internal/ignored.go", Summary: "不应被带入"},
		},
		MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("RebuildTaskContextSlice() error = %v", err)
	}
	if rebuilt.TaskID != origin.TaskID {
		t.Fatalf("TaskID = %q, want %q", rebuilt.TaskID, origin.TaskID)
	}
	if !slices.Equal(rebuilt.Descriptor.DependencyTaskIDs, origin.Descriptor.DependencyTaskIDs) {
		t.Fatalf("dependency ids = %v, want %v", rebuilt.Descriptor.DependencyTaskIDs, origin.Descriptor.DependencyTaskIDs)
	}
	if !slices.Equal(rebuilt.Descriptor.ArtifactRefs, origin.Descriptor.ArtifactRefs) {
		t.Fatalf("artifact refs = %v, want %v", rebuilt.Descriptor.ArtifactRefs, origin.Descriptor.ArtifactRefs)
	}
	if !slices.Equal(rebuilt.ActivatedSkills, []string{"skill-a"}) {
		t.Fatalf("ActivatedSkills = %v, want [skill-a]", rebuilt.ActivatedSkills)
	}
	if len(rebuilt.RelatedFiles) == 0 {
		t.Fatalf("RelatedFiles should not be empty")
	}
	if rebuilt.RelatedFiles[0].Path != "internal/subagent/context_slice.go" {
		t.Fatalf("RelatedFiles[0].Path = %q, want internal/subagent/context_slice.go", rebuilt.RelatedFiles[0].Path)
	}
}

func TestRebuildTaskContextSliceFiltersArtifactRefs(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-rebuild-artifacts",
		Content:      "重建产物过滤",
		Status:       agentsession.TodoStatusInProgress,
		Dependencies: []string{"dep-a"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-rebuild-artifacts": task,
		"dep-a": {
			ID:        "dep-a",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/keep.txt", "artifacts/drop.txt"},
		},
	}

	rebuilt, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{
			TaskID:            "task-rebuild-artifacts",
			DependencyTaskIDs: []string{"dep-a"},
			ArtifactRefs:      []string{"artifacts/keep.txt"},
		},
		Todos:    todos,
		MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("RebuildTaskContextSlice() error = %v", err)
	}

	if !slices.Equal(rebuilt.DependencyArtifacts, []string{"artifacts/keep.txt"}) {
		t.Fatalf("DependencyArtifacts = %v, want [artifacts/keep.txt]", rebuilt.DependencyArtifacts)
	}
	if !slices.Equal(rebuilt.Descriptor.ArtifactRefs, []string{"artifacts/keep.txt"}) {
		t.Fatalf("Descriptor.ArtifactRefs = %v, want [artifacts/keep.txt]", rebuilt.Descriptor.ArtifactRefs)
	}
}

func TestRebuildTaskContextSliceEmptyTodoAllowlistMeansNone(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-rebuild-empty-fragments",
		Content:      "空白名单语义",
		Status:       agentsession.TodoStatusInProgress,
		Dependencies: []string{"dep-a"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-rebuild-empty-fragments": task,
		"dep-a": {
			ID:        "dep-a",
			Content:   "依赖",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/dep-a.txt"},
		},
		"extra": {
			ID:      "extra",
			Content: "额外运行中任务",
			Status:  agentsession.TodoStatusInProgress,
		},
	}

	rebuilt, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{
			TaskID:            "task-rebuild-empty-fragments",
			DependencyTaskIDs: []string{"dep-a"},
			TodoFragmentIDs:   []string{},
		},
		Todos:    todos,
		MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("RebuildTaskContextSlice() error = %v", err)
	}

	if len(rebuilt.TodoFragment) != 0 {
		t.Fatalf("TodoFragment len = %d, want 0", len(rebuilt.TodoFragment))
	}
	if len(rebuilt.Descriptor.TodoFragmentIDs) != 0 {
		t.Fatalf("Descriptor.TodoFragmentIDs = %v, want empty", rebuilt.Descriptor.TodoFragmentIDs)
	}
}

func TestRebuildTaskContextSliceEmptyRelatedFileAllowlistMeansNone(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-rebuild-empty-related",
		Content:      "空文件白名单语义",
		Status:       agentsession.TodoStatusInProgress,
		Dependencies: []string{"dep-a"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-rebuild-empty-related": task,
		"dep-a": {
			ID:        "dep-a",
			Content:   "依赖",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/dep-a.txt"},
		},
	}

	rebuilt, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{
			TaskID:            "task-rebuild-empty-related",
			DependencyTaskIDs: []string{"dep-a"},
			RelatedFilePaths:  []string{},
		},
		Todos:    todos,
		MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("RebuildTaskContextSlice() error = %v", err)
	}

	if len(rebuilt.RelatedFiles) != 0 {
		t.Fatalf("RelatedFiles len = %d, want 0", len(rebuilt.RelatedFiles))
	}
	if len(rebuilt.Descriptor.RelatedFilePaths) != 0 {
		t.Fatalf("Descriptor.RelatedFilePaths = %v, want empty", rebuilt.Descriptor.RelatedFilePaths)
	}
}

func TestRebuildTaskContextSliceNilTodoAllowlistKeepsSnapshotFragments(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-rebuild-nil-fragments",
		Content:      "nil 白名单语义",
		Status:       agentsession.TodoStatusInProgress,
		Dependencies: []string{"dep-a"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-rebuild-nil-fragments": task,
		"dep-a": {
			ID:        "dep-a",
			Content:   "依赖",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/dep-a.txt"},
		},
		"extra": {
			ID:      "extra",
			Content: "额外运行中任务",
			Status:  agentsession.TodoStatusInProgress,
		},
	}

	rebuilt, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{
			TaskID:            "task-rebuild-nil-fragments",
			DependencyTaskIDs: []string{"dep-a"},
			TodoFragmentIDs:   nil,
			ArtifactRefs:      nil,
			SkillIDs:          nil,
		},
		Todos:           todos,
		ActivatedSkills: []string{"skill-a"},
		MaxChars:        4000,
	})
	if err != nil {
		t.Fatalf("RebuildTaskContextSlice() error = %v", err)
	}
	if len(rebuilt.TodoFragment) == 0 {
		t.Fatalf("TodoFragment should keep snapshot fragments when allowlist is nil")
	}
	if len(rebuilt.DependencyArtifacts) == 0 {
		t.Fatalf("DependencyArtifacts should keep snapshot artifacts when allowlist is nil")
	}
	if !slices.Equal(rebuilt.ActivatedSkills, []string{"skill-a"}) {
		t.Fatalf("ActivatedSkills = %v, want [skill-a]", rebuilt.ActivatedSkills)
	}
}

func TestBuildTaskContextSliceStableOrder(t *testing.T) {
	t.Parallel()

	now := time.Unix(1711000000, 0).UTC()
	task := agentsession.TodoItem{
		ID:       "order-task",
		Content:  "稳定排序验证",
		Status:   agentsession.TodoStatusInProgress,
		Priority: 2,
	}
	todos := map[string]agentsession.TodoItem{
		"a": {ID: "a", Content: "A", Status: agentsession.TodoStatusPending, Priority: 1, CreatedAt: now.Add(time.Minute)},
		"b": {ID: "b", Content: "B", Status: agentsession.TodoStatusInProgress, Priority: 1, CreatedAt: now.Add(2 * time.Minute)},
		"c": {ID: "c", Content: "C", Status: agentsession.TodoStatusInProgress, Priority: 3, CreatedAt: now.Add(3 * time.Minute)},
	}

	first := BuildTaskContextSlice(TaskContextSliceInput{
		Task:            task,
		Todos:           todos,
		ActivatedSkills: []string{"skill-b", "skill-a", "skill-b"},
		MaxChars:        4000,
	})
	second := BuildTaskContextSlice(TaskContextSliceInput{
		Task:            task,
		Todos:           todos,
		ActivatedSkills: []string{"skill-b", "skill-a", "skill-b"},
		MaxChars:        4000,
	})

	firstIDs := make([]string, 0, len(first.TodoFragment))
	for _, item := range first.TodoFragment {
		firstIDs = append(firstIDs, item.ID)
	}
	secondIDs := make([]string, 0, len(second.TodoFragment))
	for _, item := range second.TodoFragment {
		secondIDs = append(secondIDs, item.ID)
	}
	if !slices.Equal(firstIDs, secondIDs) {
		t.Fatalf("todo order unstable: first=%v second=%v", firstIDs, secondIDs)
	}
	if !slices.Equal(first.ActivatedSkills, []string{"skill-b", "skill-a"}) {
		t.Fatalf("ActivatedSkills = %v, want [skill-b skill-a]", first.ActivatedSkills)
	}
}

func TestRebuildTaskContextSliceErrors(t *testing.T) {
	t.Parallel()

	if _, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{},
		Todos:      map[string]agentsession.TodoItem{},
	}); err == nil {
		t.Fatalf("expected missing task id error")
	}

	if _, err := RebuildTaskContextSlice(TaskContextRebuildInput{
		Descriptor: TaskContextDescriptor{TaskID: "missing"},
		Todos:      map[string]agentsession.TodoItem{},
	}); err == nil {
		t.Fatalf("expected task not found error")
	}
}

func TestNormalizeTaskContextSliceInputReadOnlyTodos(t *testing.T) {
	t.Parallel()

	input := TaskContextSliceInput{
		Task:          agentsession.TodoItem{ID: "task", Content: "goal"},
		ReadOnlyTodos: true,
	}
	out := normalizeTaskContextSliceInput(input)
	if out.Todos == nil {
		t.Fatalf("Todos should be initialized for read-only input")
	}
	if len(out.Todos) != 0 {
		t.Fatalf("Todos len = %d, want 0", len(out.Todos))
	}
}

func TestTodoStatusRankAndTruncateRunesEdgeCases(t *testing.T) {
	t.Parallel()

	if got := todoStatusRank(agentsession.TodoStatusInProgress); got != 0 {
		t.Fatalf("todoStatusRank(in_progress) = %d, want 0", got)
	}
	if got := todoStatusRank(agentsession.TodoStatusBlocked); got != 1 {
		t.Fatalf("todoStatusRank(blocked) = %d, want 1", got)
	}
	if got := todoStatusRank(agentsession.TodoStatusPending); got != 2 {
		t.Fatalf("todoStatusRank(pending) = %d, want 2", got)
	}
	if got := todoStatusRank(agentsession.TodoStatusCompleted); got != 3 {
		t.Fatalf("todoStatusRank(completed) = %d, want 3", got)
	}

	if got := truncateRunes("  ", 3); got != "" {
		t.Fatalf("truncateRunes(blank, 3) = %q, want empty", got)
	}
	if got := truncateRunes("abcdef", 1); got != "…" {
		t.Fatalf("truncateRunes(abcdef, 1) = %q, want …", got)
	}
	if got := truncateRunes("abc", 4); got != "abc" {
		t.Fatalf("truncateRunes(abc, 4) = %q, want abc", got)
	}
	if got := truncateRunes("abcdef", 0); got != "" {
		t.Fatalf("truncateRunes(abcdef, 0) = %q, want empty", got)
	}
}

func TestBuildTaskContextSliceExtremeBudgetKeepsNonEmptyGoal(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:         "task-extreme-budget",
		Content:    "极小预算仍需保留目标语义",
		Status:     agentsession.TodoStatusInProgress,
		Acceptance: []string{"验收条件"},
	}
	slice := BuildTaskContextSlice(TaskContextSliceInput{
		Task:     task,
		Todos:    map[string]agentsession.TodoItem{"task-extreme-budget": task},
		MaxChars: 18,
	})
	if strings.TrimSpace(slice.Goal) == "" {
		t.Fatalf("Goal should remain non-empty under extreme budget")
	}
}

func TestBuildTaskContextSliceTinyBudgetNeverExceedsLimit(t *testing.T) {
	t.Parallel()

	task := agentsession.TodoItem{
		ID:           "task-very-long-id",
		Content:      "这是一个非常长的目标描述，用于触发预算极限裁剪",
		Status:       agentsession.TodoStatusInProgress,
		Acceptance:   []string{"验收条件A"},
		Dependencies: []string{"dep-a"},
	}
	todos := map[string]agentsession.TodoItem{
		"task-very-long-id": task,
		"dep-a": {
			ID:        "dep-a",
			Status:    agentsession.TodoStatusCompleted,
			Artifacts: []string{"artifacts/dep-a.txt"},
		},
	}
	slice := BuildTaskContextSlice(TaskContextSliceInput{
		Task:     task,
		Todos:    todos,
		MaxChars: 6,
	})

	if !slice.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if got := len([]rune(slice.Render())); got > 6 {
		t.Fatalf("rendered chars exceed tiny budget: got %d, budget 6", got)
	}
}

func TestTaskContextSliceRenderSanitizesControlChars(t *testing.T) {
	t.Parallel()

	slice := TaskContextSlice{
		TaskID:              "task-1\nforged",
		Goal:                "goal\r\nline2",
		Acceptance:          []string{"ok\n- forged"},
		DependencyArtifacts: []string{"a\tb"},
		RelatedFiles: []TaskContextFileSummary{
			{Path: "internal/x.go\ninject", Summary: "sum\r\nline"},
		},
		ActivatedSkills: []string{"skill\nbreak"},
		TodoFragment: []TaskTodoFragment{
			{ID: "todo-1\nx", Status: agentsession.TodoStatusInProgress, Content: "content\r\nx"},
		},
	}
	rendered := slice.Render()
	if strings.Contains(rendered, "\n- forged") {
		t.Fatalf("rendered text should sanitize embedded newlines, got %q", rendered)
	}
	if strings.Contains(rendered, "\r") {
		t.Fatalf("rendered text should not contain carriage return, got %q", rendered)
	}
}

func TestContextSliceRuneCountMatchesRenderLength(t *testing.T) {
	t.Parallel()

	slice := TaskContextSlice{
		TaskID:              "task-1",
		Goal:                "goal",
		Acceptance:          []string{"a", "b"},
		DependencyArtifacts: []string{"artifacts/a.txt"},
		RelatedFiles: []TaskContextFileSummary{
			{Path: "internal/subagent/context_slice.go", Summary: "summary"},
		},
		ActivatedSkills: []string{"skill-a"},
		TodoFragment: []TaskTodoFragment{
			{ID: "todo-1", Status: agentsession.TodoStatusInProgress, Content: "do it", Priority: 2},
		},
	}
	got := contextSliceRuneCount(slice)
	want := len([]rune(slice.Render()))
	if got != want {
		t.Fatalf("contextSliceRuneCount() = %d, want %d", got, want)
	}
}
