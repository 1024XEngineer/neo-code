package context

import (
	"fmt"
	"strings"

	"neo-code/internal/config"
)

// AskTurn 表示 Ask 会话历史中的单轮问答快照。
type AskTurn struct {
	UserQuery string
	Assistant string
}

// AskPromptConfig 描述 Ask Prompt 构建时的裁剪配置。
type AskPromptConfig struct {
	MaxInputTokens  int
	RetainTurns     int
	SummaryMaxChars int
}

// AskPromptBuildResult 描述 Ask Prompt 构建结果及压缩元信息。
type AskPromptBuildResult struct {
	Prompt        string
	Compacted     bool
	Summary       string
	RetainedTurns []AskTurn
}

// BuildAskPrompt 基于历史问答与当前问题构建 Ask 输入文本，并按 token 阈值触发轻量压缩。
func BuildAskPrompt(history []AskTurn, currentQuery string, cfg AskPromptConfig) AskPromptBuildResult {
	normalizedQuery := strings.TrimSpace(currentQuery)
	if normalizedQuery == "" {
		return AskPromptBuildResult{}
	}

	normalizedCfg := normalizeAskPromptConfig(cfg)
	normalizedHistory := normalizeAskHistory(history)
	if len(normalizedHistory) == 0 {
		return AskPromptBuildResult{
			Prompt: normalizedQuery,
		}
	}

	fullPrompt := composeAskPrompt("", normalizedHistory, normalizedQuery)
	if estimateTokenCountByRunes(fullPrompt) <= normalizedCfg.MaxInputTokens {
		return AskPromptBuildResult{
			Prompt:        fullPrompt,
			Compacted:     false,
			Summary:       "",
			RetainedTurns: append([]AskTurn(nil), normalizedHistory...),
		}
	}

	summaryTurns, retainedTurns := compactAskTurns(normalizedHistory, normalizedCfg.RetainTurns)
	summary := buildAskPromptSummary(summaryTurns, normalizedCfg.SummaryMaxChars)
	prompt := composeAskPrompt(summary, retainedTurns, normalizedQuery)
	prompt = trimAskPrompt(prompt, normalizedQuery, normalizedCfg.MaxInputTokens*4, normalizedCfg.SummaryMaxChars)

	return AskPromptBuildResult{
		Prompt:        prompt,
		Compacted:     len(summaryTurns) > 0,
		Summary:       summary,
		RetainedTurns: retainedTurns,
	}
}

// normalizeAskPromptConfig 对 Ask Prompt 配置进行默认值补齐。
func normalizeAskPromptConfig(cfg AskPromptConfig) AskPromptConfig {
	out := AskPromptConfig{
		MaxInputTokens:  cfg.MaxInputTokens,
		RetainTurns:     cfg.RetainTurns,
		SummaryMaxChars: cfg.SummaryMaxChars,
	}
	if out.MaxInputTokens <= 0 {
		out.MaxInputTokens = config.DefaultAskMaxInputTokens
	}
	if out.RetainTurns <= 0 {
		out.RetainTurns = config.DefaultAskRetainTurns
	}
	if out.SummaryMaxChars <= 0 {
		out.SummaryMaxChars = config.DefaultAskSummaryMaxChars
	}
	return out
}

// normalizeAskHistory 清理历史问答并移除无效轮次。
func normalizeAskHistory(history []AskTurn) []AskTurn {
	normalized := make([]AskTurn, 0, len(history))
	for _, turn := range history {
		query := strings.TrimSpace(turn.UserQuery)
		if query == "" {
			continue
		}
		normalized = append(normalized, AskTurn{
			UserQuery: query,
			Assistant: strings.TrimSpace(turn.Assistant),
		})
	}
	return normalized
}

// compactAskTurns 根据保留轮次拆分需摘要部分和保留部分。
func compactAskTurns(history []AskTurn, retainTurns int) ([]AskTurn, []AskTurn) {
	if len(history) == 0 {
		return nil, nil
	}
	if retainTurns <= 0 {
		retainTurns = 1
	}
	if retainTurns >= len(history) {
		return nil, append([]AskTurn(nil), history...)
	}
	splitAt := len(history) - retainTurns
	return append([]AskTurn(nil), history[:splitAt]...), append([]AskTurn(nil), history[splitAt:]...)
}

// buildAskPromptSummary 将被折叠历史压缩为摘要文本。
func buildAskPromptSummary(turns []AskTurn, maxChars int) string {
	if len(turns) == 0 || maxChars <= 0 {
		return ""
	}
	var builder strings.Builder
	for _, turn := range turns {
		builder.WriteString("Q: ")
		builder.WriteString(strings.TrimSpace(turn.UserQuery))
		builder.WriteString("\nA: ")
		builder.WriteString(strings.TrimSpace(turn.Assistant))
		builder.WriteString("\n")
	}
	return trimTextByRunes(strings.TrimSpace(builder.String()), maxChars)
}

// composeAskPrompt 组装完整 Ask Prompt。
func composeAskPrompt(summary string, retained []AskTurn, currentQuery string) string {
	normalizedQuery := strings.TrimSpace(currentQuery)
	if normalizedQuery == "" {
		return ""
	}
	if strings.TrimSpace(summary) == "" && len(retained) == 0 {
		return normalizedQuery
	}

	var builder strings.Builder
	builder.WriteString("Continue the same Ask session and answer with context.\n\n")
	if strings.TrimSpace(summary) != "" {
		builder.WriteString("Summary:\n")
		builder.WriteString(strings.TrimSpace(summary))
		builder.WriteString("\n\n")
	}
	if len(retained) > 0 {
		builder.WriteString("Recent turns:\n")
		for index, turn := range retained {
			builder.WriteString(fmt.Sprintf("%d) User: %s\n", index+1, strings.TrimSpace(turn.UserQuery)))
			if answer := strings.TrimSpace(turn.Assistant); answer != "" {
				builder.WriteString(fmt.Sprintf("%d) Assistant: %s\n", index+1, answer))
			}
		}
		builder.WriteString("\n")
	}
	builder.WriteString("Current question:\n")
	builder.WriteString(normalizedQuery)
	return strings.TrimSpace(builder.String())
}

// trimAskPrompt 按近似 token 上限裁剪 Prompt，优先保留当前问题。
func trimAskPrompt(prompt string, currentQuery string, maxChars int, summaryMaxChars int) string {
	normalizedPrompt := strings.TrimSpace(prompt)
	normalizedQuery := strings.TrimSpace(currentQuery)
	if maxChars <= 0 || len([]rune(normalizedPrompt)) <= maxChars {
		return normalizedPrompt
	}

	if summaryMaxChars > 0 {
		head := "Continue the same Ask session and answer with context.\n\n"
		compactQuery := trimTextByRunes(normalizedQuery, maxChars/2)
		compactSummary := trimTextByRunes(normalizedPrompt, summaryMaxChars)
		candidate := strings.TrimSpace(head + compactSummary + "\n\nCurrent question:\n" + compactQuery)
		if len([]rune(candidate)) <= maxChars {
			return candidate
		}
	}

	labeledQuery := "Current question:\n" + normalizedQuery
	if len([]rune(labeledQuery)) <= maxChars {
		return labeledQuery
	}
	labelRunes := len([]rune("Current question:\n"))
	if maxChars > labelRunes+1 {
		return "Current question:\n" + trimTextByRunes(normalizedQuery, maxChars-labelRunes)
	}
	return trimTextByRunes(normalizedQuery, maxChars)
}

// trimTextByRunes 按 rune 数裁剪文本并添加省略号。
func trimTextByRunes(value string, maxRunes int) string {
	normalized := strings.TrimSpace(value)
	if maxRunes <= 0 || normalized == "" {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
}

// estimateTokenCountByRunes 使用字符长度估算 token 数，满足 Ask 轻量压缩触发判定。
func estimateTokenCountByRunes(text string) int {
	runeCount := len([]rune(strings.TrimSpace(text)))
	if runeCount <= 0 {
		return 0
	}
	tokenCount := (runeCount + 3) / 4
	if tokenCount <= 0 {
		return 1
	}
	return tokenCount
}
