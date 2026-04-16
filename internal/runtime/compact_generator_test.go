package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func validCompactSummaryJSON() string {
	return `{"task_state":{"goal":"Finish task state refactor","progress":["Persisted task_state in session"],"open_items":["Update runtime tests"],"next_step":"Continue from retained context","blockers":[],"key_artifacts":["internal/runtime/compact_generator.go"],"decisions":["Do not keep old summary-only protocol"],"user_constraints":["No backward compatibility"]},"display_summary":"[compact_summary]\ndone:\n- Persisted durable task state.\n\nin_progress:\n- Continue from the retained recent window.\n\ndecisions:\n- Do not keep the old summary-only protocol.\n\ncode_changes:\n- Updated compact summary generation behavior.\n\nconstraints:\n- Preserve only the minimum information needed to continue the work."}`
}

func TestCompactSummaryGeneratorBuildsProviderRequestWithoutTools(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(validCompactSummaryJSON())},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	generator := newCompactSummaryGenerator(factory, resolvedProvider.ToRuntimeConfig(), "session-model")

	summary, err := generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		CurrentTaskState: agentsession.TaskState{
			Goal:      "Finish task state refactor",
			OpenItems: []string{"Update runtime tests"},
		},
		ArchivedMessages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("legacy request")}},
			{
				Role: providertypes.RoleAssistant,
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
				},
			},
		},
		RetainedMessages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent answer")}},
		},
		ArchivedMessageCount: 2,
		Config:               manager.Get().Context.Compact,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(summary.DisplaySummary, "[compact_summary]") {
		t.Fatalf("expected compact summary marker, got %+v", summary)
	}
	if summary.TaskState.Goal != "Finish task state refactor" {
		t.Fatalf("expected parsed task state, got %+v", summary.TaskState)
	}
	if factory.calls != 1 || scripted.callCount != 1 {
		t.Fatalf("expected one provider call, got factory=%d provider=%d", factory.calls, scripted.callCount)
	}
	if len(factory.configs) != 1 || factory.configs[0].Name != config.OpenAIName {
		t.Fatalf("expected openai provider config, got %+v", factory.configs)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected exactly one recorded request, got %d", len(scripted.requests))
	}
	req := scripted.requests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("expected compact summary request without tools, got %+v", req.Tools)
	}
	if req.Model != "session-model" {
		t.Fatalf("expected request model session-model, got %q", req.Model)
	}
	if !strings.Contains(req.SystemPrompt, "[compact_summary]") {
		t.Fatalf("expected compact system prompt, got %q", req.SystemPrompt)
	}
	if !strings.Contains(req.SystemPrompt, "\"task_state\"") {
		t.Fatalf("expected task state contract in system prompt, got %q", req.SystemPrompt)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != providertypes.RoleUser {
		t.Fatalf("expected a single user prompt, got %+v", req.Messages)
	}
	promptText := renderPartsForTest(req.Messages[0].Parts)
	if !strings.Contains(promptText, "<archived_source_material>") {
		t.Fatalf("expected archived material boundary, got %q", promptText)
	}
	if !strings.Contains(promptText, "<current_task_state>") {
		t.Fatalf("expected task state boundary, got %q", promptText)
	}
	if strings.Contains(promptText, "\"role\": \"user\"") {
		t.Fatalf("expected transcript-style compact prompt instead of pretty JSON, got %q", promptText)
	}
	if !strings.Contains(promptText, "[message 0] role=user") {
		t.Fatalf("expected transcript-style user message header, got %q", promptText)
	}
	if !strings.Contains(promptText, "tool_call id=call-1 name=filesystem_read_file") {
		t.Fatalf("expected tool call metadata in compact prompt, got %q", promptText)
	}
	if !strings.Contains(promptText, `"goal": "Finish task state refactor"`) {
		t.Fatalf("expected current task state JSON in compact prompt, got %q", promptText)
	}
}

func TestCompactSummaryGeneratorRejectsToolCalls(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_read_file"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-1", "{}"),
			},
		},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode: contextcompact.ModeManual,
		ArchivedMessages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("legacy request")}},
		},
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain tool calls") {
		t.Fatalf("expected tool call rejection, got %v", err)
	}
}

func TestCompactSummaryGeneratorRejectsMalformedStreamEvent(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				{Type: providertypes.StreamEventTextDelta},
			},
		},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode:   contextcompact.ModeManual,
		Config: manager.Get().Context.Compact,
	})
	if err == nil || !strings.Contains(err.Error(), "text_delta event payload is nil") {
		t.Fatalf("expected malformed stream event rejection, got %v", err)
	}
}

func TestCompactSummaryGeneratorRejectsCompletionWithoutMessageDone(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("[compact_summary]\npartial"):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	_, err = generator.Generate(context.Background(), contextcompact.SummaryInput{
		Mode:   contextcompact.ModeManual,
		Config: manager.Get().Context.Compact,
	})
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
	if !strings.Contains(err.Error(), "without message_done") {
		t.Fatalf("expected missing message_done error, got %v", err)
	}
}

func TestCompactSummaryGeneratorMalformedStreamEventDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	resolvedProvider, err := resolvedProviderForTests(manager.Get(), config.OpenAIName)
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	stream := []providertypes.StreamEvent{{Type: providertypes.StreamEventTextDelta}}
	for i := 0; i < 40; i++ {
		stream = append(stream, providertypes.NewTextDeltaStreamEvent("ignored"))
	}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{stream},
	}
	generator := newCompactSummaryGenerator(&scriptedProviderFactory{provider: scripted}, resolvedProvider.ToRuntimeConfig(), "session-model")

	errCh := make(chan error, 1)
	go func() {
		_, genErr := generator.Generate(context.Background(), contextcompact.SummaryInput{
			Mode:   contextcompact.ModeManual,
			Config: manager.Get().Context.Compact,
		})
		errCh <- genErr
	}()

	select {
	case genErr := <-errCh:
		if genErr == nil || !strings.Contains(genErr.Error(), "text_delta event payload is nil") {
			t.Fatalf("expected malformed stream event rejection, got %v", genErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected compact generation to fail instead of deadlocking on malformed stream event")
	}
}

func TestParseCompactSummaryOutputToleratesStringInsteadOfArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		json   string
		want   []string
		wantOK bool
	}{
		{
			name:   "正常数组",
			json:   `{"task_state":{"goal":"g","progress":["a","b"],"open_items":[],"next_step":"n","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"summary"}`,
			want:   []string{"a", "b"},
			wantOK: true,
		},
		{
			name:   "字符串代替数组",
			json:   `{"task_state":{"goal":"g","progress":"single item","open_items":[],"next_step":"n","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"summary"}`,
			want:   []string{"single item"},
			wantOK: true,
		},
		{
			name:   "null代替数组",
			json:   `{"task_state":{"goal":"g","progress":null,"open_items":[],"next_step":"n","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"summary"}`,
			want:   nil,
			wantOK: true,
		},
		{
			name:   "数字代替数组报错",
			json:   `{"task_state":{"goal":"g","progress":42,"open_items":[],"next_step":"n","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"summary"}`,
			want:   nil,
			wantOK: false,
		},
		{
			name:   "嵌套对象代替数组报错",
			json:   `{"task_state":{"goal":"g","progress":{"nested":true},"open_items":[],"next_step":"n","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"summary"}`,
			want:   nil,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			output, err := parseCompactSummaryOutput(tt.json)
			if (err == nil) != tt.wantOK {
				t.Fatalf("parseCompactSummaryOutput() error = %v, wantOK %v", err, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if len(output.TaskState.Progress) != len(tt.want) {
				t.Fatalf("progress = %v, want %v", output.TaskState.Progress, tt.want)
			}
			for i := range output.TaskState.Progress {
				if output.TaskState.Progress[i] != tt.want[i] {
					t.Fatalf("progress[%d] = %q, want %q", i, output.TaskState.Progress[i], tt.want[i])
				}
			}
		})
	}
}

func TestCoerceStringArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{
			name: "正常字符串数组",
			raw:  `["a","b","c"]`,
			want: []string{"a", "b", "c"},
		},
		{
			name: "单个字符串",
			raw:  `"single"`,
			want: []string{"single"},
		},
		{
			name: "空字符串返回nil",
			raw:  `""`,
			want: nil,
		},
		{
			name: "null返回nil",
			raw:  `null`,
			want: nil,
		},
		{
			name:    "数字返回nil",
			raw:     `42`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "布尔返回nil",
			raw:     `true`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "嵌套对象返回nil",
			raw:     `{"key":"val"}`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "空RawMessage返回nil",
			raw:  ``,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := coerceStringArray("progress", json.RawMessage(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("coerceStringArray(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("coerceStringArray(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("coerceStringArray(%q)[%d] = %q, want %q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseCompactSummaryOutputSkipsNonCompactJSONPreface(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`preface with braces {"hint":"not compact"}`,
		`{"task_state":{"goal":"g","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"[compact_summary]\nok"}`,
	}, "\n")

	output, err := parseCompactSummaryOutput(content)
	if err != nil {
		t.Fatalf("expected parser to recover valid compact payload, got %v", err)
	}
	if output.TaskState.Goal != "g" {
		t.Fatalf("expected parsed goal, got %+v", output.TaskState)
	}
}

func TestParseCompactSummaryOutputSkipsStrictlyInvalidCandidateAndUsesNext(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		`noise {"task_state":{"goal":"bad","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[],"unexpected":"x"},"display_summary":"[compact_summary]\ninvalid"}`,
		`{"task_state":{"goal":"good","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"[compact_summary]\nok"}`,
	}, "\n")

	output, err := parseCompactSummaryOutput(content)
	if err != nil {
		t.Fatalf("expected parser to skip invalid strict candidate, got %v", err)
	}
	if output.TaskState.Goal != "good" {
		t.Fatalf("expected second valid candidate, got %+v", output.TaskState)
	}
}

func TestParseCompactSummaryOutputRejectsUnknownTopLevelField(t *testing.T) {
	t.Parallel()

	content := `{"task_state":{"goal":"g","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[]},"display_summary":"[compact_summary]\nok","unexpected":"value"}`
	if _, err := parseCompactSummaryOutput(content); err == nil {
		t.Fatal("expected unknown top-level field to be rejected")
	}
}

func TestParseCompactSummaryOutputRejectsUnknownTaskStateField(t *testing.T) {
	t.Parallel()

	content := `{"task_state":{"goal":"g","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[],"extra":"x"},"display_summary":"[compact_summary]\nok"}`
	if _, err := parseCompactSummaryOutput(content); err == nil {
		t.Fatal("expected unknown task_state field to be rejected")
	}
}
