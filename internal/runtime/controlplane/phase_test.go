package controlplane

import "testing"

func TestValidateRunStateTransitionMainlineAndGovernanceStates(t *testing.T) {
	t.Parallel()

	validTransitions := []struct {
		from RunState
		to   RunState
	}{
		{from: "", to: RunStatePlan},
		{from: RunStatePlan, to: RunStateExecute},
		{from: RunStateExecute, to: RunStateVerify},
		{from: RunStateVerify, to: RunStatePlan},
		{from: RunStatePlan, to: RunStateCompacting},
		{from: RunStateCompacting, to: RunStatePlan},
		{from: RunStateExecute, to: RunStateWaitingPermission},
		{from: RunStateWaitingPermission, to: RunStateExecute},
		{from: RunStateVerify, to: RunStateStopped},
	}

	for _, tc := range validTransitions {
		tc := tc
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			t.Parallel()
			if err := ValidateRunStateTransition(tc.from, tc.to); err != nil {
				t.Fatalf("ValidateRunStateTransition(%q,%q) error = %v", tc.from, tc.to, err)
			}
		})
	}
}

func TestValidateRunStateTransitionRejectsInvalidJump(t *testing.T) {
	t.Parallel()

	if err := ValidateRunStateTransition(RunStateCompacting, RunStateExecute); err == nil {
		t.Fatalf("expected invalid transition to return error")
	}
}
