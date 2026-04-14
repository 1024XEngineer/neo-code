package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	"neo-code/internal/provider/streaming"
	providertypes "neo-code/internal/provider/types"
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
			Role:    providertypes.RoleUser,
			Content: prompt.UserPrompt,
		}},
	}, streaming.Hooks{})
	if outcome.err != nil {
		return contextcompact.SummaryOutput{}, outcome.err
	}

	message := outcome.message
	if len(message.ToolCalls) > 0 {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary response must not contain tool calls")
	}

	return parseCompactSummaryOutput(message.Content)
}

// parseCompactSummaryOutput 解析 compact 生成器返回的 JSON 响应，容忍数组字段被返回为字符串。
func parseCompactSummaryOutput(content string) (contextcompact.SummaryOutput, error) {
	jsonText, err := extractJSONObject(content)
	if err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	var raw tolerantSummaryResponse
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return contextcompact.SummaryOutput{}, err
	}

	output := contextcompact.SummaryOutput{
		DisplaySummary: strings.TrimSpace(raw.DisplaySummary),
	}
	output.TaskState.Goal = raw.TaskState.Goal
	output.TaskState.Progress = coerceStringArray(raw.TaskState.Progress)
	output.TaskState.OpenItems = coerceStringArray(raw.TaskState.OpenItems)
	output.TaskState.NextStep = raw.TaskState.NextStep
	output.TaskState.Blockers = coerceStringArray(raw.TaskState.Blockers)
	output.TaskState.KeyArtifacts = coerceStringArray(raw.TaskState.KeyArtifacts)
	output.TaskState.Decisions = coerceStringArray(raw.TaskState.Decisions)
	output.TaskState.UserConstraints = coerceStringArray(raw.TaskState.UserConstraints)

	if output.DisplaySummary == "" {
		return contextcompact.SummaryOutput{}, errors.New("runtime: compact summary response is empty")
	}
	return output, nil
}

// coerceStringArray 尝试将 json.RawMessage 解析为 []string，容忍单个 string 值。
func coerceStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	// 根据首字节判断 JSON 类型，避免双重 Unmarshal
	switch raw[0] {
	case '[':
		var arr []string
		if err := json.Unmarshal(raw, &arr); err == nil {
			return arr
		}
		return nil
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			s = strings.TrimSpace(s)
			if s != "" {
				return []string{s}
			}
		}
		return nil
	default:
		// null、数字、布尔、对象等均返回 nil
		return nil
	}
}

// extractJSONObject 从模型响应中提取最外层 JSON 对象，容忍前后噪音。
func extractJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", errors.New("runtime: compact summary response does not contain a JSON object")
	}

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
