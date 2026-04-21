package runtime

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"neo-code/internal/security"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

func newInvokerSuccessSubAgentFactory() subagent.Factory {
	return subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			return subagent.StepOutput{
				Done:  true,
				Delta: "completed",
				Output: subagent.Output{
					Summary:     "completed " + input.Task.ID,
					Findings:    []string{"ok"},
					Patches:     []string{"none"},
					Risks:       []string{"low"},
					NextActions: []string{"continue"},
					Artifacts:   []string{input.Task.ID + ".artifact"},
				},
			}, nil
		})
	})
}

func TestNewRuntimeSubAgentInvokerNilService(t *testing.T) {
	t.Parallel()

	if got := newRuntimeSubAgentInvoker(nil, "run", "session", "agent", ""); got != nil {
		t.Fatalf("expected nil invoker when service is nil")
	}
}

func TestRuntimeSubAgentInvokerRun(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.SetSubAgentFactory(newInvokerSuccessSubAgentFactory())

	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	if invoker == nil {
		t.Fatalf("expected non-nil invoker")
	}

	result, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:        subagent.RoleCoder,
		TaskID:      "task-inline",
		Goal:        "inspect and summarize",
		ExpectedOut: "json summary",
		Timeout:     10 * time.Second,
		MaxSteps:    2,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.TaskID != "task-inline" {
		t.Fatalf("task id = %q, want task-inline", result.TaskID)
	}
	if result.State != subagent.StateSucceeded {
		t.Fatalf("state = %q, want %q", result.State, subagent.StateSucceeded)
	}
}

func TestRuntimeSubAgentInvokerRunInheritsParentCapabilityByDefault(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	var captured subagent.Capability
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			captured = input.Capability
			return subagent.StepOutput{
				Done: true,
				Output: subagent.Output{
					Summary:     "done",
					Findings:    []string{"ok"},
					Patches:     []string{"none"},
					Risks:       []string{"low"},
					NextActions: []string{"continue"},
					Artifacts:   []string{"artifact"},
				},
			}, nil
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	parent := &security.CapabilityToken{
		AllowedTools: []string{"filesystem_read_file", "bash"},
		AllowedPaths: []string{"/workspace"},
	}
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:                  subagent.RoleCoder,
		TaskID:                "task-inline-parent-default",
		Goal:                  "inherit parent capability",
		ExpectedOut:           "json summary",
		Timeout:               10 * time.Second,
		MaxSteps:              2,
		ParentCapabilityToken: parent,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !sameStringSet(captured.AllowedTools, []string{"filesystem_read_file", "bash"}) {
		t.Fatalf("allowed tools = %v, want parent capability set", captured.AllowedTools)
	}
	if !slices.Equal(captured.AllowedPaths, []string{"/workspace"}) {
		t.Fatalf("allowed paths = %v, want parent capability", captured.AllowedPaths)
	}
}

func TestRuntimeSubAgentInvokerRunIntersectsRequestedCapabilityWithParent(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	var captured subagent.Capability
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			captured = input.Capability
			return subagent.StepOutput{
				Done: true,
				Output: subagent.Output{
					Summary:     "done",
					Findings:    []string{"ok"},
					Patches:     []string{"none"},
					Risks:       []string{"low"},
					NextActions: []string{"continue"},
					Artifacts:   []string{"artifact"},
				},
			}, nil
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	parent := &security.CapabilityToken{
		AllowedTools: []string{"filesystem_read_file", "bash"},
		AllowedPaths: []string{"/workspace/project"},
	}
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:                  subagent.RoleCoder,
		TaskID:                "task-inline-parent-intersection",
		Goal:                  "intersection",
		ExpectedOut:           "json summary",
		Timeout:               10 * time.Second,
		MaxSteps:              2,
		AllowedTools:          []string{"bash", "webfetch"},
		AllowedPaths:          []string{"/workspace/project/sub", "/tmp"},
		ParentCapabilityToken: parent,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !slices.Equal(captured.AllowedTools, []string{"bash"}) {
		t.Fatalf("allowed tools = %v, want [bash]", captured.AllowedTools)
	}
	if !slices.Equal(captured.AllowedPaths, []string{"/workspace/project/sub"}) {
		t.Fatalf("allowed paths = %v, want [/workspace/project/sub]", captured.AllowedPaths)
	}
}

func TestRuntimeSubAgentInvokerRunRejectsRequestedCapabilityOutsideParent(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.SetSubAgentFactory(newInvokerSuccessSubAgentFactory())
	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	parent := &security.CapabilityToken{
		AllowedTools: []string{"filesystem_read_file"},
		AllowedPaths: []string{"/workspace/project"},
	}
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:                  subagent.RoleCoder,
		TaskID:                "task-inline-parent-reject",
		Goal:                  "reject escalation",
		ExpectedOut:           "json summary",
		Timeout:               10 * time.Second,
		MaxSteps:              2,
		AllowedTools:          []string{"bash"},
		AllowedPaths:          []string{"/tmp"},
		ParentCapabilityToken: parent,
	})
	if err == nil {
		t.Fatalf("expected capability tightening error")
	}
	if !strings.Contains(err.Error(), "requested tools exceed parent") &&
		!strings.Contains(err.Error(), "requested paths exceed parent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveInlineSubAgentCapabilityWithoutParent(t *testing.T) {
	t.Parallel()

	got, err := resolveInlineSubAgentCapability(nil, []string{" Bash ", "bash", ""}, []string{"/a", "/a", " "})
	if err != nil {
		t.Fatalf("resolveInlineSubAgentCapability() error = %v", err)
	}
	if !slices.Equal(got.AllowedTools, []string{"bash"}) {
		t.Fatalf("allowed tools = %v, want [bash]", got.AllowedTools)
	}
	if !slices.Equal(got.AllowedPaths, []string{"/a"}) {
		t.Fatalf("allowed paths = %v, want [/a]", got.AllowedPaths)
	}
}

func TestPathCoveredByAllowlist(t *testing.T) {
	t.Parallel()

	if !pathCoveredByAllowlist("/workspace/project/sub", []string{"/workspace/project"}) {
		t.Fatalf("expected nested path to be covered")
	}
	if pathCoveredByAllowlist("/workspace/other", []string{"/workspace/project"}) {
		t.Fatalf("expected unrelated path to be rejected")
	}
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[string]int, len(left))
	for _, item := range left {
		set[item]++
	}
	for _, item := range right {
		set[item]--
		if set[item] < 0 {
			return false
		}
	}
	for _, count := range set {
		if count != 0 {
			return false
		}
	}
	return true
}
