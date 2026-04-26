package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
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
		{
			name: "fills chat completions path for explicit completions mode",
			path: "",
			mode: "chat_completions",
			want: "/chat/completions",
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

func TestResolveChatEndpointUsesExplicitModeFallbackAndCustomPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  provider.RuntimeConfig
		want string
	}{
		{
			name: "explicit responses mode falls back to responses path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatAPIMode:      provider.ChatAPIModeResponses,
				ChatEndpointPath: "",
			},
			want: "https://api.example.com/v1/responses",
		},
		{
			name: "explicit completions mode falls back to completions path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatAPIMode:      provider.ChatAPIModeChatCompletions,
				ChatEndpointPath: "",
			},
			want: "https://api.example.com/v1/chat/completions",
		},
		{
			name: "custom path stays unchanged when explicit mode is set",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatAPIMode:      provider.ChatAPIModeChatCompletions,
				ChatEndpointPath: "/v1/text/chatcompletion_v2",
			},
			want: "https://api.example.com/v1/v1/text/chatcompletion_v2",
		},
		{
			name: "slash keeps direct base url mode",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatAPIMode:      provider.ChatAPIModeResponses,
				ChatEndpointPath: "/",
			},
			want: "https://api.example.com/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveChatEndpoint(tt.cfg)
			if err != nil {
				t.Fatalf("resolveChatEndpoint() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveChatEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShouldUseCompatibleChatCompletionsEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  provider.RuntimeConfig
		want bool
	}{
		{
			name: "default completions endpoint uses sdk path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatEndpointPath: "",
			},
			want: false,
		},
		{
			name: "explicit default completions endpoint uses sdk path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1",
				ChatEndpointPath: "/chat/completions",
			},
			want: false,
		},
		{
			name: "custom completions endpoint uses compatible path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com",
				ChatEndpointPath: "/v1/text/chatcompletion_v2",
			},
			want: true,
		},
		{
			name: "direct base url mode uses compatible path",
			cfg: provider.RuntimeConfig{
				BaseURL:          "https://api.example.com/v1/text/chatcompletion_v2",
				ChatEndpointPath: "/",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseCompatibleChatCompletionsEndpoint(tt.cfg); got != tt.want {
				t.Fatalf("shouldUseCompatibleChatCompletionsEndpoint() = %v, want %v", got, tt.want)
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

	if shouldFallbackToCompatibleChatStream(io.EOF, false) {
		t.Fatal("did not expect fallback for EOF")
	}
	if !shouldFallbackToCompatibleChatStream(errors.New("SDK stream error: invalid character '[' after top-level value"), false) {
		t.Fatal("expected fallback for weak SSE decode error")
	}
	if !shouldFallbackToCompatibleChatStream(fmt.Errorf("SDK stream error: %w", &json.SyntaxError{Offset: 1}), false) {
		t.Fatal("expected fallback for json syntax error")
	}
	if !shouldFallbackToCompatibleChatStream(fmt.Errorf("SDK stream error: %w", io.ErrUnexpectedEOF), false) {
		t.Fatal("expected fallback for unexpected EOF")
	}
	if shouldFallbackToCompatibleChatStream(errors.New("context deadline exceeded"), false) {
		t.Fatal("did not expect fallback for non-decode error")
	}
	if shouldFallbackToCompatibleChatStream(errors.New("SDK stream error: invalid character '[' after top-level value"), true) {
		t.Fatal("did not expect fallback after payload has started")
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
	if !strings.Contains(wrapped.Error(), "network_error") {
		t.Fatalf("expected network provider error, got %v", wrapped)
	}

	mapped := wrapSDKRequestError(&openai.Error{Message: "invalid key", StatusCode: 401}, "send request")
	if !strings.Contains(mapped.Error(), "auth_failed") {
		t.Fatalf("expected mapped provider error, got %v", mapped)
	}

	timeoutErr := wrapSDKRequestError(timeoutNetError{}, "send request")
	if !strings.Contains(timeoutErr.Error(), "timeout") {
		t.Fatalf("expected timeout provider error, got %v", timeoutErr)
	}
}

func TestEmitSDKChatCompletionStreamTracksPayloadStart(t *testing.T) {
	t.Parallel()

	stream := &fakeOpenAICompatSDKStream{
		chunks: []openai.ChatCompletionChunk{
			{
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Delta: openai.ChatCompletionChunkChoiceDelta{
							Content: "hello",
						},
					},
				},
			},
		},
		err: errors.New("decode failed"),
	}

	events := make(chan providertypes.StreamEvent, 4)
	started, err := emitSDKChatCompletionStream(context.Background(), stream, events)
	if err == nil {
		t.Fatal("expected stream error")
	}
	if !started {
		t.Fatal("expected payloadStarted=true after text delta")
	}
	if len(drainOpenAICompatEvents(events)) == 0 {
		t.Fatal("expected forwarded events")
	}
}

func TestEmitSDKChatCompletionStreamKeepsPayloadStartFalseBeforeAnyEvent(t *testing.T) {
	t.Parallel()

	stream := &fakeOpenAICompatSDKStream{
		err: errors.New("decode failed"),
	}

	events := make(chan providertypes.StreamEvent, 4)
	started, err := emitSDKChatCompletionStream(context.Background(), stream, events)
	if err == nil {
		t.Fatal("expected stream error")
	}
	if started {
		t.Fatal("expected payloadStarted=false when no effective event was emitted")
	}
}

type fakeOpenAICompatSDKStream struct {
	chunks []openai.ChatCompletionChunk
	index  int
	err    error
}

func (s *fakeOpenAICompatSDKStream) Next() bool {
	if s.index >= len(s.chunks) {
		return false
	}
	s.index++
	return true
}

func (s *fakeOpenAICompatSDKStream) Current() openai.ChatCompletionChunk {
	if s.index == 0 {
		return openai.ChatCompletionChunk{}
	}
	return s.chunks[s.index-1]
}

func (s *fakeOpenAICompatSDKStream) Err() error {
	return s.err
}

func drainOpenAICompatEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	out := make([]providertypes.StreamEvent, 0, len(events))
	for {
		select {
		case evt := <-events:
			out = append(out, evt)
		default:
			return out
		}
	}
}
