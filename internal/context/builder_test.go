package context

import (
	stdcontext "context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/context/internalcompact"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const maxRetainedMessageSpans = config.DefaultCompactReadTimeMaxMessageSpans

type stubPromptSectionSource struct {
	sections []promptSection
	err      error
}

func (s stubPromptSectionSource) Sections(ctx stdcontext.Context, input BuildInput) ([]promptSection, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]promptSection(nil), s.sections...), nil
}

func TestDefaultBuilderBuild(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []providertypes.Message{
			{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
		},
		Metadata: testMetadata(t.TempDir()),
	}

	got, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got.SystemPrompt == "" {
		t.Fatalf("expected non-empty system prompt")
	}
	if !strings.Contains(got.SystemPrompt, "## Agent Identity") {
		t.Fatalf("expected core prompt sections to be included")
	}
	if !strings.Contains(got.SystemPrompt, "## System State") {
		t.Fatalf("expected system state section in composed prompt")
	}
	if strings.Contains(got.SystemPrompt, "## Project Rules") {
		t.Fatalf("did not expect project rules section without AGENTS.md")
	}
	if strings.Contains(got.SystemPrompt, "\n\n\n") {
		t.Fatalf("did not expect repeated blank lines in composed prompt")
	}
	if !strings.Contains(got.SystemPrompt, input.Metadata.Workdir) {
		t.Fatalf("expected workdir in system state section")
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}
	if &got.Messages[0] == &input.Messages[0] {
		t.Fatalf("expected messages slice to be cloned")
	}
}

func TestDefaultBuilderBuildHonorsCancellation(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()

	_, err := builder.Build(ctx, BuildInput{})
	if err != stdcontext.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDefaultBuilderBuildComposesPromptSectionsInOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, projectRuleFileName), []byte("project-rules"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	builder := NewBuilder()
	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(root),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	identityIndex := strings.Index(got.SystemPrompt, "## Agent Identity")
	rulesIndex := strings.Index(got.SystemPrompt, "## Project Rules")
	stateIndex := strings.Index(got.SystemPrompt, "## System State")
	if identityIndex < 0 || rulesIndex < 0 || stateIndex < 0 {
		t.Fatalf("expected all prompt sections, got %q", got.SystemPrompt)
	}
	if !(identityIndex < rulesIndex && rulesIndex < stateIndex) {
		t.Fatalf("expected section order core -> project rules -> system state, got %q", got.SystemPrompt)
	}
}

func TestDefaultBuilderBuildIncludesTaskStateBeforeSystemState(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		TaskState: agentsession.TaskState{
			Goal:      "Finish task state refactor",
			OpenItems: []string{"Update tests"},
			NextStep:  "Run go test ./...",
		},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	taskStateIndex := strings.Index(got.SystemPrompt, "## Task State")
	systemStateIndex := strings.Index(got.SystemPrompt, "## System State")
	if taskStateIndex < 0 || systemStateIndex < 0 {
		t.Fatalf("expected task state and system state sections, got %q", got.SystemPrompt)
	}
	if taskStateIndex > systemStateIndex {
		t.Fatalf("expected task state before system state, got %q", got.SystemPrompt)
	}
	if !strings.Contains(got.SystemPrompt, "- goal: Finish task state refactor") {
		t.Fatalf("expected task state content in system prompt, got %q", got.SystemPrompt)
	}
}

func TestDefaultBuilderBuildIncludesTodosBeforeSystemState(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Todos: []agentsession.TodoItem{
			{
				ID:        "todo-1",
				Content:   "implement todo tool",
				Status:    agentsession.TodoStatusInProgress,
				Priority:  3,
				Revision:  2,
				CreatedAt: time.Now(),
			},
		},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	todoIndex := strings.Index(got.SystemPrompt, "## Todo State")
	systemStateIndex := strings.Index(got.SystemPrompt, "## System State")
	if todoIndex < 0 || systemStateIndex < 0 {
		t.Fatalf("expected todo and system sections, got %q", got.SystemPrompt)
	}
	if todoIndex > systemStateIndex {
		t.Fatalf("expected todo section before system section, got %q", got.SystemPrompt)
	}
}

func TestDefaultBuilderBuildUsesSpanTrimPolicyWhenTrimPolicyIsUnset(t *testing.T) {
	t.Parallel()

	messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+2)
	for i := 0; i < maxRetainedMessageSpans+2; i++ {
		messages = append(messages, providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("u-%d", i))},
		})
	}

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages:  messages,
		TaskState: agentsession.TaskState{Goal: "keep implementing task"},
		Compact: CompactOptions{
			MicroCompactRetainedToolSpans: 2,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Messages) != maxRetainedMessageSpans {
		t.Fatalf("expected %d retained messages, got %d", maxRetainedMessageSpans, len(got.Messages))
	}
	if renderDisplayParts(got.Messages[0].Parts) != "u-2" {
		t.Fatalf("expected oldest messages to be trimmed, got first message %+v", got.Messages[0])
	}
}

func TestDefaultBuilderBuildReturnsPromptSourceError(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{err: fmt.Errorf("source failed")},
		},
	}

	_, err := builder.Build(stdcontext.Background(), BuildInput{})
	if err == nil || !strings.Contains(err.Error(), "source failed") {
		t.Fatalf("expected source error, got %v", err)
	}
}

func TestDefaultBuilderBuildAppliesMicroCompactAfterTrim(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages:  messages,
		TaskState: agentsession.TaskState{Goal: "keep implementing task"},
		Compact: CompactOptions{
			MicroCompactRetainedToolSpans: 2,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Messages) != len(messages) {
		t.Fatalf("expected builder output to keep message count, got %d want %d", len(got.Messages), len(messages))
	}
	if renderDisplayParts(got.Messages[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected builder output to clear older tool result, got %q", renderDisplayParts(got.Messages[2].Parts))
	}
	if renderDisplayParts(got.Messages[4].Parts) != "recent bash result" {
		t.Fatalf("expected recent tool result to stay visible, got %q", renderDisplayParts(got.Messages[4].Parts))
	}
	if renderDisplayParts(got.Messages[6].Parts) != "latest webfetch result" {
		t.Fatalf("expected latest tool result to stay visible, got %q", renderDisplayParts(got.Messages[6].Parts))
	}
}

func TestDefaultBuilderBuildDefaultsPinCheckerForLiteralBuilder(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_write_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{
			Role:       providertypes.RoleTool,
			ToolCallID: "call-1",
			Parts:      []providertypes.ContentPart{providertypes.NewTextPart("README content")},
			ToolMetadata: map[string]string{
				"path": "/project/README.md",
			},
		},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages:  messages,
		TaskState: agentsession.TaskState{Goal: "keep implementing task"},
		Compact: CompactOptions{
			MicroCompactRetainedToolSpans: 1,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	projectedText := renderDisplayParts(got.Messages[2].Parts)
	if projectedText == microCompactClearedMessage {
		t.Fatalf("expected pinned README result to avoid cleared placeholder, got %q", projectedText)
	}
	if !strings.Contains(projectedText, "README content") {
		t.Fatalf("expected pinned README result to retain content, got %q", projectedText)
	}
}

func TestDefaultBuilderBuildRespectsExplicitPinCheckerOverride(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
		microCompactPinChecker: noopPinChecker{},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_write_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{
			Role:       providertypes.RoleTool,
			ToolCallID: "call-1",
			Parts:      []providertypes.ContentPart{providertypes.NewTextPart("README content")},
			ToolMetadata: map[string]string{
				"path": "/project/README.md",
			},
		},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages:  messages,
		TaskState: agentsession.TaskState{Goal: "keep implementing task"},
		Compact: CompactOptions{
			MicroCompactRetainedToolSpans: 1,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if renderDisplayParts(got.Messages[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected explicit noop pin checker to allow compaction, got %q", renderDisplayParts(got.Messages[2].Parts))
	}
}

type noopPinChecker struct{}

func (noopPinChecker) ShouldPin(string, map[string]string) bool { return false }

func TestNewBuilderWithToolPoliciesAndSummarizers(t *testing.T) {
	t.Parallel()

	builder := NewBuilderWithToolPoliciesAndSummarizers(
		nil,
		stubMicroCompactSummarizerSource{
			"filesystem_read_file": func(content string, metadata map[string]string, isError bool) string {
				return "[summary] read_file"
			},
		},
	)

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages:  messages,
		TaskState: agentsession.TaskState{Goal: "keep implementing task"},
		Compact: CompactOptions{
			MicroCompactRetainedToolSpans: 2,
		},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	const summarizedMessageIndex = 2
	if renderDisplayParts(got.Messages[summarizedMessageIndex].Parts) != "[summary] read_file" {
		t.Fatalf(
			"expected summarized older read result, got %q",
			renderDisplayParts(got.Messages[summarizedMessageIndex].Parts),
		)
	}
}

func TestDefaultBuilderBuildSkipsMicroCompactWithoutEstablishedTaskState(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{Messages: messages})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if renderDisplayParts(got.Messages[2].Parts) != "old read result" {
		t.Fatalf("expected old tool result to remain visible without task state, got %q", renderDisplayParts(got.Messages[2].Parts))
	}
}

func TestDefaultBuilderBuildSkipsMicroCompactWhenDisabled(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: messages,
		Compact: CompactOptions{
			DisableMicroCompact: true,
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !reflect.DeepEqual(got.Messages, messages) {
		t.Fatalf("expected messages to remain unchanged when micro compact is disabled, got %+v", got.Messages)
	}
	if &got.Messages[2] == &messages[2] {
		t.Fatalf("expected disabled path to still clone message slice")
	}
}

func TestDefaultBuilderBuildHonorsToolMicroCompactPolicies(t *testing.T) {
	t.Parallel()

	builder := &DefaultBuilder{
		promptSources: []promptSectionSource{
			stubPromptSectionSource{sections: []promptSection{{Title: "Stub", Content: "body"}}},
		},
		microCompactPolicies: stubMicroCompactPolicySource{
			"custom_tool": tools.MicroCompactPolicyPreserveHistory,
		},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old custom result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{Messages: messages})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if renderDisplayParts(got.Messages[2].Parts) != "old custom result" {
		t.Fatalf("expected preserved tool result to remain, got %q", renderDisplayParts(got.Messages[2].Parts))
	}
}

func TestNewBuilderWithToolPoliciesUsesProvidedPolicySource(t *testing.T) {
	t.Parallel()

	builder := NewBuilderWithToolPolicies(stubMicroCompactPolicySource{
		"custom_tool": tools.MicroCompactPolicyPreserveHistory,
	})

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old custom result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	got, err := builder.Build(stdcontext.Background(), BuildInput{Messages: messages})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if renderDisplayParts(got.Messages[2].Parts) != "old custom result" {
		t.Fatalf("expected preserved tool result to remain, got %q", renderDisplayParts(got.Messages[2].Parts))
	}
}

func TestTrimMessagesPreservesToolPairs(t *testing.T) {
	t.Parallel()

	messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+4)
	for i := 0; i < 8; i++ {
		messages = append(messages, providertypes.Message{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("u-%d", i))}})
	}
	messages = append(messages,
		providertypes.Message{
			Role: "assistant",
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		providertypes.Message{Role: "tool", ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-result")}},
		providertypes.Message{Role: "assistant", Parts: []providertypes.ContentPart{providertypes.NewTextPart("after-tool")}},
		providertypes.Message{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest")}},
	)

	trimmed := trimMessages(messages, maxRetainedMessageSpans)
	if len(trimmed) > len(messages) {
		t.Fatalf("trimmed messages should not grow")
	}

	foundAssistantToolCall := false
	foundToolResult := false
	for _, message := range trimmed {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			foundAssistantToolCall = true
		}
		if message.Role == "tool" && message.ToolCallID == "call-1" {
			foundToolResult = true
		}
	}
	if foundAssistantToolCall != foundToolResult {
		t.Fatalf("expected tool call and tool result to be preserved together, got %+v", trimmed)
	}
}

func TestTrimMessagesProtectsLatestExplicitUserInstructionTail(t *testing.T) {
	t.Parallel()

	const retainedSpans = 10

	messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+5)
	for i := 0; i < 2; i++ {
		messages = append(messages, providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("old-%d", i))}})
	}
	messages = append(messages,
		providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-1")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-2")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-3")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-4")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-5")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-6")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-7")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-8")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-9")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-10")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("follow-up-11")}},
	)

	trimmed := trimMessages(messages, retainedSpans)
	if trimmed[0].Role != providertypes.RoleUser || renderDisplayParts(trimmed[0].Parts) != "latest explicit instruction" {
		t.Fatalf("expected protected tail to keep latest explicit user instruction, got %+v", trimmed[0])
	}
	if len(trimmed) != 12 {
		t.Fatalf("expected protected tail to keep latest instruction and full assistant tail, got %d messages", len(trimmed))
	}
}

func TestTrimMessagesUsesSharedSpanModel(t *testing.T) {
	t.Parallel()

	const retainedSpans = 10

	messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+6)
	for i := 0; i < 3; i++ {
		messages = append(messages, providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("u-%d", i))}})
	}
	messages = append(messages,
		providertypes.Message{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		providertypes.Message{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-result")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after tool")}},
		providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("u-4")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("a-5")}},
		providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("u-6")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("a-7")}},
		providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("u-8")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("a-9")}},
		providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("u-10")}},
		providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("a-11")}},
	)

	spans := internalcompact.BuildMessageSpans(messages)
	trimmed := trimMessages(messages, retainedSpans)

	start := spans[len(spans)-retainedSpans].Start
	if len(trimmed) == 0 || renderDisplayParts(trimmed[0].Parts) != renderDisplayParts(messages[start].Parts) {
		t.Fatalf("expected trim to start from shared span boundary %d, got %+v", start, trimmed)
	}
	if trimmed[0].Role != providertypes.RoleAssistant || len(trimmed[0].ToolCalls) != 1 {
		t.Fatalf("expected trim to keep whole tool block at shared boundary, got %+v", trimmed[0])
	}
}

func TestTrimMessagesBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []providertypes.Message
		wantLen int
		assert  func(t *testing.T, original []providertypes.Message, trimmed []providertypes.Message)
	}{
		{
			name: "within max turns returns full cloned slice",
			input: []providertypes.Message{
				{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("one")}},
				{Role: "assistant", Parts: []providertypes.ContentPart{providertypes.NewTextPart("two")}},
			},
			wantLen: 2,
			assert: func(t *testing.T, original []providertypes.Message, trimmed []providertypes.Message) {
				t.Helper()
				if &trimmed[0] == &original[0] {
					t.Fatalf("expected trimmed slice to be cloned")
				}
			},
		},
		{
			name: "long message list with limited spans keeps full history",
			input: func() []providertypes.Message {
				messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+3)
				for i := 0; i < maxRetainedMessageSpans-1; i++ {
					messages = append(messages, providertypes.Message{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("u-%d", i))}})
				}
				messages = append(messages,
					providertypes.Message{
						Role: "assistant",
						ToolCalls: []providertypes.ToolCall{
							{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
						},
					},
					providertypes.Message{Role: "tool", ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-1")}},
					providertypes.Message{Role: "tool", ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-2")}},
				)
				return messages
			}(),
			wantLen: maxRetainedMessageSpans + 2,
			assert: func(t *testing.T, original []providertypes.Message, trimmed []providertypes.Message) {
				t.Helper()
				if len(trimmed) != len(original) {
					t.Fatalf("expected full history to remain, got %d want %d", len(trimmed), len(original))
				}
			},
		},
		{
			name: "message count beyond limit trims by span count",
			input: func() []providertypes.Message {
				messages := make([]providertypes.Message, 0, maxRetainedMessageSpans+5)
				for i := 0; i < maxRetainedMessageSpans+1; i++ {
					messages = append(messages, providertypes.Message{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("u-%d", i))}})
				}
				messages = append(messages,
					providertypes.Message{
						Role: "assistant",
						ToolCalls: []providertypes.ToolCall{
							{ID: "call-2", Name: "filesystem_edit", Arguments: "{}"},
						},
					},
					providertypes.Message{Role: "tool", ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-result")}},
				)
				return messages
			}(),
			wantLen: maxRetainedMessageSpans + 1,
			assert: func(t *testing.T, original []providertypes.Message, trimmed []providertypes.Message) {
				t.Helper()
				if renderDisplayParts(trimmed[0].Parts) != "u-2" {
					t.Fatalf("expected oldest spans to be removed, got first message %+v", trimmed[0])
				}
				if trimmed[len(trimmed)-1].Role != "tool" {
					t.Fatalf("expected trailing tool result to remain, got %+v", trimmed[len(trimmed)-1])
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			trimmed := trimMessages(tt.input, maxRetainedMessageSpans)
			if len(trimmed) != tt.wantLen {
				t.Fatalf("expected len %d, got %d", tt.wantLen, len(trimmed))
			}
			if tt.assert != nil {
				tt.assert(t, tt.input, trimmed)
			}
		})
	}
}

func TestBuildAutoCompactSuggestedDisabled(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(t.TempDir()),
		Compact:  CompactOptions{AutoCompactThreshold: 0},
	}
	input.Metadata.SessionInputTokens = 100

	result, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.AutoCompactSuggested {
		t.Fatalf("expected AutoCompactSuggested false when threshold is 0")
	}
}

func TestBuildAutoCompactSuggestedBelowThreshold(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(t.TempDir()),
		Compact:  CompactOptions{AutoCompactThreshold: 100},
	}
	input.Metadata.SessionInputTokens = 99

	result, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.AutoCompactSuggested {
		t.Fatalf("expected AutoCompactSuggested false when tokens below threshold")
	}
}

func TestBuildAutoCompactSuggestedAtThreshold(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(t.TempDir()),
		Compact:  CompactOptions{AutoCompactThreshold: 100},
	}
	input.Metadata.SessionInputTokens = 100

	result, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !result.AutoCompactSuggested {
		t.Fatalf("expected AutoCompactSuggested true when tokens equal threshold")
	}
}

func TestBuildAutoCompactSuggestedAboveThreshold(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		Metadata: testMetadata(t.TempDir()),
		Compact:  CompactOptions{AutoCompactThreshold: 100},
	}
	input.Metadata.SessionInputTokens = 200

	result, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !result.AutoCompactSuggested {
		t.Fatalf("expected AutoCompactSuggested true when tokens above threshold")
	}
}

func TestNewBuilderWithMemo(t *testing.T) {
	t.Parallel()

	t.Run("with memo source injects memo section", func(t *testing.T) {
		memoSource := stubPromptSectionSource{
			sections: []promptSection{{Title: "Memo", Content: "- [user] test entry"}},
		}
		builder := NewBuilderWithMemo(stubMicroCompactPolicySource{}, memoSource)
		input := BuildInput{
			Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
			Metadata: testMetadata(t.TempDir()),
		}
		result, err := builder.Build(stdcontext.Background(), input)
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		if !strings.Contains(result.SystemPrompt, "## Memo") {
			t.Errorf("expected Memo section in system prompt")
		}
		if !strings.Contains(result.SystemPrompt, "test entry") {
			t.Errorf("expected memo content in system prompt")
		}
	})

	t.Run("nil memo source skips memo section", func(t *testing.T) {
		builder := NewBuilderWithMemo(stubMicroCompactPolicySource{}, nil)
		input := BuildInput{
			Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
			Metadata: testMetadata(t.TempDir()),
		}
		result, err := builder.Build(stdcontext.Background(), input)
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		if strings.Contains(result.SystemPrompt, "## Memo") {
			t.Error("nil memo source should not inject Memo section")
		}
	})
}

func TestProjectToolMessagesForModelKeepsBuilderProjectionBehavior(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-1",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("tool output")},
			ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
		},
	}

	projected := ProjectToolMessagesForModel(cloneContextMessages(messages))
	if len(projected) != 1 {
		t.Fatalf("len(projected) = %d, want 1", len(projected))
	}
	projectedText := renderDisplayParts(projected[0].Parts)
	if !strings.Contains(projectedText, "tool result") || !strings.Contains(projectedText, "tool: filesystem_read_file") {
		t.Fatalf("unexpected projected content: %q", projectedText)
	}
	if projected[0].ToolMetadata != nil {
		t.Fatalf("expected projected metadata to be cleared, got %#v", projected[0].ToolMetadata)
	}
	if messages[0].ToolMetadata == nil {
		t.Fatal("expected source messages to remain unchanged")
	}
}

func TestDefaultBuilderBuildProjectsMetadataOnlyToolResult(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	result, err := builder.Build(stdcontext.Background(), BuildInput{
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("inspect README")}},
			{
				Role: providertypes.RoleAssistant,
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
				},
			},
			{
				Role:         providertypes.RoleTool,
				ToolCallID:   "call-1",
				Parts:        []providertypes.ContentPart{providertypes.NewTextPart("   ")},
				ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
			},
		},
		Metadata: testMetadata(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("len(result.Messages) = %d, want 3", len(result.Messages))
	}
	toolMessage := result.Messages[2]
	if toolMessage.Role != providertypes.RoleTool {
		t.Fatalf("expected tool message at index 2, got %+v", toolMessage)
	}
	toolMessageText := renderDisplayParts(toolMessage.Parts)
	if !strings.Contains(toolMessageText, "tool result") ||
		!strings.Contains(toolMessageText, "tool: filesystem_read_file") ||
		!strings.Contains(toolMessageText, "meta.path: README.md") {
		t.Fatalf("expected metadata-only tool result to be projected, got %q", toolMessageText)
	}
	if toolMessage.ToolMetadata != nil {
		t.Fatalf("expected projected tool metadata to be cleared, got %#v", toolMessage.ToolMetadata)
	}
}
