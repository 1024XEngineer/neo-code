package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

func TestDriver(t *testing.T) {
	t.Parallel()

	driver := Driver()
	if driver.Name != DriverName {
		t.Fatalf("expected driver name %q, got %q", DriverName, driver.Name)
	}
	if driver.Build == nil {
		t.Fatal("expected Build function to be non-nil")
	}
	if driver.Discover == nil {
		t.Fatal("expected Discover function to be non-nil")
	}
}

func TestWithTransport(t *testing.T) {
	t.Parallel()

	customTransport := &http.Transport{}
	cfg := resolvedConfig("", "")

	p, err := New(cfg, withTransport(customTransport))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if p.generateClient.Transport != customTransport {
		t.Fatal("expected generate client transport to be set")
	}
	if p.discoveryClient.Transport != customTransport {
		t.Fatal("expected discovery client transport to be set")
	}
}

func TestNewValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("empty api key returns error", func(t *testing.T) {
		t.Parallel()
		cfg := resolvedConfig("", "")
		cfg.APIKeyEnv = ""
		cfg.APIKeyResolver = nil
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty api key")
		}
		if !strings.Contains(err.Error(), "api_key_env is empty") {
			t.Fatalf("expected api key error, got: %v", err)
		}
	})

	t.Run("whitespace-only api key returns error", func(t *testing.T) {
		t.Parallel()
		cfg := resolvedConfig("", "")
		cfg.APIKeyEnv = "   "
		cfg.APIKeyResolver = nil
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for whitespace-only api key")
		}
	})

	t.Run("invalid config validate fails", func(t *testing.T) {
		t.Parallel()
		cfg := provider.RuntimeConfig{
			Name:           DriverName,
			Driver:         DriverName,
			BaseURL:        "",
			DefaultModel:   config.OpenAIDefaultModel,
			APIKeyEnv:      "OPENAI_TEST_KEY",
			APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty base url")
		}
		if !strings.Contains(err.Error(), "base url is empty") {
			t.Fatalf("expected base url error, got: %v", err)
		}
	})
}

func TestNewDefaultTransportWhenNoOption(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig("", "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.generateClient.Transport == nil {
		t.Fatal("expected default transport to be set")
	}
}

func TestDefaultRetryTransport(t *testing.T) {
	t.Parallel()

	transport := defaultRetryTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestDiscoverModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4", "name": "GPT-4"},
				{"id": "gpt-3.5-turbo"},
			},
		})
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4" || models[0].Name != "GPT-4" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
}

func TestDiscoverModelsUsesConfiguredDiscoveryEndpointPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1-mini"},
			},
		})
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "")
	cfg.DiscoveryEndpointPath = "/gateway/models"
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4.1-mini" {
		t.Fatalf("expected configured discovery endpoint path to return one model, got %+v", models)
	}
}

func TestDiscoverModelsParsesGeminiProfileModelList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "gemini-2.5-flash", "displayName": "Gemini 2.5 Flash"},
			},
		})
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].Name != "Gemini 2.5 Flash" {
		t.Fatalf("expected gemini profile parsing, got %+v", models)
	}
}

func TestDiscoverModelsParsesNestedContainerAndAliasFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"models": []map[string]any{
					{"model_id": "qwen-plus", "displayname": "Qwen Plus"},
				},
			},
		})
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "qwen-plus" || models[0].Name != "Qwen Plus" {
		t.Fatalf("expected nested alias payload parsing, got %+v", models)
	}
}

func TestEstimateInputTokensReturnsAdvisoryLocalEstimate(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig("", ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	estimate, err := p.EstimateInputTokens(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	})
	if err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}
	if estimate.EstimateSource != provider.EstimateSourceLocal {
		t.Fatalf("estimate source = %q, want %q", estimate.EstimateSource, provider.EstimateSourceLocal)
	}
	if estimate.GatePolicy != provider.EstimateGateAdvisory {
		t.Fatalf("gate policy = %q, want %q", estimate.GatePolicy, provider.EstimateGateAdvisory)
	}
	if estimate.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive estimate tokens, got %d", estimate.EstimatedInputTokens)
	}
}

func TestEstimateThenGenerateReusesPreparedRequest(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}
data: [DONE]

`))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "gpt-4.1"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	reader := &singleUseSessionAssetReader{
		maxOpen: 1,
		assets: map[string]sessionAsset{
			"asset-1": {data: []byte("image-bytes"), mime: "image/png"},
		},
	}
	request := providertypes.GenerateRequest{
		Model: "gpt-4.1",
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-1", "image/png")},
			},
		},
		SessionAssetReader: reader,
	}
	if _, err := p.EstimateInputTokens(context.Background(), request); err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), request, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if reader.openCount != 1 {
		t.Fatalf("expected session asset to be opened once, got %d", reader.openCount)
	}
}

func TestGenerateRetriesReuseFrozenRequestPayload(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"retry later"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}
data: [DONE]

`))
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "gpt-4.1")
	cfg.ChatEndpointPath = "/gateway/chat/completions"
	cfg.GenerateMaxRetries = 1
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.generateClient = server.Client()

	reader := &singleUseSessionAssetReader{
		maxOpen: 1,
		assets: map[string]sessionAsset{
			"asset-1": {data: []byte("image-bytes"), mime: "image/png"},
		},
	}
	request := providertypes.GenerateRequest{
		Model: "gpt-4.1",
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-1", "image/png")},
			},
		},
		SessionAssetReader: reader,
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), request, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if reader.openCount != 1 {
		t.Fatalf("expected session asset to be opened once across retries, got %d", reader.openCount)
	}
}

func TestNewCreatesDedicatedDiscoveryClient(t *testing.T) {
	t.Parallel()

	customTransport := &http.Transport{}
	p, err := New(resolvedConfig("https://api.example.com/v1", "gpt-4.1"), withTransport(customTransport))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.discoveryClient.Timeout != provider.DefaultSDKRequestTimeout {
		t.Fatalf("expected discovery timeout %s, got %s", provider.DefaultSDKRequestTimeout, p.discoveryClient.Timeout)
	}
	if p.discoveryClient.Transport != customTransport {
		t.Fatalf("expected discovery client to preserve custom transport")
	}
	if p.generateClient == p.discoveryClient {
		t.Fatal("expected generate and discovery clients to stay separated")
	}
}

func TestDiscoverModelsOpenAIProfileFallsBackToGenericListKeys(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "thirdparty-chat", "display_name": "ThirdParty Chat"},
			},
		})
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "thirdparty-chat" || models[0].Name != "ThirdParty Chat" {
		t.Fatalf("expected openai profile fallback to generic list keys, got %+v", models)
	}
}

func TestDiscoverModelsParsesStringModelIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{"model-alpha", "model-beta"},
		})
	}))
	defer server.Close()

	cfg := resolvedConfig(server.URL, "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 || models[0].ID != "model-alpha" || models[1].ID != "model-beta" {
		t.Fatalf("expected string model ids to be normalized, got %+v", models)
	}
}

// --- toOpenAIMessage tests ---

func TestToOpenAIMessage_BasicMessage(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role:  "user",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello world")},
	}
	result, err := chatcompletions.ToOpenAIMessage(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("ToOpenAIMessage() basic error = %v", err)
	}

	if result.Role != "user" || result.Content != "hello world" {
		t.Fatalf("unexpected basic message: role=%q content=%q", result.Role, result.Content)
	}
	if result.ToolCallID != "" || len(result.ToolCalls) > 0 {
		t.Fatal("basic message should not have tool call fields")
	}
}

func TestToOpenAIMessage_ToolRoleMessage(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role:       "tool",
		Parts:      []providertypes.ContentPart{providertypes.NewTextPart("result data")},
		ToolCallID: "call_123",
	}
	result, err := chatcompletions.ToOpenAIMessage(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("ToOpenAIMessage() tool role error = %v", err)
	}

	if result.Role != "tool" || result.ToolCallID != "call_123" {
		t.Fatalf("unexpected tool message: role=%q toolCallID=%q", result.Role, result.ToolCallID)
	}
}

func TestToOpenAIMessage_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role: "assistant",
		ToolCalls: []providertypes.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
			{ID: "call_2", Name: "write_file", Arguments: `{"path":"test.go","content":"..."}`},
		},
	}
	result, err := chatcompletions.ToOpenAIMessage(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("ToOpenAIMessage() assistant error = %v", err)
	}

	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	tc1 := result.ToolCalls[0]
	if tc1.ID != "call_1" || tc1.Type != "function" {
		t.Fatalf("unexpected first tool call: id=%q type=%q", tc1.ID, tc1.Type)
	}
	if tc1.Function.Name != "read_file" || tc1.Function.Arguments != `{"path":"main.go"}` {
		t.Fatalf("unexpected first function: name=%q args=%q", tc1.Function.Name, tc1.Function.Arguments)
	}
	tc2 := result.ToolCalls[1]
	if tc2.Function.Name != "write_file" {
		t.Fatalf("unexpected second function name: %q", tc2.Function.Name)
	}
}

func TestToOpenAIMessage_EmptyToolCalls(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("test")}}
	result, err := chatcompletions.ToOpenAIMessage(context.Background(), msg, nil)
	if err != nil {
		t.Fatalf("ToOpenAIMessage() empty tool calls error = %v", err)
	}
	if len(result.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls for user message, got %d", len(result.ToolCalls))
	}
}

// --- buildRequest tests ---

func TestBuildRequest_EmptyModelReturnsError(t *testing.T) {
	t.Parallel()

	// Directly construct Provider and bypass New() validation
	// to test BuildRequest's own empty-model check.
	p := &Provider{
		cfg: provider.RuntimeConfig{
			Name:           DriverName,
			Driver:         DriverName,
			BaseURL:        config.OpenAIDefaultBaseURL,
			DefaultModel:   "",
			APIKeyEnv:      "OPENAI_TEST_KEY",
			APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
		},
		generateClient: &http.Client{},
	}

	_, buildErr := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{})
	if buildErr == nil {
		t.Fatal("expected error for empty model")
	}
	if !strings.Contains(buildErr.Error(), "model is empty") {
		t.Fatalf("unexpected error message: %v", buildErr)
	}
}

func TestBuildRequest_FallsBackToConfigModel(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.Model != config.OpenAIDefaultModel {
		t.Fatalf("expected model %q, got %q", config.OpenAIDefaultModel, payload.Model)
	}
}

func TestBuildRequest_RequestModelTakesPrecedence(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{Model: "gpt-4-custom", Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.Model != "gpt-4-custom" {
		t.Fatalf("expected model %q, got %q", "gpt-4-custom", payload.Model)
	}
}

func TestBuildRequest_NoSystemPrompt(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{SystemPrompt: "", Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	for _, msg := range payload.Messages {
		if msg.Role == "system" {
			t.Fatal("expected no system message when SystemPrompt is empty")
		}
	}
}

func TestBuildRequest_NoTools(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}, Tools: nil})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.ToolChoice != "" || len(payload.Tools) != 0 {
		t.Fatalf("expected no tools, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
}

func TestBuildRequest_EmptyToolsSlice(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}, Tools: []providertypes.ToolSpec{}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.ToolChoice != "" || len(payload.Tools) != 0 {
		t.Fatalf("expected empty tools for empty slice, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
}

func TestBuildRequest_MultipleTools(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("use tools")}}},
		Tools: []providertypes.ToolSpec{
			{Name: "tool_a", Description: "Tool A", Schema: map[string]any{"type": "object"}},
			{Name: "tool_b", Description: "Tool B", Schema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(payload.Tools))
	}
	if payload.ToolChoice != "auto" {
		t.Fatalf("expected tool_choice=auto, got %q", payload.ToolChoice)
	}
}

func TestBuildRequest_WhitespaceSystemPromptSkipped(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(context.Background(), p.cfg, providertypes.GenerateRequest{SystemPrompt: "   ", Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	for _, msg := range payload.Messages {
		if msg.Role == "system" {
			t.Fatal("expected no system message for whitespace-only system prompt")
		}
	}
}

// --- Generate tests ---

func TestGenerate_BaseURLTrailingSlashHandled(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}
data: [DONE]

`))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL+"/", "gpt-4.1"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.generateClient = server.Client()

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Model: "gpt-4.1",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}},
		},
	}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestDiscoverModelsSkipsInvalidEntriesAndDedupes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1", "name": "GPT-4.1"},
				{"foo": "bar"},
				{"id": "gpt-4.1", "name": "GPT-4.1 Duplicate"},
			},
		})
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "discover-key"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.discoveryClient = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected invalid and duplicate models to be filtered, got %+v", models)
	}
	if models[0].ID != "gpt-4.1" {
		t.Fatalf("expected remaining model to be gpt-4.1, got %+v", models[0])
	}
}

// --- helpers ---

func resolvedConfig(baseURL, model string) provider.RuntimeConfig {
	if baseURL == "" {
		baseURL = config.OpenAIDefaultBaseURL
	}
	if model == "" {
		model = config.OpenAIDefaultModel
	}
	return provider.RuntimeConfig{
		Name:           DriverName,
		Driver:         DriverName,
		BaseURL:        baseURL,
		DefaultModel:   model,
		APIKeyEnv:      "OPENAI_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	}
}

func drainStreamEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	var res []providertypes.StreamEvent
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return res
			}
			res = append(res, evt)
		case <-time.After(time.Millisecond * 100):
			return res
		}
	}
}

func requireTextDeltaPayload(t *testing.T, evt providertypes.StreamEvent) providertypes.TextDeltaPayload {
	if evt.Type != providertypes.StreamEventTextDelta {
		t.Fatalf("expected text_delta event, got %v", evt.Type)
	}
	if evt.TextDelta == nil {
		t.Fatal("expected non-nil TextDelta payload")
	}
	return *evt.TextDelta
}

func requireMessageDonePayload(t *testing.T, evt providertypes.StreamEvent) providertypes.MessageDonePayload {
	if evt.Type != providertypes.StreamEventMessageDone {
		t.Fatalf("expected message_done event, got %v", evt.Type)
	}
	if evt.MessageDone == nil {
		t.Fatal("expected non-nil MessageDone payload")
	}
	return *evt.MessageDone
}

type cancelThenErrorReader struct {
	cancel func()
	err    error
}

func (r *cancelThenErrorReader) Read(p []byte) (int, error) {
	r.cancel()
	return 0, r.err
}

type cancelOnEOFReader struct {
	reader io.Reader
	cancel func()
}

func (r *cancelOnEOFReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}

type cancelAfterDoneReader struct {
	payload []byte
	pos     int
	cancel  func()
	err     error
}

func (r *cancelAfterDoneReader) Read(p []byte) (int, error) {
	if r.pos < len(r.payload) {
		n := copy(p, r.payload[r.pos:])
		r.pos += n
		return n, nil
	}
	r.cancel()
	return 0, r.err
}

type sessionAsset struct {
	data []byte
	mime string
	err  error
}

type singleUseSessionAssetReader struct {
	assets    map[string]sessionAsset
	openCount int
	maxOpen   int
}

func (r *singleUseSessionAssetReader) Open(_ context.Context, assetID string) (io.ReadCloser, string, error) {
	if r.maxOpen > 0 && r.openCount >= r.maxOpen {
		return nil, "", fmt.Errorf("open limit exceeded for asset: %s", assetID)
	}
	r.openCount++
	asset, ok := r.assets[assetID]
	if !ok {
		return nil, "", fmt.Errorf("asset not found: %s", assetID)
	}
	if asset.err != nil {
		return nil, "", asset.err
	}
	return io.NopCloser(strings.NewReader(string(asset.data))), asset.mime, nil
}
