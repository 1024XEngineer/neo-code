package runtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/tools"
)

func TestProgressStreakStopsRun(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-progress", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-progress",
		Workdir:          t.TempDir(),
	}

	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_error"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			// Always return error to avoid generating progress
			return tools.ToolResult{Content: "error occurred", IsError: true}, nil
		},
	}

	var promptInjected bool
	var signatureSeq int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				seq := atomic.AddInt32(&signatureSeq, 1)
				if strings.Contains(req.SystemPrompt, selfHealingReminder) {
					promptInjected = true
				}
				// the model always decides to call the tool
				events <- providertypes.NewToolCallStartStreamEvent(0, "call_err", "tool_error")
				events <- providertypes.NewToolCallDeltaStreamEvent(
					0,
					"call_err",
					`{"seq":`+strconv.FormatInt(int64(seq), 10)+`}`,
				)
				events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))

	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	input := UserInput{
		RunID: "run-progress",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger error loop")},
	}

	err := service.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from streak limit, got nil")
	}

	if !errors.Is(err, ErrNoProgressStreakLimit) {
		t.Fatalf("expected ErrNoProgressStreakLimit, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())

	// Verify StopReason is error and specifies the streak limit
	assertStopReasonDecided(t, events, controlplane.StopReasonError, ErrNoProgressStreakLimit.Error())

	if !promptInjected {
		t.Error("expected self-healing prompt to be injected before streak limit is reached, but it wasn't")
	}
}

func TestProgressEvidenceResetsNoProgressStreak(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-progress", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-progress",
		Workdir:          t.TempDir(),
	}

	var executeCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_mixed"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			call := int(atomic.AddInt32(&executeCalls, 1))
			if call == 3 {
				return tools.ToolResult{Name: input.Name, Content: "ok", IsError: false}, nil
			}
			return tools.ToolResult{Name: input.Name, Content: "error occurred", IsError: true}, nil
		},
	}

	var providerCalls int32
	var signatureSeq int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				call := int(atomic.AddInt32(&providerCalls, 1))
				if call <= 4 {
					seq := atomic.AddInt32(&signatureSeq, 1)
					events <- providertypes.NewToolCallStartStreamEvent(0, "call_mixed", "tool_mixed")
					events <- providertypes.NewToolCallDeltaStreamEvent(
						0,
						"call_mixed",
						`{"seq":`+strconv.FormatInt(int64(seq), 10)+`}`,
					)
					events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
					return nil
				}
				events <- providertypes.NewTextDeltaStreamEvent("done")
				events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))
	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	err := service.Run(context.Background(), UserInput{
		RunID: "run-progress-reset",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger mixed progress loop")},
	})
	if err != nil {
		t.Fatalf("expected run to finish successfully, got %v", err)
	}

	if executeCalls != 4 {
		t.Fatalf("expected 4 tool executions, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider calls (4 tool turns + 1 done), got %d", providerCalls)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonSuccess, "")
}

func TestRepeatCycleStreakStopsRunAndInjectsReminder(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-repeat", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-repeat",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
			MaxNoProgressStreak:  10,
			MaxRepeatCycleStreak: 3,
		},
	}

	var executeCalls int32
	var providerCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_repeat"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			atomic.AddInt32(&executeCalls, 1)
			return tools.ToolResult{Name: input.Name, Content: "ok", IsError: false}, nil
		},
	}

	var promptInjected bool
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				atomic.AddInt32(&providerCalls, 1)
				if strings.Contains(req.SystemPrompt, selfHealingRepeatReminder) {
					promptInjected = true
				}
				events <- providertypes.NewToolCallStartStreamEvent(0, "call_repeat", "tool_repeat")
				events <- providertypes.NewToolCallDeltaStreamEvent(0, "call_repeat", `{"path":"x"}`)
				events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))
	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	err := service.Run(context.Background(), UserInput{
		RunID: "run-repeat-streak",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger repeat loop")},
	})
	if err == nil {
		t.Fatal("expected repeat cycle limit error, got nil")
	}
	if !errors.Is(err, ErrRepeatCycleLimit) {
		t.Fatalf("expected ErrRepeatCycleLimit, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())

	assertStopReasonDecided(t, events, controlplane.StopReasonError, ErrRepeatCycleLimit.Error())

	if !promptInjected {
		t.Fatal("expected repeat self-healing prompt to be injected before repeat limit is reached")
	}
	if executeCalls != 3 {
		t.Fatalf("expected break on the 3rd identical tool execution, got %d", executeCalls)
	}
	if providerCalls != 3 {
		t.Fatalf("expected 3 provider turns before repeat breaker, got %d", providerCalls)
	}
}

func TestRepeatCycleStreakCountsFailedToolCalls(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-repeat-fail", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-repeat-fail",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
			MaxNoProgressStreak:  10,
			MaxRepeatCycleStreak: 3,
		},
	}

	var executeCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_repeat_fail"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			atomic.AddInt32(&executeCalls, 1)
			return tools.ToolResult{Name: input.Name, Content: "error", IsError: true}, nil
		},
	}

	var providerCalls int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				atomic.AddInt32(&providerCalls, 1)
				events <- providertypes.NewToolCallStartStreamEvent(0, "call_repeat_fail", "tool_repeat_fail")
				events <- providertypes.NewToolCallDeltaStreamEvent(0, "call_repeat_fail", `{"path":"x"}`)
				events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))
	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	err := service.Run(context.Background(), UserInput{
		RunID: "run-repeat-fail-streak",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger repeat fail loop")},
	})
	if !errors.Is(err, ErrRepeatCycleLimit) {
		t.Fatalf("expected ErrRepeatCycleLimit, got %v", err)
	}
	if executeCalls != 3 {
		t.Fatalf("expected failed repeated calls to break on the 3rd execution, got %d", executeCalls)
	}
	if providerCalls != 3 {
		t.Fatalf("expected 3 provider turns before repeat breaker, got %d", providerCalls)
	}
}

func TestComputeToolSignatureNormalizationAndFallback(t *testing.T) {
	if got := computeToolSignature(nil); got != "" {
		t.Fatalf("expected empty signature for nil tool calls, got %q", got)
	}

	callsA := []providertypes.ToolCall{
		{Name: "filesystem_read_file", Arguments: "{\n  \"path\": \"/tmp/a.txt\",\n  \"opts\": {\"y\": [2,3], \"x\": 1}\n}"},
		{Name: "bash", Arguments: "{\"cmd\":\"pwd\"}"},
	}
	callsB := []providertypes.ToolCall{
		{Name: "filesystem_read_file", Arguments: "{\"opts\":{\"x\":1,\"y\":[2,3]},\"path\":\"/tmp/a.txt\"}"},
		{Name: "bash", Arguments: "{ \"cmd\" : \"pwd\" }"},
	}
	sigA := computeToolSignature(callsA)
	sigB := computeToolSignature(callsB)
	if sigA != sigB {
		t.Fatalf("expected canonicalized signatures to match, got %q vs %q", sigA, sigB)
	}

	invalidA := []providertypes.ToolCall{{Name: "bash", Arguments: "{\"cmd\":"}}
	invalidB := []providertypes.ToolCall{{Name: "bash", Arguments: "{\"cmd\":\"ls\"}"}}
	if computeToolSignature(invalidA) == computeToolSignature(invalidB) {
		t.Fatal("expected invalid-json fallback signature to differ from valid-json signature")
	}
}

func TestPrepareTurnSnapshotInjectRepeatReminderWithEmptyPrompt(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Runtime.MaxRepeatCycleStreak = 3
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	service := &Service{
		configManager: manager,
		contextBuilder: &stubContextBuilder{
			buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
				return agentcontext.BuildResult{SystemPrompt: "", Messages: input.Messages}, nil
			},
		},
		toolManager: &stubToolManager{},
	}
	state := newRunState("run-repeat-reminder-empty", newRuntimeSession("session-repeat-reminder-empty"))
	state.progress.LastScore.RepeatCycleStreak = 2

	snapshot, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state)
	if err != nil {
		t.Fatalf("prepareTurnSnapshot() error = %v", err)
	}
	if rebuilt {
		t.Fatal("expected rebuilt=false")
	}
	if snapshot.request.SystemPrompt != selfHealingRepeatReminder {
		t.Fatalf("expected repeat reminder only, got %q", snapshot.request.SystemPrompt)
	}
}

func TestPrepareTurnSnapshotRepeatReminderTakesPriority(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Runtime.MaxNoProgressStreak = 3
		cfg.Runtime.MaxRepeatCycleStreak = 3
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	service := &Service{
		configManager: manager,
		contextBuilder: &stubContextBuilder{
			buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
				return agentcontext.BuildResult{SystemPrompt: "base prompt", Messages: input.Messages}, nil
			},
		},
		toolManager: &stubToolManager{},
	}
	state := newRunState("run-reminder-priority", newRuntimeSession("session-reminder-priority"))
	state.progress.LastScore.NoProgressStreak = 2
	state.progress.LastScore.RepeatCycleStreak = 2

	snapshot, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state)
	if err != nil {
		t.Fatalf("prepareTurnSnapshot() error = %v", err)
	}
	if rebuilt {
		t.Fatal("expected rebuilt=false")
	}
	if !strings.Contains(snapshot.request.SystemPrompt, selfHealingRepeatReminder) {
		t.Fatalf("expected prompt to contain repeat reminder, got %q", snapshot.request.SystemPrompt)
	}
	if strings.Contains(snapshot.request.SystemPrompt, selfHealingReminder) {
		t.Fatalf("expected no-progress reminder to be skipped when repeat reminder is injected, got %q", snapshot.request.SystemPrompt)
	}
}

func TestResolveStreakLimitDefaults(t *testing.T) {
	if got := resolveNoProgressStreakLimit(config.RuntimeConfig{MaxNoProgressStreak: 0}); got != config.DefaultMaxNoProgressStreak {
		t.Fatalf("expected default no-progress limit %d, got %d", config.DefaultMaxNoProgressStreak, got)
	}
	if got := resolveNoProgressStreakLimit(config.RuntimeConfig{MaxNoProgressStreak: 8}); got != 8 {
		t.Fatalf("expected explicit no-progress limit 8, got %d", got)
	}

	if got := resolveRepeatCycleStreakLimit(config.RuntimeConfig{MaxRepeatCycleStreak: -1}); got != config.DefaultMaxRepeatCycleStreak {
		t.Fatalf("expected default repeat limit %d, got %d", config.DefaultMaxRepeatCycleStreak, got)
	}
	if got := resolveRepeatCycleStreakLimit(config.RuntimeConfig{MaxRepeatCycleStreak: 6}); got != 6 {
		t.Fatalf("expected explicit repeat limit 6, got %d", got)
	}
}

func assertStopReasonDecided(t *testing.T, events []RuntimeEvent, wantReason controlplane.StopReason, wantDetail string) {
	t.Helper()
	assertEventContains(t, events, EventStopReasonDecided)
	for _, e := range events {
		if e.Type != EventStopReasonDecided {
			continue
		}
		payload := e.Payload.(StopReasonDecidedPayload)
		if payload.Reason != wantReason {
			t.Fatalf("expected stop reason %s, got %s", wantReason, payload.Reason)
		}
		if wantDetail != "" && payload.Detail != wantDetail {
			t.Fatalf("expected detail to be %q, got %q", wantDetail, payload.Detail)
		}
	}
}
