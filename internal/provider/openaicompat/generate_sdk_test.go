package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"

	"neo-code/internal/provider/openaicompat/chatcompletions"
)

type closeTrackingReadCloser struct {
	reader io.Reader
	closed bool
}

func (c *closeTrackingReadCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *closeTrackingReadCloser) Close() error {
	c.closed = true
	return nil
}

func TestResolveChatEndpointPathByMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		mode string
		want string
	}{
		{
			name: "preserves explicit path",
			path: "/gateway/chat/completions",
			mode: "responses",
			want: "/gateway/chat/completions",
		},
		{
			name: "fills chat completions path by default mode",
			path: "",
			mode: "",
			want: "/chat/completions",
		},
		{
			name: "fills responses path for responses mode",
			path: "",
			mode: "responses",
			want: "/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveChatEndpointPathByMode(tt.path, tt.mode); got != tt.want {
				t.Fatalf("resolveChatEndpointPathByMode(%q, %q) = %q, want %q", tt.path, tt.mode, got, tt.want)
			}
		})
	}
}

func TestConvertToSDKMessageMapsToolRoleAndAssistantToolCalls(t *testing.T) {
	t.Parallel()

	toolMessage := convertToSDKMessage(chatcompletions.Message{
		Role:       "tool",
		Content:    "file content",
		ToolCallID: "call_1",
	})
	if toolMessage.OfTool == nil {
		t.Fatal("expected tool message variant")
	}
	if toolMessage.OfTool.ToolCallID != "call_1" {
		t.Fatalf("expected tool_call_id=call_1, got %q", toolMessage.OfTool.ToolCallID)
	}

	assistantMessage := convertToSDKMessage(chatcompletions.Message{
		Role: "assistant",
		ToolCalls: []chatcompletions.ToolCall{
			{
				ID: "call_2",
				Function: chatcompletions.FunctionCall{
					Name:      "filesystem_read_file",
					Arguments: `{"path":"README.md"}`,
				},
			},
		},
	})
	if assistantMessage.OfAssistant == nil {
		t.Fatal("expected assistant message variant")
	}
	if len(assistantMessage.OfAssistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistantMessage.OfAssistant.ToolCalls))
	}
	functionCall := assistantMessage.OfAssistant.ToolCalls[0].GetFunction()
	if functionCall == nil {
		t.Fatal("expected function tool call variant")
	}
	if functionCall.Name != "filesystem_read_file" {
		t.Fatalf("unexpected function name %q", functionCall.Name)
	}
}

func TestConvertToSDKMessageMapsMultipartUserContent(t *testing.T) {
	t.Parallel()

	message := convertToSDKMessage(chatcompletions.Message{
		Role: "user",
		Content: []chatcompletions.MessageContentPart{
			{Type: "text", Text: "look"},
			{Type: "image_url", ImageURL: &chatcompletions.ImageURL{URL: "https://example.com/cat.png"}},
		},
	})
	if message.OfUser == nil {
		t.Fatal("expected user message variant")
	}
	if len(message.OfUser.Content.OfArrayOfContentParts) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(message.OfUser.Content.OfArrayOfContentParts))
	}
}

func TestConvertToChatCompletionParamsEnablesUsageInStream(t *testing.T) {
	t.Parallel()

	params := convertToChatCompletionParams(chatcompletions.Request{
		Model: "gpt-4o-mini",
		Messages: []chatcompletions.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if !params.StreamOptions.IncludeUsage.Valid() || !params.StreamOptions.IncludeUsage.Value {
		t.Fatalf("expected stream_options.include_usage=true, got %+v", params.StreamOptions)
	}
}

func TestShouldFallbackToCompatibleChatStream(t *testing.T) {
	t.Parallel()

	if shouldFallbackToCompatibleChatStream(io.EOF) {
		t.Fatal("did not expect fallback for EOF")
	}
	if !shouldFallbackToCompatibleChatStream(errors.New("SDK stream error: invalid character '[' after top-level value")) {
		t.Fatal("expected fallback for weak SSE decode error")
	}
	if !shouldFallbackToCompatibleChatStream(fmt.Errorf("SDK stream error: %w", &json.SyntaxError{Offset: 1})) {
		t.Fatal("expected fallback for json syntax error")
	}
	if !shouldFallbackToCompatibleChatStream(fmt.Errorf("SDK stream error: %w", io.ErrUnexpectedEOF)) {
		t.Fatal("expected fallback for unexpected EOF")
	}
	if shouldFallbackToCompatibleChatStream(errors.New("context deadline exceeded")) {
		t.Fatal("did not expect fallback for non-decode error")
	}
}

func TestMapOpenAIError(t *testing.T) {
	t.Parallel()

	mapped, ok := mapOpenAIError(&openai.Error{Message: "invalid api key", StatusCode: 401})
	if !ok {
		t.Fatal("expected openai error to be mapped")
	}
	if !strings.Contains(mapped.Error(), "auth_failed") {
		t.Fatalf("expected mapped provider error, got %v", mapped)
	}

	if _, ok := mapOpenAIError(io.EOF); ok {
		t.Fatal("did not expect non-openai error to be mapped")
	}
}

func TestWrapSDKRequestError(t *testing.T) {
	t.Parallel()

	wrapped := wrapSDKRequestError(io.EOF, "send request")
	if !strings.Contains(wrapped.Error(), "send request") {
		t.Fatalf("expected wrapped action in error, got %v", wrapped)
	}

	mapped := wrapSDKRequestError(&openai.Error{Message: "invalid key", StatusCode: 401}, "send request")
	if !strings.Contains(mapped.Error(), "auth_failed") {
		t.Fatalf("expected mapped provider error, got %v", mapped)
	}
}
