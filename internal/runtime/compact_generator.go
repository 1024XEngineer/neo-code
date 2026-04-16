package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	"neo-code/internal/provider/streaming"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

type compactSummaryGenerator struct {
	providerFactory ProviderFactory
	providerConfig  provider.RuntimeConfig
	model           string
}

func newCompactSummaryGenerator(
	providerFactory ProviderFactory,
	providerCfg provider.RuntimeConfig,
	model string,
) contextcompact.SummaryGenerator {
	return &compactSummaryGenerator{
		providerFactory: providerFactory,
		providerConfig:  providerCfg,
		model:           strings.TrimSpace(model),
	}
}

// tolerantSummaryResponse 使用 json.RawMessage 接收数组字段，容忍 LLM 返回 string 代替 []string。
type tolerantSummaryResponse struct {
	TaskState struct {
		Goal            string          `json:"goal"`
		Progress        json.RawMessage `json:"progress"`
		OpenItems       json.RawMessage `json:"open_items"`
		NextStep        string          `json:"next_step"`
		Blockers        json.RawMessage `json:"blockers"`
		KeyArtifacts    json.RawMessage `json:"key_artifacts"`
		Decisions       json.RawMessage `json:"decisions"`
		UserConstraints json.RawMessage `json:"user_constraints"`
	} `json:"task_state"`
	DisplaySummary string `json:"display_summary"`
}

// Generate 使用冻结后的 provider 配置生成新的 durable task state 与展示摘要。
func (g *compactSummaryGenerator) Generate(
	ctx context.Context,
	input contextcompact.SummaryInput,
) (contextcompact.SummaryOutput, error) {
	if err := ctx.Err(); err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	if g.providerFactory == nil {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary generator provider factory is nil")
	}
	if strings.TrimSpace(g.providerConfig.Driver) == "" ||
		strings.TrimSpace(g.providerConfig.BaseURL) == "" ||
		strings.TrimSpace(g.providerConfig.APIKey) == "" {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary generator provider config is incomplete")
	}

	prompt := agentcontext.BuildCompactPrompt(agentcontext.CompactPromptInput{
		Mode:                     string(input.Mode),
		ManualStrategy:           input.Config.ManualStrategy,
		ManualKeepRecentMessages: input.Config.ManualKeepRecentMessages,
		ArchivedMessageCount:     input.ArchivedMessageCount,
		MaxSummaryChars:          input.Config.MaxSummaryChars,
		MaxArchivedPromptChars:   input.Config.MaxArchivedPromptChars,
		CurrentTaskState:         input.CurrentTaskState,
		ArchivedMessages:         input.ArchivedMessages,
		RetainedMessages:         input.RetainedMessages,
	})

	modelProvider, err := g.providerFactory.Build(ctx, g.providerConfig)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	outcome := generateStreamingMessage(ctx, modelProvider, providertypes.GenerateRequest{
		Model:        g.model,
		SystemPrompt: prompt.SystemPrompt,
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(prompt.UserPrompt)},
		}},
	}, streaming.Hooks{})
	if outcome.err != nil {
		return contextcompact.SummaryOutput{}, outcome.err
	}

	message := outcome.message
	if len(message.ToolCalls) > 0 {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary response must not contain tool calls")
	}

	return parseCompactSummaryOutput(renderCompactSummaryResponseParts(message.Parts))
}

// renderCompactSummaryResponseParts 提取 compact 生成器响应文本，非文本分片仅保留占位以避免二进制泄露。
func renderCompactSummaryResponseParts(parts []providertypes.ContentPart) string {
	var builder strings.Builder
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			builder.WriteString(part.Text)
		case providertypes.ContentPartImage:
			builder.WriteString("[Image]")
		}
	}
	return builder.String()
}

// parseCompactSummaryOutput 解析 compact 生成器返回的 JSON 响应，容忍数组字段被返回为字符串。
func parseCompactSummaryOutput(content string) (contextcompact.SummaryOutput, error) {
	jsonText, err := extractJSONObject(content)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	raw, err := decodeCompactSummaryResponse(jsonText)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	task := raw.TaskState
	progress, err := coerceStringArray("progress", task.Progress)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	openItems, err := coerceStringArray("open_items", task.OpenItems)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	blockers, err := coerceStringArray("blockers", task.Blockers)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	keyArtifacts, err := coerceStringArray("key_artifacts", task.KeyArtifacts)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	decisions, err := coerceStringArray("decisions", task.Decisions)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}
	userConstraints, err := coerceStringArray("user_constraints", task.UserConstraints)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	output := contextcompact.SummaryOutput{
		DisplaySummary: strings.TrimSpace(raw.DisplaySummary),
		TaskState: agentsession.TaskState{
			Goal:            task.Goal,
			Progress:        progress,
			OpenItems:       openItems,
			NextStep:        task.NextStep,
			Blockers:        blockers,
			KeyArtifacts:    keyArtifacts,
			Decisions:       decisions,
			UserConstraints: userConstraints,
		},
	}

	if output.DisplaySummary == "" {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary response is empty")
	}
	return output, nil
}

// decodeCompactSummaryResponse 对 compact JSON 响应执行严格解码，拒绝未知字段与尾随垃圾内容。
func decodeCompactSummaryResponse(jsonText string) (tolerantSummaryResponse, error) {
	decoder := json.NewDecoder(strings.NewReader(jsonText))
	decoder.DisallowUnknownFields()

	var response tolerantSummaryResponse
	if err := decoder.Decode(&response); err != nil {
		return tolerantSummaryResponse{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != nil && !errors.Is(err, io.EOF) {
		return tolerantSummaryResponse{}, errors.New("runtime: compact summary response contains trailing JSON content")
	}
	return response, nil
}

// coerceStringArray 尝试将 json.RawMessage 解析为 []string，容忍单个 string 值。
func coerceStringArray(fieldName string, raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// 根据首字节判断 JSON 类型，避免双重 Unmarshal
	switch raw[0] {
	case '[':
		var arr []string
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, fmt.Errorf("runtime: compact summary task_state.%s must be string array: %w", fieldName, err)
		}
		return arr, nil
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("runtime: compact summary task_state.%s must be string: %w", fieldName, err)
		}
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			return []string{trimmed}, nil
		}
		return nil, nil
	case 'n':
		return nil, nil
	}
	return nil, fmt.Errorf("runtime: compact summary task_state.%s must be string or string array", fieldName)
}

// extractJSONObject 从模型响应中提取首个满足 compact 协议的 JSON 对象，容忍前后噪音。
func extractJSONObject(text string) (string, error) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return "", errors.New("runtime: compact summary response does not contain a JSON object")
	}

	for {
		candidate, err := extractJSONObjectCandidate(text, start)
		if err == nil {
			// 与最终解析保持一致：候选对象必须通过严格解码且包含非空 display_summary。
			if probe, decodeErr := decodeCompactSummaryResponse(candidate); decodeErr == nil &&
				strings.TrimSpace(probe.DisplaySummary) != "" {
				return candidate, nil
			}
		}

		next := strings.IndexByte(text[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}

	return "", errors.New("runtime: compact summary response does not contain a valid compact JSON object")
}

// extractJSONObjectCandidate 从给定起点抽取平衡的 JSON 对象片段。
func extractJSONObjectCandidate(text string, start int) (string, error) {
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(text); index++ {
		ch := text[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : index+1]), nil
			}
		}
	}

	return "", errors.New("runtime: compact summary response contains an incomplete JSON object")
}
