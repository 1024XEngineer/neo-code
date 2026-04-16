package memo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
)

const llmExtractorRecentMessageLimit = 10

// LLMExtractor 基于 LLM 分析最近对话，并返回结构化记忆条目。
type LLMExtractor struct {
	generator TextGenerator
	now       func() time.Time
}

type extractedEntry struct {
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Keywords []string `json:"keywords"`
}

// NewLLMExtractor 创建基于 TextGenerator 的记忆提取器。
func NewLLMExtractor(generator TextGenerator) *LLMExtractor {
	return &LLMExtractor{
		generator: generator,
		now:       time.Now,
	}
}

// Extract 从最近对话中提取可跨会话持久化的记忆条目。
func (e *LLMExtractor) Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.generator == nil {
		return nil, errors.New("memo: text generator is nil")
	}

	recent := agentcontext.BuildRecentMessagesForModel(messages, llmExtractorRecentMessageLimit)
	if len(recent) == 0 || !containsUserMessage(recent) {
		return nil, nil
	}

	response, err := e.generator.Generate(ctx, buildExtractionPrompt(e.now()), recent)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	jsonText, err := extractJSONArray(response)
	if err != nil {
		return nil, err
	}

	var extracted []extractedEntry
	if err := json.Unmarshal([]byte(jsonText), &extracted); err != nil {
		return nil, fmt.Errorf("memo: parse extraction response: %w", err)
	}

	entries := make([]Entry, 0, len(extracted))
	for _, item := range extracted {
		entry, ok := toMemoEntry(item)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// buildExtractionPrompt 构造记忆提取专用的 system prompt。
func buildExtractionPrompt(now time.Time) string {
	currentDate := now.In(time.Local).Format("2006-01-02")
	return strings.TrimSpace(fmt.Sprintf(`
你是一个记忆提取助手（memory extraction assistant）。
分析最近对话中值得跨会话持久记住的信息，并返回严格 JSON 数组。

当前本地日期：%s
如果对话中出现“今天、明天、下周二”等相对日期，必须先转换为绝对日期再写入 content。

只允许以下四种 type：
- user: 用户偏好、习惯、背景、专长
- feedback: 用户对 Agent 做法的纠正、要求、确认过的工作方式
- project: 项目事实、项目决策、截止时间、进行中的工作
- reference: 外部资源、文档、链接、仪表盘、沟通渠道

提取规则：
1. 只提取无法从代码仓库直接推导的信息。
2. 不要提取通用编程知识、代码结构、文件路径、Git 历史。
3. 每条记忆必须具体、可操作。
4. 没有值得记住的信息时，返回 []。
5. 输出必须是 JSON 数组，不要输出任何额外解释。

输出格式：
[{"type":"user","title":"...","content":"...","keywords":["..."]}]
`, currentDate))
}

// containsUserMessage 检查待提取消息中是否包含用户输入。
func containsUserMessage(messages []providertypes.Message) bool {
	for _, message := range messages {
		if message.Role == providertypes.RoleUser && hasMemoRelevantUserInput(message.Parts) {
			return true
		}
	}
	return false
}

// toMemoEntry 将 LLM 输出条目收敛为合法的 memo.Entry。
func toMemoEntry(item extractedEntry) (Entry, bool) {
	entryType, ok := ParseType(strings.TrimSpace(item.Type))
	if !ok {
		return Entry{}, false
	}

	title := NormalizeTitle(item.Title)
	content := strings.TrimSpace(item.Content)
	if title == "" || content == "" {
		return Entry{}, false
	}

	return Entry{
		Type:     entryType,
		Title:    title,
		Content:  content,
		Keywords: normalizeKeywords(item.Keywords),
		Source:   SourceAutoExtract,
	}, true
}

// normalizeKeywords 规范化关键词列表，移除空值和重复值。
func normalizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}

	result := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// extractJSONArray 从模型返回文本中提取最外层 JSON 数组，容忍前后噪声。
func extractJSONArray(text string) (string, error) {
	start := strings.Index(text, "[")
	if start < 0 {
		return "", errors.New("memo: extraction response does not contain a JSON array")
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
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : index+1]), nil
			}
		}
	}

	return "", errors.New("memo: extraction response contains an incomplete JSON array")
}
