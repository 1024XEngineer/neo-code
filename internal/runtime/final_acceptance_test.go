package runtime

import (
	"context"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	agentsession "neo-code/internal/session"
)

func TestBeforeAcceptFinalDecisionPaths(t *testing.T) {
	t.Parallel()

	service := &Service{}
	baseCfg := config.StaticDefaults().Clone()
	baseCfg.Runtime.Verification.Enabled = boolPtr(true)
	baseCfg.Runtime.Verification.FinalIntercept = boolPtr(true)
	snapshot := TurnBudgetSnapshot{
		Config:  baseCfg,
		Workdir: t.TempDir(),
	}

	t.Run("pending required todo -> continue", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-continue", agentsession.New("continue"))
		required := true
		state.session.Todos = []agentsession.TodoItem{
			{
				ID:       "todo-1",
				Content:  "do work",
				Status:   agentsession.TodoStatusPending,
				Required: &required,
			},
		}
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{
			Role:  providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
		}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "continue" {
			t.Fatalf("status = %q, want continue", decision.Status)
		}
	})

	t.Run("all converged -> accepted", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-accepted", agentsession.New("accepted"))
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{
			Role:  providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
		}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "accepted" {
			t.Fatalf("status = %q, want accepted", decision.Status)
		}
	})

	t.Run("verification disabled -> compatibility fallback", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-fallback", agentsession.New("fallback"))
		cfg := snapshot.Config
		cfg.Runtime.Verification.Enabled = boolPtr(false)
		decision, err := service.beforeAcceptFinal(context.Background(), &state, TurnBudgetSnapshot{
			Config:  cfg,
			Workdir: snapshot.Workdir,
		}, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.StopReason != "compatibility_fallback" {
			t.Fatalf("stop_reason = %q, want compatibility_fallback", decision.StopReason)
		}
	})

	t.Run("final intercept disabled -> compatibility fallback", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-no-intercept", agentsession.New("no-intercept"))
		cfg := snapshot.Config
		cfg.Runtime.Verification.FinalIntercept = boolPtr(false)
		decision, err := service.beforeAcceptFinal(context.Background(), &state, TurnBudgetSnapshot{
			Config:  cfg,
			Workdir: snapshot.Workdir,
		}, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.StopReason != "compatibility_fallback" {
			t.Fatalf("stop_reason = %q, want compatibility_fallback", decision.StopReason)
		}
	})

	t.Run("final intercept disabled still respects completion gate", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-no-intercept-completion-gate", agentsession.New("no-intercept-completion-gate"))
		required := true
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "待完成", Status: agentsession.TodoStatusPending, Required: &required},
		}
		cfg := snapshot.Config
		cfg.Runtime.Verification.FinalIntercept = boolPtr(false)
		decision, err := service.beforeAcceptFinal(context.Background(), &state, TurnBudgetSnapshot{
			Config:  cfg,
			Workdir: snapshot.Workdir,
		}, providertypes.Message{}, false)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "continue" {
			t.Fatalf("status = %q, want continue", decision.Status)
		}
	})

	t.Run("last turn continue becomes max-turn incomplete", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-max-turn-incomplete", agentsession.New("max-turn-incomplete"))
		state.turn = 0
		required := true
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "待完成", Status: agentsession.TodoStatusPending, Required: &required},
		}
		cfg := snapshot.Config
		cfg.Runtime.MaxTurns = 1
		decision, err := service.beforeAcceptFinal(context.Background(), &state, TurnBudgetSnapshot{
			Config:  cfg,
			Workdir: snapshot.Workdir,
		}, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "incomplete" {
			t.Fatalf("status = %q, want incomplete", decision.Status)
		}
		if decision.StopReason != "max_turn_exceeded_with_unconverged_todos" {
			t.Fatalf("stop_reason = %q, want max_turn_exceeded_with_unconverged_todos", decision.StopReason)
		}
	})
}

func TestInferTaskTypeSupportsChineseKeywords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		taskID string
		goal   string
		want   string
	}{
		{name: "fix bug chinese", goal: "修复 provider 报错", want: "fix_bug"},
		{name: "refactor chinese", goal: "重构 runtime 结构", want: "refactor"},
		{name: "edit code chinese", goal: "修改代码逻辑", want: "edit_code"},
		{name: "create file chinese", goal: "创建文件并补充脚手架", want: "create_file"},
		{name: "docs chinese", goal: "补充文档说明", want: "docs"},
		{name: "config chinese", goal: "调整配置 yaml", want: "config"},
		{name: "unknown", goal: "整理需求", want: "unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			session := agentsession.New(tc.name)
			session.TaskState.Goal = tc.goal
			state := newRunState("run-"+tc.name, session)
			state.taskID = tc.taskID
			if got := inferTaskType(&state); got != tc.want {
				t.Fatalf("inferTaskType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFinalAcceptanceHelperBranches(t *testing.T) {
	t.Parallel()

	t.Run("beforeAcceptFinal nil state", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		decision, err := service.beforeAcceptFinal(context.Background(), nil, TurnBudgetSnapshot{}, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "" {
			t.Fatalf("expected empty decision for nil state, got %+v", decision)
		}
	})

	t.Run("maxNoProgress fallback to default", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		cfg := config.StaticDefaults().Clone()
		cfg.Runtime.Verification.MaxNoProgress = 0
		state := newRunState("run-max-no-progress-default", agentsession.New("max-no-progress-default"))
		state.finalInterceptStreak = 3
		required := true
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "pending", Status: agentsession.TodoStatusPending, Required: &required},
		}
		decision, err := service.beforeAcceptFinal(context.Background(), &state, TurnBudgetSnapshot{
			Config:  cfg,
			Workdir: t.TempDir(),
		}, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != "incomplete" {
			t.Fatalf("status = %q, want incomplete", decision.Status)
		}
	})

	t.Run("recordAcceptanceTerminal nil state no panic", func(t *testing.T) {
		t.Parallel()
		recordAcceptanceTerminal(nil, acceptance.AcceptanceDecision{})
	})

	t.Run("renderPartsForVerification non-text ignored", func(t *testing.T) {
		t.Parallel()
		got := renderPartsForVerification([]providertypes.ContentPart{
			{Kind: providertypes.ContentPartImage, Text: "ignored"},
			providertypes.NewTextPart(" ok "),
		})
		if got != "ok" {
			t.Fatalf("renderPartsForVerification() = %q, want %q", got, "ok")
		}
	})

	t.Run("inferTaskType nil state", func(t *testing.T) {
		t.Parallel()
		if got := inferTaskType(nil); got != "unknown" {
			t.Fatalf("inferTaskType(nil) = %q, want unknown", got)
		}
	})

	t.Run("applyAcceptanceResultProgress branches", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-progress", agentsession.New("progress"))
		state.finalInterceptStreak = 2

		applyAcceptanceResultProgress(&state, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceContinue, HasProgress: true})
		if state.finalInterceptStreak != 0 {
			t.Fatalf("streak = %d, want 0 after progress", state.finalInterceptStreak)
		}

		applyAcceptanceResultProgress(&state, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceContinue, HasProgress: false})
		if state.finalInterceptStreak != 1 {
			t.Fatalf("streak = %d, want 1 after continue without progress", state.finalInterceptStreak)
		}

		applyAcceptanceResultProgress(&state, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceAccepted})
		if state.finalInterceptStreak != 0 {
			t.Fatalf("streak = %d, want 0 after non-continue", state.finalInterceptStreak)
		}

		applyAcceptanceResultProgress(nil, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceContinue})
	})
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}
