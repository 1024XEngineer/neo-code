package conformance_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	"neo-code/internal/provider/anthropic"
	"neo-code/internal/provider/deepseek"
	"neo-code/internal/provider/gemini"
	"neo-code/internal/provider/mimo"
	"neo-code/internal/provider/minimax"
	"neo-code/internal/provider/openaicompat"
	"neo-code/internal/provider/openaicompat/glm"
	"neo-code/internal/provider/openaicompat/qwen"
	providertypes "neo-code/internal/provider/types"
)

func TestGenerateContractAcrossDrivers(t *testing.T) {
	testCases := []struct {
		name           string
		driver         provider.DriverDefinition
		buildConfig    func(baseURL string) provider.RuntimeConfig
		expectedPath   string
		expectedHeader string
		streamBody     string
		expectReason   string
		expectTokens   int
		expectedOrder  []providertypes.StreamEventType
	}{
		{
			name:   "openaicompat_chat_completions",
			driver: openaicompat.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:             "openai",
					Driver:           provider.DriverOpenAICompat,
					BaseURL:          baseURL,
					DefaultModel:     "gpt-4.1",
					APIKeyEnv:        "OPENAI_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/chat/completions",
				}
			},
			expectedPath:   "/chat/completions",
			expectedHeader: "Authorization",
			streamBody: "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_read_file\"}}]}}]}\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n" +
				"data: [DONE]\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "gemini_native",
			driver: gemini.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:             "gemini",
					Driver:           provider.DriverGemini,
					BaseURL:          baseURL,
					DefaultModel:     "gemini-2.5-flash",
					APIKeyEnv:        "GEMINI_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/models",
				}
			},
			expectedPath:   "/models/gemini-2.5-flash:streamGenerateContent",
			expectedHeader: "x-goog-api-key",
			streamBody: "data: {\"candidates\":[{\"index\":0,\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n" +
				"data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\",\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"filesystem_read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "anthropic_messages",
			driver: anthropic.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:             "anthropic",
					Driver:           provider.DriverAnthropic,
					BaseURL:          baseURL,
					DefaultModel:     "claude-3-7-sonnet",
					APIKeyEnv:        "ANTHROPIC_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/messages",
				}
			},
			expectedPath:   "/v1/messages",
			expectedHeader: "x-api-key",
			streamBody: "event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"Hello \"}}\n\n" +
				"event: content_block_start\n" +
				"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"filesystem_read_file\"}}\n\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":6}}\n\n",
			expectReason: "tool_use",
			expectTokens: 10,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "deepseek_chat_completions_with_reasoning",
			driver: deepseek.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           deepseek.DriverName,
					Driver:         provider.DriverDeepSeek,
					BaseURL:        baseURL,
					DefaultModel:   "deepseek-v4",
					APIKeyEnv:      "DEEPSEEK_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			expectedPath:   "/v1/chat/completions",
			expectedHeader: "Authorization",
			streamBody: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\",\"content\":\"Hello \"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_read_file\"}}]}}]}\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n" +
				"data: [DONE]\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventThinkingDelta,
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "mimo_chat_completions_with_reasoning",
			driver: mimo.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           mimo.DriverName,
					Driver:         provider.DriverMiMo,
					BaseURL:        baseURL,
					DefaultModel:   "mimo-v2.5",
					APIKeyEnv:      "MIMO_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			expectedPath:   "/chat/completions",
			expectedHeader: "Authorization",
			streamBody: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\",\"content\":\"Hello \"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_read_file\"}}]}}]}\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n" +
				"data: [DONE]\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventThinkingDelta,
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "qwen_chat_completions_with_reasoning",
			driver: qwen.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           qwen.DriverName,
					Driver:         provider.DriverQwen,
					BaseURL:        baseURL,
					DefaultModel:   "qwen3",
					APIKeyEnv:      "QWEN_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			expectedPath:   "/chat/completions",
			expectedHeader: "Authorization",
			streamBody: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\",\"content\":\"Hello \"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_read_file\"}}]}}]}\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n" +
				"data: [DONE]\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventThinkingDelta,
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
		{
			name:   "glm_chat_completions_with_reasoning",
			driver: glm.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           glm.DriverName,
					Driver:         provider.DriverGLM,
					BaseURL:        baseURL,
					DefaultModel:   "glm-5.1",
					APIKeyEnv:      "GLM_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			expectedPath:   "/chat/completions",
			expectedHeader: "Authorization",
			streamBody: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\",\"content\":\"Hello \"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"filesystem_read_file\"}}]}}]}\n" +
				"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n" +
				"data: [DONE]\n\n",
			expectReason: "stop",
			expectTokens: 7,
			expectedOrder: []providertypes.StreamEventType{
				providertypes.StreamEventThinkingDelta,
				providertypes.StreamEventTextDelta,
				providertypes.StreamEventToolCallStart,
				providertypes.StreamEventToolCallDelta,
				providertypes.StreamEventMessageDone,
			},
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.expectedPath {
					t.Fatalf("expected path %q, got %q", tt.expectedPath, r.URL.Path)
				}
				if headerValue := strings.TrimSpace(r.Header.Get(tt.expectedHeader)); headerValue == "" {
					t.Fatalf("expected header %q to be set", tt.expectedHeader)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, tt.streamBody)
			}))
			defer server.Close()

			cfg := tt.buildConfig(server.URL)
			p, err := tt.driver.Build(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			events := make(chan providertypes.StreamEvent, 32)
			err = p.Generate(context.Background(), generateRequestWithAssets(), events)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			drained := drainEvents(events)
			if len(drained) != len(tt.expectedOrder) {
				t.Fatalf("expected %d events, got %d (%+v)", len(tt.expectedOrder), len(drained), drained)
			}
			for i := range tt.expectedOrder {
				if drained[i].Type != tt.expectedOrder[i] {
					t.Fatalf("unexpected event order at index %d, expected %q got %q", i, tt.expectedOrder[i], drained[i].Type)
				}
			}

			done, doneErr := drained[len(drained)-1].MessageDoneValue()
			if doneErr != nil {
				t.Fatalf("MessageDoneValue() error = %v", doneErr)
			}
			if done.FinishReason != tt.expectReason {
				t.Fatalf("expected finish reason %q, got %q", tt.expectReason, done.FinishReason)
			}
			if done.Usage == nil || done.Usage.TotalTokens != tt.expectTokens {
				t.Fatalf("expected total tokens %d, got %+v", tt.expectTokens, done.Usage)
			}
		})
	}
}

func TestDiscoverContractAcrossDrivers(t *testing.T) {
	testCases := []struct {
		name           string
		driver         provider.DriverDefinition
		buildConfig    func(baseURL string) provider.RuntimeConfig
		expectedPath   string
		expectedHeader string
		responseBody   string
	}{
		{
			name:   "openaicompat_discover",
			driver: openaicompat.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:                  "openai",
					Driver:                provider.DriverOpenAICompat,
					BaseURL:               baseURL,
					APIKeyEnv:             "OPENAI_TEST_KEY",
					APIKeyResolver:        provider.StaticAPIKeyResolver("test-key"),
					DiscoveryEndpointPath: "/models",
				}
			},
			expectedPath:   "/models",
			expectedHeader: "Authorization",
			responseBody:   `{"data":[{"id":"gpt-4.1","name":"GPT 4.1"}]}`,
		},
		{
			name:   "gemini_discover",
			driver: gemini.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:                  "gemini",
					Driver:                provider.DriverGemini,
					BaseURL:               baseURL,
					APIKeyEnv:             "GEMINI_TEST_KEY",
					APIKeyResolver:        provider.StaticAPIKeyResolver("test-key"),
					DiscoveryEndpointPath: "/models",
				}
			},
			expectedPath:   "/models",
			expectedHeader: "x-goog-api-key",
			responseBody:   `{"models":[{"name":"models/gemini-2.5-flash","displayName":"Gemini 2.5 Flash"}]}`,
		},
		{
			name:   "anthropic_discover",
			driver: anthropic.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:                  "anthropic",
					Driver:                provider.DriverAnthropic,
					BaseURL:               baseURL,
					APIKeyEnv:             "ANTHROPIC_TEST_KEY",
					APIKeyResolver:        provider.StaticAPIKeyResolver("test-key"),
					DiscoveryEndpointPath: "/models",
				}
			},
			expectedPath:   "/v1/models",
			expectedHeader: "x-api-key",
			responseBody:   `{"data":[{"id":"claude-3-7-sonnet","display_name":"Claude 3.7 Sonnet"}],"has_more":false}`,
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.expectedPath {
					t.Fatalf("expected path %q, got %q", tt.expectedPath, r.URL.Path)
				}
				if headerValue := strings.TrimSpace(r.Header.Get(tt.expectedHeader)); headerValue == "" {
					t.Fatalf("expected header %q to be set", tt.expectedHeader)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.responseBody)
			}))
			defer server.Close()

			models, err := tt.driver.Discover(context.Background(), tt.buildConfig(server.URL))
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if len(models) == 0 || strings.TrimSpace(models[0].ID) == "" {
				t.Fatalf("expected discovered models with non-empty ID, got %+v", models)
			}
		})
	}
}

func TestDiscoverUnsupportedDrivers(t *testing.T) {
	testCases := []struct {
		name   string
		driver provider.DriverDefinition
	}{
		{name: "deepseek_discover_unsupported", driver: deepseek.Driver()},
		{name: "minimax_discover_unsupported", driver: minimax.Driver()},
		{name: "qwen_discover_unsupported", driver: qwen.Driver()},
		{name: "glm_discover_unsupported", driver: glm.Driver()},
		{name: "mimo_discover_unsupported", driver: mimo.Driver()},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			reg := provider.NewRegistry()
			if err := reg.Register(tt.driver); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if reg.SupportsDiscovery(tt.driver.Name) {
				t.Fatalf("expected driver %q to not support discovery", tt.driver.Name)
			}
			_, err := reg.DiscoverModels(context.Background(), provider.RuntimeConfig{Driver: tt.driver.Name})
			if err == nil {
				t.Fatal("expected discovery unsupported error, got nil")
			}
			if !provider.IsDiscoveryConfigError(err) {
				t.Fatalf("expected discovery config error, got %T: %v", err, err)
			}
		})
	}
}

func TestGenerateErrorClassificationAcrossDrivers(t *testing.T) {
	testCases := []struct {
		name        string
		driver      provider.DriverDefinition
		buildConfig func(baseURL string) provider.RuntimeConfig
		path        string
		body        string
	}{
		{
			name:   "openaicompat_auth_error",
			driver: openaicompat.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:           provider.DriverOpenAICompat,
					BaseURL:          baseURL,
					DefaultModel:     "gpt-4.1",
					APIKeyEnv:        "OPENAI_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/chat/completions",
				}
			},
			path: "/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "deepseek_auth_error",
			driver: deepseek.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:         provider.DriverDeepSeek,
					BaseURL:        baseURL,
					DefaultModel:   "deepseek-v4",
					APIKeyEnv:      "DEEPSEEK_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			path: "/v1/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "gemini_auth_error",
			driver: gemini.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:           provider.DriverGemini,
					BaseURL:          baseURL,
					DefaultModel:     "gemini-2.5-flash",
					APIKeyEnv:        "GEMINI_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/models",
				}
			},
			path: "/models/gemini-2.5-flash:streamGenerateContent",
			body: `{"error":{"message":"invalid x-goog-api-key"}}`,
		},
		{
			name:   "anthropic_auth_error",
			driver: anthropic.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:           provider.DriverAnthropic,
					BaseURL:          baseURL,
					DefaultModel:     "claude-3-7-sonnet",
					APIKeyEnv:        "ANTHROPIC_TEST_KEY",
					APIKeyResolver:   provider.StaticAPIKeyResolver("test-key"),
					ChatEndpointPath: "/messages",
				}
			},
			path: "/v1/messages",
			body: `{"error":{"message":"invalid x-api-key"}}`,
		},
		{
			name:   "mimo_auth_error",
			driver: mimo.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:         provider.DriverMiMo,
					BaseURL:        baseURL,
					DefaultModel:   "mimo-v2.5",
					APIKeyEnv:      "MIMO_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			path: "/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "minimax_auth_error",
			driver: minimax.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:         provider.DriverMiniMax,
					BaseURL:        baseURL + "/chat/completions",
					DefaultModel:   "minimax-m2.7",
					APIKeyEnv:      "MINIMAX_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			path: "/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "qwen_auth_error",
			driver: qwen.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:         provider.DriverQwen,
					BaseURL:        baseURL,
					DefaultModel:   "qwen3",
					APIKeyEnv:      "QWEN_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			path: "/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
		{
			name:   "glm_auth_error",
			driver: glm.Driver(),
			buildConfig: func(baseURL string) provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Driver:         provider.DriverGLM,
					BaseURL:        baseURL,
					DefaultModel:   "glm-5.1",
					APIKeyEnv:      "GLM_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
			path: "/chat/completions",
			body: `{"error":{"message":"invalid api key"}}`,
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("expected path %q, got %q", tt.path, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			p, err := tt.driver.Build(context.Background(), tt.buildConfig(server.URL))
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			err = p.Generate(context.Background(), providertypes.GenerateRequest{
				Messages: []providertypes.Message{
					{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
				},
			}, make(chan providertypes.StreamEvent, 4))
			if err == nil {
				t.Fatal("expected auth error, got nil")
			}

			var pErr *provider.ProviderError
			if !strings.Contains(err.Error(), "provider error") && !strings.Contains(err.Error(), "unauthorized") {
				t.Fatalf("expected provider error message, got %v", err)
			}
			if !errors.As(err, &pErr) {
				t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
			}
			if pErr.Code != provider.ErrorCodeAuthFailed {
				t.Fatalf("expected auth_failed code, got %+v", pErr)
			}
		})
	}
}

func TestEstimateInputTokensContractAcrossDrivers(t *testing.T) {
	testCases := []struct {
		name        string
		driver      provider.DriverDefinition
		buildConfig func() provider.RuntimeConfig
	}{
		{
			name:   "deepseek_estimate",
			driver: deepseek.Driver(),
			buildConfig: func() provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           deepseek.DriverName,
					Driver:         provider.DriverDeepSeek,
					BaseURL:        "https://api.deepseek.example",
					DefaultModel:   "deepseek-v4",
					APIKeyEnv:      "DEEPSEEK_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
		},
		{
			name:   "mimo_estimate",
			driver: mimo.Driver(),
			buildConfig: func() provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           mimo.DriverName,
					Driver:         provider.DriverMiMo,
					BaseURL:        "https://api.mimo.example",
					DefaultModel:   "mimo-v2.5",
					APIKeyEnv:      "MIMO_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
		},
		{
			name:   "minimax_estimate",
			driver: minimax.Driver(),
			buildConfig: func() provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           minimax.DriverName,
					Driver:         provider.DriverMiniMax,
					BaseURL:        "https://api.minimax.example/chat/completions",
					DefaultModel:   "minimax-m2.7",
					APIKeyEnv:      "MINIMAX_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
		},
		{
			name:   "qwen_estimate",
			driver: qwen.Driver(),
			buildConfig: func() provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           qwen.DriverName,
					Driver:         provider.DriverQwen,
					BaseURL:        "https://api.qwen.example",
					DefaultModel:   "qwen3",
					APIKeyEnv:      "QWEN_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
		},
		{
			name:   "glm_estimate",
			driver: glm.Driver(),
			buildConfig: func() provider.RuntimeConfig {
				return provider.RuntimeConfig{
					Name:           glm.DriverName,
					Driver:         provider.DriverGLM,
					BaseURL:        "https://api.glm.example",
					DefaultModel:   "glm-5.1",
					APIKeyEnv:      "GLM_TEST_KEY",
					APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
				}
			},
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			p, err := tt.driver.Build(context.Background(), tt.buildConfig())
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			estimate, err := p.EstimateInputTokens(context.Background(), generateRequestWithAssets())
			if err != nil {
				t.Fatalf("EstimateInputTokens() error = %v", err)
			}
			if estimate.EstimatedInputTokens <= 0 {
				t.Fatalf("expected positive token estimate, got %+v", estimate)
			}
			if estimate.EstimateSource != provider.EstimateSourceLocal {
				t.Fatalf("expected local estimate source, got %+v", estimate)
			}
		})
	}
}

func generateRequestWithAssets() providertypes.GenerateRequest {
	return providertypes.GenerateRequest{
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("look"),
					providertypes.NewRemoteImagePart("https://example.com/cat.png"),
					providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
				},
			},
		},
		Tools: []providertypes.ToolSpec{
			{
				Name:        "filesystem_read_file",
				Description: "read file",
				Schema:      map[string]any{"type": "object"},
			},
		},
		SessionAssetReader: staticAssetReader{
			dataByID: map[string]assetPayload{
				"asset-1": {mime: "image/png", data: []byte("image-bytes")},
			},
		},
	}
}

func drainEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	drained := make([]providertypes.StreamEvent, 0, 8)
	for {
		select {
		case event := <-events:
			drained = append(drained, event)
		default:
			return drained
		}
	}
}

type assetPayload struct {
	mime string
	data []byte
}

type staticAssetReader struct {
	dataByID map[string]assetPayload
}

func (r staticAssetReader) Open(_ context.Context, assetID string) (io.ReadCloser, string, error) {
	payload, ok := r.dataByID[assetID]
	if !ok {
		return nil, "", fmt.Errorf("asset not found: %s", assetID)
	}
	return io.NopCloser(strings.NewReader(string(payload.data))), payload.mime, nil
}
