package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const errorPrefix = "gemini provider: "

const defaultGenerateRetryMax = provider.DefaultGenerateMaxRetries

// Provider 封装 Gemini native 协议的请求发送与流式响应解析。
type Provider struct {
	cfg provider.RuntimeConfig

	mu       sync.Mutex
	prepared *preparedRequest

	retryBackoff func(attempt int) time.Duration
	retryWait    func(ctx context.Context, wait time.Duration) error
}

type preparedRequest struct {
	signature string
	model     string
	contents  []*genai.Content
	config    *genai.GenerateContentConfig
}

// EstimateInputTokens 基于 Gemini 最终请求结构做本地输入 token 估算。
func (p *Provider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	model, contents, genConfig, err := BuildRequest(ctx, p.cfg, req)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	payload := struct {
		Model    string                       `json:"model"`
		Contents []*genai.Content             `json:"contents"`
		Config   *genai.GenerateContentConfig `json:"config,omitempty"`
	}{
		Model:    model,
		Contents: contents,
		Config:   genConfig,
	}
	tokens, err := provider.EstimateSerializedPayloadTokens(payload)
	if err != nil {
		return providertypes.BudgetEstimate{}, err
	}
	p.storePreparedRequest(provider.BuildGenerateRequestSignature(req), model, contents, genConfig)
	return providertypes.BudgetEstimate{
		EstimatedInputTokens: tokens,
		EstimateSource:       provider.EstimateSourceLocal,
		GatePolicy:           provider.EstimateGateAdvisory,
	}, nil
}

// New 创建 Gemini native provider 实例。
func New(cfg provider.RuntimeConfig) (*Provider, error) {
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return nil, errors.New(errorPrefix + "api_key_env is empty")
	}
	return &Provider{
		cfg: cfg,
	}, nil
}

// Generate 发起 Gemini 流式请求，并将重试与超时语义收敛到 provider 公共 runner。
func (p *Provider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	model, contents, config, ok := p.takePreparedRequest(provider.BuildGenerateRequestSignature(req))
	if !ok {
		var err error
		model, contents, config, err = BuildRequest(ctx, p.cfg, req)
		if err != nil {
			return err
		}
	}
	normalizedModel := normalizeGeminiModelName(model)
	if normalizedModel == "" {
		return errors.New(errorPrefix + "model is empty")
	}

	return provider.RunGenerateWithRetryUsing(ctx, p.cfg, events, p.retryBackoff, p.retryWait, func(
		attemptCtx context.Context,
		attemptEvents chan<- providertypes.StreamEvent,
	) error {
		return p.generateOnce(attemptCtx, normalizedModel, contents, config, attemptEvents)
	})
}

// generateOnce 执行一次 Gemini 流式尝试，并将 SDK chunk 转为统一流式事件。
func (p *Provider) generateOnce(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
	events chan<- providertypes.StreamEvent,
) error {
	client, err := newGenerateSDKClient(ctx, p.cfg)
	if err != nil {
		return err
	}

	var (
		finishReason string
		usage        providertypes.Usage
		hasChunk     bool
		callSeq      int
	)
	for chunk, streamErr := range client.Models.GenerateContentStream(ctx, model, contents, config) {
		if streamErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return normalizeGenerateError(streamErr)
		}

		if chunk == nil {
			continue
		}
		hasChunk = true
		extractUsage(&usage, chunk.UsageMetadata)

		for _, candidate := range chunk.Candidates {
			if reason := normalizeFinishReason(string(candidate.FinishReason)); reason != "" {
				finishReason = reason
			}
			if candidate.Content == nil {
				continue
			}
			for _, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				if strings.TrimSpace(part.Text) != "" {
					if err := provider.EmitTextDelta(ctx, events, part.Text); err != nil {
						return err
					}
				}
				if part.FunctionCall == nil {
					continue
				}

				callSeq++
				callID := strings.TrimSpace(part.FunctionCall.ID)
				if callID == "" {
					callID = fmt.Sprintf("gemini-call-%d", callSeq)
				}
				name := strings.TrimSpace(part.FunctionCall.Name)
				if name == "" {
					continue
				}
				if err := provider.EmitToolCallStart(ctx, events, callSeq-1, callID, name); err != nil {
					return err
				}
				argsJSON, err := encodeArguments(part.FunctionCall.Args)
				if err != nil {
					return err
				}
				if err := provider.EmitToolCallDelta(ctx, events, callSeq-1, callID, argsJSON); err != nil {
					return err
				}
			}
		}
	}
	if !hasChunk {
		return fmt.Errorf("%w: empty gemini stream payload", provider.ErrStreamInterrupted)
	}
	if !usage.InputObserved && !usage.OutputObserved {
		return provider.EmitMessageDone(ctx, events, finishReason, nil)
	}
	return provider.EmitMessageDone(ctx, events, finishReason, &usage)
}

// storePreparedRequest 缓存估算阶段的 Gemini 构建结果，供同轮发送直接复用。
func (p *Provider) storePreparedRequest(
	signature string,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prepared = &preparedRequest{
		signature: strings.TrimSpace(signature),
		model:     model,
		contents:  contents,
		config:    config,
	}
}

// takePreparedRequest 读取并消费签名匹配的预构建请求，避免跨请求误复用。
func (p *Provider) takePreparedRequest(signature string) (string, []*genai.Content, *genai.GenerateContentConfig, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepared == nil {
		return "", nil, nil, false
	}
	current := p.prepared
	p.prepared = nil
	if strings.TrimSpace(signature) == "" || current.signature != strings.TrimSpace(signature) {
		return "", nil, nil, false
	}
	return current.model, current.contents, current.config, true
}

// normalizeGeminiModelName 统一清洗 Gemini 模型名，兼容 discover 返回的 "models/{id}" 形式。
func normalizeGeminiModelName(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "models/"))
}

// extractUsage 从 SDK usageMetadata 中抽取统一 token 统计。
func extractUsage(usage *providertypes.Usage, raw *genai.GenerateContentResponseUsageMetadata) {
	if raw == nil {
		return
	}
	usage.InputTokens = int(raw.PromptTokenCount)
	usage.OutputTokens = int(raw.CandidatesTokenCount)
	usage.TotalTokens = int(raw.TotalTokenCount)
	usage.InputObserved = true
	usage.OutputObserved = true
}

// encodeArguments 将函数参数对象编码为 JSON 字符串，供统一 tool_call_delta 事件复用。
func encodeArguments(args map[string]any) (string, error) {
	if len(args) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("%sencode function args: %w", errorPrefix, err)
	}
	return string(encoded), nil
}

// normalizeFinishReason 规范化 Gemini finish reason，便于上层统一处理。
func normalizeFinishReason(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// normalizeGenerateError 统一归类 Gemini 流式生成错误，避免把网络异常直接泄漏到 runtime。
func normalizeGenerateError(err error) error {
	if mappedErr := mapGeminiSDKError(err); mappedErr != nil {
		return mappedErr
	}

	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown stream error"
	}
	if isTimeoutGenerateError(err) {
		return provider.NewTimeoutProviderError("gemini generate timeout: " + message)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return provider.NewNetworkProviderError("gemini generate network error: " + message)
	}
	return fmt.Errorf("%sstream generate: %w", errorPrefix, err)
}

// isTimeoutGenerateError 判断 Gemini 流式生成错误是否由超时触发。
func isTimeoutGenerateError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// mapGeminiSDKError 将 Gemini SDK 错误映射为 provider 领域错误，仅保留状态码级别兜底。
func mapGeminiSDKError(err error) error {
	var apiErr genai.APIError
	if !errors.As(err, &apiErr) {
		var apiErrPtr *genai.APIError
		if !errors.As(err, &apiErrPtr) || apiErrPtr == nil {
			return nil
		}
		apiErr = *apiErrPtr
	}

	statusCode := apiErr.Code
	statusName := strings.ToUpper(strings.TrimSpace(apiErr.Status))
	message := strings.TrimSpace(apiErr.Message)

	if statusCode == 0 {
		switch statusName {
		case "UNAUTHENTICATED":
			statusCode = http.StatusUnauthorized
		case "PERMISSION_DENIED":
			statusCode = http.StatusForbidden
		case "RESOURCE_EXHAUSTED":
			statusCode = http.StatusTooManyRequests
		default:
			statusCode = http.StatusBadRequest
		}
	}
	if message == "" {
		message = strings.TrimSpace(err.Error())
	}
	if statusCode == http.StatusBadRequest {
		normalized := strings.ToLower(message)
		switch {
		case strings.Contains(normalized, "api key"),
			strings.Contains(normalized, "api-key"),
			strings.Contains(normalized, "x-goog-api-key"),
			strings.Contains(normalized, "unauthorized"):
			statusCode = http.StatusUnauthorized
		case strings.Contains(normalized, "rate limit"),
			strings.Contains(normalized, "quota"),
			strings.Contains(normalized, "resource_exhausted"):
			statusCode = http.StatusTooManyRequests
		}
	}
	return provider.NewProviderErrorFromStatus(statusCode, message)
}
