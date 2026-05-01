package runtime

import (
	"context"
	"strconv"
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

func TestProgressStreakNoLongerStopsRun(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-progress", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-progress",
		Workdir:          t.TempDir(),
		Runtime: config.RuntimeConfig{
			MaxNoProgressStreak:  3,
			MaxRepeatCycleStreak: 6,
		},
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
	var providerCalls int32
	var signatureSeq int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				call := atomic.AddInt32(&providerCalls, 1)
				seq := atomic.AddInt32(&signatureSeq, 1)
				if strings.Contains(req.SystemPrompt, selfHealingReminder) {
					promptInjected = true
				}
				if call >= 5 {
					events <- providertypes.NewTextDeltaStreamEvent("done")
					events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
					return nil
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

	if err := service.Run(context.Background(), input); err != nil {
		t.Fatalf("expected run success without no-progress hard stop, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")

	if !promptInjected {
		t.Error("expected self-healing prompt to be injected before repetitive no-progress turns")
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns (4 tool cycles + done), got %d", providerCalls)
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
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")
}

func TestRepeatCycleStreakNoLongerStopsRunAndInjectsReminder(t *testing.T) {
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
				call := atomic.AddInt32(&providerCalls, 1)
				if strings.Contains(req.SystemPrompt, selfHealingRepeatReminder) {
					promptInjected = true
				}
				if call >= 5 {
					events <- providertypes.NewTextDeltaStreamEvent("done")
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
		t.Fatalf("expected run success without repeat hard stop, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")

	if !promptInjected {
		t.Fatal("expected repeat self-healing prompt to be injected before repeat limit is reached")
	}
	if executeCalls != 4 {
		t.Fatalf("expected repeated tool executions to continue until model stops, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns (4 tool cycles + done), got %d", providerCalls)
	}
}

func TestRepeatCycleFailedCallsNoLongerHardStop(t *testing.T) {
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
				call := atomic.AddInt32(&providerCalls, 1)
				if call >= 5 {
					events <- providertypes.NewTextDeltaStreamEvent("done")
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
		t.Fatalf("expected run success without repeat hard stop, got %v", err)
	}
	if executeCalls != 4 {
		t.Fatalf("expected failed repeated calls to continue until model stops, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider turns (4 tool cycles + done), got %d", providerCalls)
	}
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
	if strings.Contains(snapshot.Request.SystemPrompt, selfHealingReminder) {
		t.Fatalf("expected no-progress reminder to be skipped when repeat reminder is injected, got %q", snapshot.Request.SystemPrompt)
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

	if got := resolveRuntimeMaxTurns(config.RuntimeConfig{MaxTurns: 0}); got != config.DefaultMaxTurns {
		t.Fatalf("expected default max turns %d, got %d", config.DefaultMaxTurns, got)
	}
	if got := resolveRuntimeMaxTurns(config.RuntimeConfig{MaxTurns: 30}); got != 30 {
		t.Fatalf("expected explicit max turns 30, got %d", got)
	}
}

func TestComputeTodoStateSignature(t *testing.T) {
	t.Parallel()

	if got := computeTodoStateSignature(nil); got != "" {
		t.Fatalf("computeTodoStateSignature(nil) = %q", got)
	}

	base := []agentsession.TodoItem{
		{
			ID:       "t1",
			Content:  "task",
			Status:   agentsession.TodoStatusPending,
			Executor: agentsession.TodoExecutorAgent,
		},
	}
	sig1 := computeTodoStateSignature(base)
	if strings.TrimSpace(sig1) == "" {
		t.Fatal("expected non-empty signature")
	}

	same := []agentsession.TodoItem{
		{
			ID:       "t1",
			Content:  "task",
			Status:   agentsession.TodoStatusPending,
			Executor: agentsession.TodoExecutorAgent,
		},
	}
	sig2 := computeTodoStateSignature(same)
	if sig1 != sig2 {
		t.Fatalf("expected stable signature, got %q vs %q", sig1, sig2)
	}

	changed := []agentsession.TodoItem{
		{
			ID:       "t1",
			Content:  "task",
			Status:   agentsession.TodoStatusCompleted,
			Executor: agentsession.TodoExecutorAgent,
		},
	}
	sig3 := computeTodoStateSignature(changed)
	if sig3 == sig1 {
		t.Fatalf("expected changed signature when todo state changes")
	}
}

func TestNoToolIncompleteTurnStillEvaluatesProgressAndInjectsReminder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Runtime.MaxNoProgressStreak = 1
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
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
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
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
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
	foundReminder := false
	for _, message := range providerImpl.requests[1].Messages {
		content := renderPartsForTest(message.Parts)
		if message.Role == providertypes.RoleSystem &&
			strings.Contains(content, "<acceptance_continue>") &&
			strings.Contains(content, "MUST call todo_write") {
			foundReminder = true
			break
		}
	}
	if !foundReminder {
		t.Fatalf("expected continue reminder in second provider request messages, got %+v", providerImpl.requests[1].Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventProgressEvaluated)
	assertStopReasonDecided(t, events, controlplane.StopReasonAccepted, "")
}

func TestAcceptanceContinueWithoutToolCallStopsAsIncomplete(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Runtime.Verification.MaxNoProgress = 2
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
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("我已经完成了")},
				},
				FinishReason: "stop",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("任务已完成")},
				},
				FinishReason: "stop",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("不应再到这里")},
				},
				FinishReason: "stop",
			},
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

	if len(providerImpl.requests) != 2 {
		t.Fatalf("expected runtime to stop after two no-tool continues, got %d requests", len(providerImpl.requests))
	}
	secondRequestMessages := providerImpl.requests[1].Messages
	foundHint := false
	for _, message := range secondRequestMessages {
		if message.Role != providertypes.RoleSystem {
			continue
		}
		content := renderPartsForTest(message.Parts)
		if strings.Contains(content, "<acceptance_continue>") && strings.Contains(content, "MUST call todo_write") {
			foundHint = true
			break
		}
	}
	if !foundHint {
		t.Fatalf("expected actionable acceptance continue hint, got messages: %+v", secondRequestMessages)
	}

	events := collectRuntimeEvents(service.Events())
	assertStopReasonDecided(t, events, controlplane.StopReasonNoProgressAfterFinalIntercept, "")
	foundVerificationReason := false
	foundAcceptanceReason := false
	for _, event := range events {
		switch event.Type {
		case EventVerificationStarted:
			payload, ok := event.Payload.(VerificationStartedPayload)
			if ok && strings.TrimSpace(payload.CompletionBlockedReason) == string(controlplane.CompletionBlockedReasonPendingTodo) {
				foundVerificationReason = true
			}
		case EventAcceptanceDecided:
			payload, ok := event.Payload.(AcceptanceDecidedPayload)
			if ok && strings.TrimSpace(payload.CompletionBlockedReason) == string(controlplane.CompletionBlockedReasonPendingTodo) {
				foundAcceptanceReason = true
			}
		}
	}
	if !foundVerificationReason {
		t.Fatal("expected verification_started payload to include completion_blocked_reason=pending_todo")
	}
	if !foundAcceptanceReason {
		t.Fatal("expected acceptance_decided payload to include completion_blocked_reason=pending_todo")
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
