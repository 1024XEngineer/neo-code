package subagent

import "testing"

func TestTaskValidateContextSliceTaskIDMismatch(t *testing.T) {
	t.Parallel()

	err := (Task{
		ID:   "task-a",
		Goal: "goal",
		ContextSlice: TaskContextSlice{
			TaskID: "task-b",
		},
	}).Validate()
	if err == nil {
		t.Fatalf("expected context slice task id mismatch error")
	}
}

func TestTaskValidateAllowsEmptyOrMatchedContextSliceTaskID(t *testing.T) {
	t.Parallel()

	cases := []Task{
		{ID: "task-a", Goal: "goal", ContextSlice: TaskContextSlice{}},
		{ID: "task-a", Goal: "goal", ContextSlice: TaskContextSlice{TaskID: "task-a"}},
		{ID: "task-a", Goal: "goal", ContextSlice: TaskContextSlice{TaskID: " TASK-A "}},
	}
	for _, task := range cases {
		task := task
		t.Run(task.ContextSlice.TaskID, func(t *testing.T) {
			t.Parallel()
			if err := task.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestTaskValidateRequiresIDAndGoal(t *testing.T) {
	t.Parallel()

	if err := (Task{Goal: "goal"}).Validate(); err == nil {
		t.Fatalf("expected task id required error")
	}
	if err := (Task{ID: "task-id"}).Validate(); err == nil {
		t.Fatalf("expected task goal required error")
	}
}

func TestBudgetNormalizeFallbacks(t *testing.T) {
	t.Parallel()

	normalized := (Budget{}).normalize(Budget{})
	if normalized.MaxSteps != 6 {
		t.Fatalf("MaxSteps = %d, want 6", normalized.MaxSteps)
	}
	if normalized.Timeout <= 0 {
		t.Fatalf("Timeout should be > 0")
	}
}
