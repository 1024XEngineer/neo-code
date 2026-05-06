package runtime

import (
	"context"
	"errors"
	"path/filepath"
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
					Report:      "completed report " + input.Task.ID,
					Summary:     "completed " + input.Task.ID,
					Status:      "passed",
					Logs:        []string{"ok"},
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

func TestRuntimeSubAgentInvokerRunAppliesInputTimeoutToContext(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, _ subagent.StepInput) (subagent.StepOutput, error) {
			<-ctx.Done()
			return subagent.StepOutput{}, ctx.Err()
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-timeout", "session-timeout", "agent-main", t.TempDir())
	if invoker == nil {
		t.Fatalf("expected non-nil invoker")
	}

	startedAt := time.Now()
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:        subagent.RoleCoder,
		TaskID:      "task-timeout",
		Goal:        "force timeout",
		ExpectedOut: "json summary",
		Timeout:     80 * time.Millisecond,
		MaxSteps:    2,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 400*time.Millisecond {
		t.Fatalf("timeout propagation is too slow, elapsed = %v", elapsed)
	}
}

func TestRuntimeSubAgentInvokerRunKeepsParentDeadlineWhenPresent(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, _ subagent.StepInput) (subagent.StepOutput, error) {
			<-ctx.Done()
			return subagent.StepOutput{}, ctx.Err()
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-parent-deadline", "session-parent-deadline", "agent-main", t.TempDir())
	if invoker == nil {
		t.Fatalf("expected non-nil invoker")
	}

	parentCtx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err := invoker.Run(parentCtx, tools.SubAgentRunInput{
		Role:        subagent.RoleCoder,
		TaskID:      "task-parent-deadline",
		Goal:        "respect parent deadline",
		ExpectedOut: "json summary",
		Timeout:     50 * time.Millisecond,
		MaxSteps:    2,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline exceeded", err)
	}
	elapsed := time.Since(startedAt)
	if elapsed < 120*time.Millisecond {
		t.Fatalf("elapsed = %v, want keep parent deadline (not narrowed by input timeout)", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed = %v, want bounded by parent deadline", elapsed)
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
		AllowedTools:  []string{"filesystem_read_file", "bash"},
		AllowedPaths:  []string{"/workspace"},
		NetworkPolicy: security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
	}
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:                  subagent.RoleCoder,
		TaskType:              subagent.TaskTypeEdit,
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
	if captured.CapabilityToken == nil {
		t.Fatalf("expected parent capability token to be propagated")
	}
	if captured.CapabilityToken.NetworkPolicy.Mode != security.NetworkPermissionDenyAll {
		t.Fatalf("network policy mode = %q, want deny_all", captured.CapabilityToken.NetworkPolicy.Mode)
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
		TaskType:              subagent.TaskTypeEdit,
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
	wantPath := filepath.Clean("/workspace/project/sub")
	if !slices.Equal(captured.AllowedPaths, []string{wantPath}) {
		t.Fatalf("allowed paths = %v, want [%s]", captured.AllowedPaths, wantPath)
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
		TaskType:              subagent.TaskTypeEdit,
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

func TestRuntimeSubAgentInvokerPropagatesTaskType(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	var capturedTaskType subagent.TaskType
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			capturedTaskType = input.Task.TaskType
			return subagent.StepOutput{
				Done: true,
				Output: subagent.Output{
					Report:   "reviewed",
					Findings: []string{"ok"},
				},
			}, nil
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:     subagent.RoleReviewer,
		TaskType: subagent.TaskTypeReview,
		TaskID:   "task-review",
		Goal:     "review this",
		Timeout:  5 * time.Second,
		MaxSteps: 2,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if capturedTaskType != subagent.TaskTypeReview {
		t.Fatalf("task type = %q, want review", capturedTaskType)
	}
}

func TestRuntimeSubAgentInvokerDefaultsEmptyTaskTypeToReview(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	var capturedTaskType subagent.TaskType
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			capturedTaskType = input.Task.TaskType
			return subagent.StepOutput{
				Done: true,
				Output: subagent.Output{
					Report:   "reviewed",
					Findings: []string{"ok"},
				},
			}, nil
		})
	}))

	invoker := newRuntimeSubAgentInvoker(service, "run-inline", "session-inline", "agent-main", t.TempDir())
	_, err := invoker.Run(context.Background(), tools.SubAgentRunInput{
		Role:     subagent.RoleReviewer,
		TaskID:   "task-review-default",
		Goal:     "review this",
		Timeout:  5 * time.Second,
		MaxSteps: 2,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if capturedTaskType != subagent.TaskTypeReview {
		t.Fatalf("task type = %q, want review", capturedTaskType)
	}
}

func TestResolveInlineSubAgentCapabilityWithoutParent(t *testing.T) {
	t.Parallel()

	got, err := resolveInlineSubAgentCapability(nil, []string{" Bash ", "bash", ""}, []string{"/a", "/a", " "}, "", "")
	if err != nil {
		t.Fatalf("resolveInlineSubAgentCapability() error = %v", err)
	}
	if !slices.Equal(got.AllowedTools, []string{"bash"}) {
		t.Fatalf("allowed tools = %v, want [bash]", got.AllowedTools)
	}
	wantPath := filepath.Clean("/a")
	if !slices.Equal(got.AllowedPaths, []string{wantPath}) {
		t.Fatalf("allowed paths = %v, want [%s]", got.AllowedPaths, wantPath)
	}
	if got.CapabilityToken != nil {
		t.Fatalf("expected nil capability token without parent, got %+v", got.CapabilityToken)
	}
}

func TestResolveInlineSubAgentCapabilityResolvesRelativeRequestedPathByWorkdir(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	parent := &security.CapabilityToken{
		AllowedTools: []string{"filesystem_read_file"},
		AllowedPaths: []string{workdir},
	}
	got, err := resolveInlineSubAgentCapability(
		parent,
		[]string{"filesystem_read_file"},
		[]string{"README.md"},
		"",
		workdir,
	)
	if err != nil {
		t.Fatalf("resolveInlineSubAgentCapability() error = %v", err)
	}
	if len(got.AllowedPaths) != 1 {
		t.Fatalf("allowed paths length = %d, want 1", len(got.AllowedPaths))
	}
	wantPath := filepath.Join(workdir, "README.md")
	wantPath, err = filepath.Abs(filepath.Clean(wantPath))
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", wantPath, err)
	}
	if got.AllowedPaths[0] != wantPath {
		t.Fatalf("allowed paths = %v, want [%s]", got.AllowedPaths, wantPath)
	}
}

func TestResolveInlineSubAgentCapabilityCarriesToolUseMode(t *testing.T) {
	t.Parallel()

	got, err := resolveInlineSubAgentCapability(
		nil,
		[]string{"filesystem_read_file"},
		nil,
		subagent.ToolUseModeDisabled,
		"",
	)
	if err != nil {
		t.Fatalf("resolveInlineSubAgentCapability() error = %v", err)
	}
	if got.ToolUseMode != subagent.ToolUseModeDisabled {
		t.Fatalf("tool use mode = %q, want %q", got.ToolUseMode, subagent.ToolUseModeDisabled)
	}

	if _, err := resolveInlineSubAgentCapability(nil, nil, nil, subagent.ToolUseMode("invalid"), ""); err == nil {
		t.Fatal("expected invalid tool use mode error")
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
