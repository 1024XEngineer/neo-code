package runtime

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	todotool "neo-code/internal/tools/todo"
)

func TestRepeatCycleStreakNoLongerStopsRunAndInjectsReminder(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-repeat", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-repeat",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
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
				call := atomic.AddInt32(&providerCalls, 1)
				if strings.Contains(req.SystemPrompt, selfHealingRepeatReminder) {
					promptInjected = true
				}
				if call >= 5 {
					events <- providertypes.NewTextDeltaStreamEvent("{\"task_completion\":{\"completed\":true}}\ndone")
					events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
					return nil
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
	if err != nil {
		t.Fatalf("expected run to recover after repeat-cycle reminder, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")

	if !promptInjected {
		t.Fatal("expected repeat self-healing prompt injection before recovery")
	}
	if executeCalls != 4 {
		t.Fatalf("expected 4 repeated tool executions before repeat reminder recovery, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns including recovery response, got %d", providerCalls)
	}
}

func TestRepeatCycleTerminatesAfterReminderIfStillStalled(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-repeat-hard-stop", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-repeat-hard-stop",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
			MaxRepeatCycleStreak: 3,
		},
	}

	var executeCalls int32
	var providerCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "tool_repeat"}},
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
	service := NewWithFactory(manager, toolManager, newMemoryStore(), providerFactory, nil)

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-repeat-hard-stop",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger unrecovered repeat loop")},
	}); err != nil {
		t.Fatalf("expected run to stop cleanly on repeat-cycle, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonRepeatCycle, "")

	if !promptInjected {
		t.Fatal("expected repeat self-healing prompt injection before hard repeat-cycle termination")
	}
	if executeCalls != 5 {
		t.Fatalf("expected 5 repeated tool executions before repeat-cycle termination, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns before repeat-cycle termination, got %d", providerCalls)
	}
}

func TestRepeatCycleFailedCallsNoLongerHardStop(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-repeat-fail", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-repeat-fail",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
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
				call := atomic.AddInt32(&providerCalls, 1)
				if call >= 5 {
					events <- providertypes.NewTextDeltaStreamEvent("{\"task_completion\":{\"completed\":true}}\ndone")
					events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
					return nil
				}
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
	if err != nil {
		t.Fatalf("expected run to recover after repeat-cycle reminder, got %v", err)
	}
	if executeCalls != 4 {
		t.Fatalf("expected 4 failed repeated calls before recovery, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns including recovery response, got %d", providerCalls)
	}
	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")
}

func TestRunStopsWhenMaxTurnsReached(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-max-turns", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-max-turns",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
			MaxTurns: 1,
		},
	}

	var toolCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "tool_loop"}},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			_ = ctx
			atomic.AddInt32(&toolCalls, 1)
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		},
	}

	var providerCalls int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				_ = ctx
				_ = req
				atomic.AddInt32(&providerCalls, 1)
				events <- providertypes.NewToolCallStartStreamEvent(0, "call_loop", "tool_loop")
				events <- providertypes.NewToolCallDeltaStreamEvent(0, "call_loop", `{"step":"loop"}`)
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
		RunID: "run-max-turns",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("trigger loop")},
	})
	if err == nil || !strings.Contains(err.Error(), "runtime: max turn limit reached (1)") {
		t.Fatalf("Run() err = %v, want max turn limit reached", err)
	}
	if toolCalls != 1 {
		t.Fatalf("toolCalls = %d, want 1", toolCalls)
	}
	if providerCalls != 1 {
		t.Fatalf("providerCalls = %d, want 1", providerCalls)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonMaxTurnExceeded, "runtime: max turn limit reached (1)")
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
	state.progress.LastScore.StalledProgressState = controlplane.StalledProgressStalled
	state.progress.LastScore.ReminderKind = controlplane.ReminderKindRepeatCycle

	snapshot, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state)
	if err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	}
	if rebuilt {
		t.Fatal("expected rebuilt=false")
	}
	if snapshot.Request.SystemPrompt != selfHealingRepeatReminder {
		t.Fatalf("expected repeat reminder only, got %q", snapshot.Request.SystemPrompt)
	}
}

func TestPrepareTurnBudgetSnapshotRepeatReminderTakesPriority(t *testing.T) {
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
				return agentcontext.BuildResult{SystemPrompt: "base prompt", Messages: input.Messages}, nil
			},
		},
		toolManager: &stubToolManager{},
	}
	state := newRunState("run-reminder-priority", newRuntimeSession("session-reminder-priority"))
	state.progress.LastScore.RepeatCycleStreak = 2
	state.progress.LastScore.StalledProgressState = controlplane.StalledProgressStalled
	state.progress.LastScore.ReminderKind = controlplane.ReminderKindRepeatCycle

	snapshot, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state)
	if err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	}
	if rebuilt {
		t.Fatal("expected rebuilt=false")
	}
	if !strings.Contains(snapshot.Request.SystemPrompt, selfHealingRepeatReminder) {
		t.Fatalf("expected prompt to contain repeat reminder, got %q", snapshot.Request.SystemPrompt)
	}
}

func TestResolveStreakLimitDefaults(t *testing.T) {
	if got := resolveRepeatCycleStreakLimit(config.RuntimeConfig{MaxRepeatCycleStreak: -1}); got != config.DefaultMaxRepeatCycleStreak {
		t.Fatalf("expected default repeat limit %d, got %d", config.DefaultMaxRepeatCycleStreak, got)
	}
	if got := resolveRepeatCycleStreakLimit(config.RuntimeConfig{MaxRepeatCycleStreak: 6}); got != 6 {
		t.Fatalf("expected explicit repeat limit 6, got %d", got)
	}

	if got := resolveRuntimeMaxTurns(config.RuntimeConfig{MaxTurns: 0}); got != config.DefaultMaxTurns {
		t.Fatalf("expected default max turns %d, got %d", config.DefaultMaxTurns, got)
	}
	if got := resolveRuntimeMaxTurns(config.RuntimeConfig{MaxTurns: 30}); got != 30 {
		t.Fatalf("expected explicit max turns 30, got %d", got)
	}
}

func TestNoToolIncompleteTurnStillEvaluatesProgressAndInjectsReminder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := newRuntimeSession("session-no-tool-reminder")
	session.Todos = []agentsession.TodoItem{
		{
			ID:       "todo-1",
			Content:  "close me",
			Status:   agentsession.TodoStatusPending,
			Executor: agentsession.TodoExecutorAgent,
			Revision: 1,
		},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(todotool.New())

	providerImpl := &scriptedProvider{
		requireExplicitCompletion: true,
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\ndone")},
				},
				FinishReason: "stop",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-start",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"set_status","id":"todo-1","status":"in_progress","expected_revision":1}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-complete",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"complete","id":"todo-1","expected_revision":2}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":true}}\ndone")},
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(
		manager,
		registry,
		store,
		&scriptedProviderFactory{provider: providerImpl},
		&stubContextBuilder{},
	)

	if err := service.Run(context.Background(), UserInput{
		RunID:     "run-no-tool-reminder",
		SessionID: session.ID,
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(providerImpl.requests) < 2 {
		t.Fatalf("expected at least 2 provider requests, got %d", len(providerImpl.requests))
	}
	secondSystemPrompt := providerImpl.requests[1].SystemPrompt
	if !strings.Contains(secondSystemPrompt, "[Runtime Control]") ||
		!strings.Contains(secondSystemPrompt, "task_completion") {
		t.Fatalf("expected runtime protocol note in second provider request system prompt, got %q", secondSystemPrompt)
	}
	if len(providerImpl.requests) > 2 {
		thirdSystemPrompt := providerImpl.requests[2].SystemPrompt
		if strings.Contains(thirdSystemPrompt, "[Runtime Control]") &&
			strings.Contains(thirdSystemPrompt, "task_completion") {
			t.Fatalf("expected runtime protocol note to be injected once, got third system prompt %q", thirdSystemPrompt)
		}
	}

	savedSession := store.sessions[session.ID]
	for _, message := range savedSession.Messages {
		content := renderPartsForTest(message.Parts)
		if message.Role == providertypes.RoleSystem &&
			strings.Contains(content, "[Runtime Control]") &&
			strings.Contains(content, "task_completion") {
			t.Fatalf("expected completion reminder to stay out of session transcript, found %q", content)
		}
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventProgressEvaluated)
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")
}

func TestAcceptanceContinueWithoutToolCallStopsAsIncomplete(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := newRuntimeSession("session-no-tool-incomplete")
	required := true
	session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
	session.Todos = []agentsession.TodoItem{
		{
			ID:       "todo-1",
			Content:  "must be completed",
			Status:   agentsession.TodoStatusPending,
			Required: &required,
		},
	}
	store.sessions[session.ID] = cloneSession(session)

	providerImpl := &scriptedProvider{
		requireExplicitCompletion: true,
		responses: []scriptedResponse{
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\n1")}}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\n2")}}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\n3")}}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\n4")}}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("{\"task_completion\":{\"completed\":false}}\n5")}}, FinishReason: "stop"},
			{Message: providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("不应再到这里")}}, FinishReason: "stop"},
		},
	}

	service := NewWithFactory(
		manager,
		&stubToolManager{},
		store,
		&scriptedProviderFactory{provider: providerImpl},
		&stubContextBuilder{},
	)

	if err := service.Run(context.Background(), UserInput{
		RunID:     "run-no-tool-incomplete",
		SessionID: session.ID,
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("继续")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(providerImpl.requests) != 6 {
		t.Fatalf("expected runtime to stop after six missing completion signals, got %d requests", len(providerImpl.requests))
	}
	// 第 6 个请求（streak=5 时注入最终提醒后）应包含最终协议提醒
	fifthSystemPrompt := providerImpl.requests[5].SystemPrompt
	if !strings.Contains(fifthSystemPrompt, "[Runtime Control]") ||
		!strings.Contains(fifthSystemPrompt, "final protocol reminder") ||
		!strings.Contains(fifthSystemPrompt, "task_completion") {
		t.Fatalf("expected final runtime protocol note in request 5 system prompt, got %q", fifthSystemPrompt)
	}
	savedSession := store.sessions[session.ID]
	for _, message := range savedSession.Messages {
		content := renderPartsForTest(message.Parts)
		if message.Role == providertypes.RoleSystem &&
			strings.Contains(content, "[Runtime Control]") &&
			strings.Contains(content, "task_completion") {
			t.Fatalf("expected completion reminder to stay out of session transcript, found %q", content)
		}
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonMissingCompletionSignal, "")
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
