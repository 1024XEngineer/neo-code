package context

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestProjectToolMessagesForModelSkipsMessagesThatCannotBeProjected(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("user")}},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-1",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("tool output")},
			ToolMetadata: nil,
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-2",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("   ")},
			ToolMetadata: map[string]string{"tool_name": "bash"},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-3",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart(microCompactClearedMessage)},
			ToolMetadata: map[string]string{"tool_name": "bash"},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-4",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("result")},
			ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
		},
	}

	projected := ProjectToolMessagesForModel(cloneContextMessages(messages))
	if renderDisplayParts(projected[0].Parts) != "user" {
		t.Fatalf("non-tool message should remain unchanged, got %+v", projected[0])
	}
	if renderDisplayParts(projected[1].Parts) != "tool output" || projected[1].ToolMetadata != nil {
		t.Fatalf("tool without projection metadata should remain unchanged, got %+v", projected[1])
	}
	if !strings.Contains(renderDisplayParts(projected[2].Parts), "tool result") || projected[2].ToolMetadata != nil {
		t.Fatalf("metadata-only tool message should be projected, got %+v", projected[2])
	}
	if renderDisplayParts(projected[3].Parts) != microCompactClearedMessage || projected[3].ToolMetadata == nil {
		t.Fatalf("cleared tool content should not be projected, got %+v", projected[3])
	}
	if !strings.Contains(renderDisplayParts(projected[4].Parts), "tool result") || projected[4].ToolMetadata != nil {
		t.Fatalf("valid tool message should be projected, got %+v", projected[4])
	}
}

func TestBuildRecentMessagesForModelBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []providertypes.Message
		limit    int
	}{
		{
			name:     "empty messages",
			messages: nil,
			limit:    10,
		},
		{
			name:     "non-positive limit",
			messages: []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("x")}}},
			limit:    0,
		},
		{
			name: "no keepable anchor",
			messages: []providertypes.Message{
				{Role: providertypes.RoleTool, ToolCallID: "orphan", Parts: []providertypes.ContentPart{providertypes.NewTextPart("orphan")}},
			},
			limit: 10,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := BuildRecentMessagesForModel(tt.messages, tt.limit); got != nil {
				t.Fatalf("expected nil, got %+v", got)
			}
		})
	}
}

func TestBuildRecentMessagesForModelKeepsOnlyRecentValidAnchors(t *testing.T) {
	t.Parallel()

	original := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old-user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-1",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("README body")},
			ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
		},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest-user")}},
	}

	recent := BuildRecentMessagesForModel(original, 2)
	if len(recent) != 3 {
		t.Fatalf("len(recent) = %d, want 3", len(recent))
	}
	if recent[0].Role != providertypes.RoleAssistant || len(recent[0].ToolCalls) != 1 {
		t.Fatalf("expected valid tool span to remain, got %+v", recent[0])
	}
	if recent[1].Role != providertypes.RoleTool || !strings.Contains(renderDisplayParts(recent[1].Parts), "tool result") {
		t.Fatalf("expected tool message to be projected, got %+v", recent[1])
	}
	if strings.Contains(renderDisplayParts(recent[1].Parts), "content:\nREADME body") {
		t.Fatalf("expected tool payload to be minimized, got %+v", recent[1])
	}
	if !strings.Contains(renderDisplayParts(recent[1].Parts), "content_excerpt:") {
		t.Fatalf("expected tool payload excerpt marker, got %+v", recent[1])
	}
	if renderDisplayParts(recent[2].Parts) != "latest-user" {
		t.Fatalf("expected latest user anchor to remain, got %+v", recent[2])
	}

	recent[1].Parts = []providertypes.ContentPart{providertypes.NewTextPart("changed")}
	if renderDisplayParts(original[2].Parts) != "README body" {
		t.Fatalf("expected original messages to remain unchanged, got %+v", original[2])
	}
}

func TestBuildRecentMessagesForModelRespectsAbsoluteMessageBudget(t *testing.T) {
	t.Parallel()

	longSpan := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: `{}`},
				{ID: "call-2", Name: "bash", Arguments: `{}`},
				{ID: "call-3", Name: "bash", Arguments: `{}`},
				{ID: "call-4", Name: "bash", Arguments: `{}`},
				{ID: "call-5", Name: "bash", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("one")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("two")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("three")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-4", Parts: []providertypes.ContentPart{providertypes.NewTextPart("four")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-5", Parts: []providertypes.ContentPart{providertypes.NewTextPart("five")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("old-user")}},
	}
	messages = append(messages, longSpan...)
	messages = append(messages, providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest-user")}})

	recent := BuildRecentMessagesForModel(messages, 3)
	if len(recent) != 2 {
		t.Fatalf("len(recent) = %d, want 2", len(recent))
	}
	if renderDisplayParts(recent[0].Parts) != "old-user" || renderDisplayParts(recent[1].Parts) != "latest-user" {
		t.Fatalf("expected oversized tool span to be skipped, got %+v", recent)
	}
}

func TestSanitizeProjectedToolContent(t *testing.T) {
	t.Parallel()

	rawBody := strings.Repeat("甲", recentWindowToolContentCharLimit+10)
	projected := "tool result\nstatus: ok\n\ncontent:\n" + rawBody
	sanitized := sanitizeProjectedToolContent(projected)
	if !strings.Contains(sanitized, "content_excerpt:") {
		t.Fatalf("expected excerpt marker, got %q", sanitized)
	}
	if strings.Contains(sanitized, "content:\n") {
		t.Fatalf("expected original content marker removed, got %q", sanitized)
	}
	if !strings.Contains(sanitized, "[content truncated for memo extraction]") {
		t.Fatalf("expected truncation marker, got %q", sanitized)
	}
}

func TestMatchedToolCallSpanRejectsInvalidAssistantStates(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: " ", Name: "bash", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool output")}},
	}

	if span := matchedToolCallSpan(messages, -1); span != nil {
		t.Fatalf("expected nil span for invalid negative index, got %+v", span)
	}
	if span := matchedToolCallSpan(messages, len(messages)); span != nil {
		t.Fatalf("expected nil span for invalid upper index, got %+v", span)
	}
	if span := matchedToolCallSpan(messages, 0); span != nil {
		t.Fatalf("expected nil span for non-assistant message, got %+v", span)
	}
	if span := matchedToolCallSpan(messages, 1); span != nil {
		t.Fatalf("expected nil span for blank tool call id, got %+v", span)
	}
}

func TestMatchedToolCallSpanRequiresProjectableResponsesAndSkipsDuplicates(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: `{}`},
				{ID: "call-2", Name: "filesystem_read_file", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("first result")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("duplicate result")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "ignored", Parts: []providertypes.ContentPart{providertypes.NewTextPart("ignored result")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("second result")}, ToolMetadata: map[string]string{"tool_name": "filesystem_read_file"}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after")}},
	}

	span := matchedToolCallSpan(messages, 0)
	if len(span) != 3 {
		t.Fatalf("len(span) = %d, want 3 (%+v)", len(span), span)
	}
	if span[0] != 0 || span[1] != 1 || span[2] != 5 {
		t.Fatalf("unexpected span indexes %+v", span)
	}
}

func TestMatchedToolCallSpanAcceptsMetadataOnlyResponses(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "webfetch", Arguments: `{}`},
			},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call-1",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("   ")},
			ToolMetadata: map[string]string{"tool_name": "webfetch", "status_code": "200"},
		},
	}

	span := matchedToolCallSpan(messages, 0)
	if len(span) != 2 || span[0] != 0 || span[1] != 1 {
		t.Fatalf("unexpected metadata-only span %+v", span)
	}
}

func TestMatchedToolCallSpanRejectsResponsesWithoutProjectionMetadata(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("raw result")}},
	}

	if span := matchedToolCallSpan(messages, 0); span != nil {
		t.Fatalf("expected nil span when tool metadata is missing, got %+v", span)
	}
}

func TestIsInjectableToolMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message providertypes.Message
		want    bool
	}{
		{
			name:    "non-tool",
			message: providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("user")}},
			want:    false,
		},
		{
			name:    "empty",
			message: providertypes.Message{Role: providertypes.RoleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("   ")}},
			want:    false,
		},
		{
			name:    "metadata-only",
			message: providertypes.Message{Role: providertypes.RoleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("   ")}, ToolMetadata: map[string]string{"tool_name": "bash"}},
			want:    true,
		},
		{
			name:    "cleared",
			message: providertypes.Message{Role: providertypes.RoleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart(microCompactClearedMessage)}},
			want:    false,
		},
		{
			name:    "valid",
			message: providertypes.Message{Role: providertypes.RoleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ok")}},
			want:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isInjectableToolMessage(tt.message); got != tt.want {
				t.Fatalf("isInjectableToolMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeProjectedToolContentFallsBackForRawPayload(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("x", recentWindowToolContentCharLimit+10)
	sanitized := sanitizeProjectedToolContent(raw)
	if !strings.Contains(sanitized, "content_excerpt:") {
		t.Fatalf("expected raw payload to be excerpted, got %q", sanitized)
	}
	if !strings.Contains(sanitized, "[content truncated for memo extraction]") {
		t.Fatalf("expected truncation marker, got %q", sanitized)
	}
}
