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

var (
	// ErrExtractionNoJSONArray 表示提取结果中找不到合法 JSON 数组起始。
	ErrExtractionNoJSONArray = errors.New("memo: extraction response does not contain a JSON array")
	// ErrExtractionIncompleteJSONArray 表示提取结果中的 JSON 数组不完整。
	ErrExtractionIncompleteJSONArray = errors.New("memo: extraction response contains an incomplete JSON array")
)

// LLMExtractor 基于 LLM 分析最近对话，并返回结构化记忆条目。
type LLMExtractor struct {
	generator          TextGenerator
	now                func() time.Time
	recentMessageLimit int
}

type extractedEntry struct {
	Action   string   `json:"action"`
	Ref      string   `json:"ref"`
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Keywords []string `json:"keywords"`
}

// NewLLMExtractor 创建基于 TextGenerator 的记忆提取器。
func NewLLMExtractor(generator TextGenerator, recentMessageLimit int) *LLMExtractor {
	if recentMessageLimit <= 0 {
		recentMessageLimit = 10
	}
	return &LLMExtractor{
		generator:          generator,
		now:                time.Now,
		recentMessageLimit: recentMessageLimit,
	}
}

// Extract 从当前 run 对话中提取可跨会话持久化的新增记忆条目。
func (e *LLMExtractor) Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
	decisions, err := e.ExtractDecisions(ctx, messages, nil)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Action == ExtractionActionCreate {
			entries = append(entries, decision.Entry)
		}
	}
	return entries, nil
}

// ExtractDecisions 从当前 run 对话中提取记忆，并结合既有记忆输出新增、合并或跳过决策。
func (e *LLMExtractor) ExtractDecisions(
	ctx context.Context,
	messages []providertypes.Message,
	existing []ExtractionCandidate,
) ([]ExtractionDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.generator == nil {
		return nil, errors.New("memo: text generator is nil")
	}

	runMessages := agentcontext.BuildMemoExtractionMessagesForModel(messages)
	if len(runMessages) == 0 || !containsUserMessage(runMessages) {
		return nil, nil
	}

	response, err := e.generator.Generate(ctx, buildExtractionPrompt(e.now(), existing), runMessages)
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

	decisions := make([]ExtractionDecision, 0, len(extracted))
	for _, item := range extracted {
		decision, ok := toExtractionDecision(item)
		if !ok {
			continue
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

// buildExtractionPrompt 构造记忆提取专用的 system prompt。
func buildExtractionPrompt(now time.Time, existing []ExtractionCandidate) string {
	currentDate := now.In(time.Local).Format("2006-01-02")
	existingJSON := "[]"
	if len(existing) > 0 {
		if data, err := json.Marshal(existing); err == nil {
			existingJSON = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`
你是一个记忆提取助手（memory extraction assistant）。
分析当前 run 对话中值得跨会话持久记住的信息，并结合既有记忆完成语义去重，返回严格 JSON 数组。

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
5. 如果新信息与既有记忆语义相同或只是轻微改写，输出 action="skip"。
6. 如果新信息能补充或修正既有 source="extractor_auto" 的记忆，输出 action="update" 并填写目标 ref。
7. 不允许 update source 不是 "extractor_auto" 的既有记忆；这类相近内容只能 skip。
8. 如果是全新的可持久化信息，输出 action="create"。
9. 输出必须是 JSON 数组，不要输出任何额外解释。

既有记忆候选（JSON）：
%s

输出格式：
[{"action":"create","type":"user","title":"...","content":"...","keywords":["..."]},{"action":"update","ref":"project:p.md","title":"...","content":"...","keywords":["..."]},{"action":"skip","ref":"user:u.md"}]
`, currentDate, existingJSON))
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

// toExtractionDecision 将 LLM 输出收敛为自动提取持久化决策。
func toExtractionDecision(item extractedEntry) (ExtractionDecision, bool) {
	action := parseExtractionAction(item.Action)
	if action == "" {
		return ExtractionDecision{}, false
	}
	if action == ExtractionActionSkip {
		return ExtractionDecision{
			Action: action,
			Ref:    strings.TrimSpace(item.Ref),
		}, true
	}

	if action == ExtractionActionUpdate {
		ref := strings.TrimSpace(item.Ref)
		if ref == "" {
			return ExtractionDecision{}, false
		}
		entry, ok := toMemoUpdateEntry(item)
		if !ok {
			return ExtractionDecision{}, false
		}
		return ExtractionDecision{Action: action, Ref: ref, Entry: entry}, true
	}

	entry, ok := toMemoEntry(item)
	if !ok {
		return ExtractionDecision{}, false
	}
	return ExtractionDecision{Action: action, Entry: entry}, true
}

// toMemoUpdateEntry 将 update 决策中的可变字段收敛为 Entry 片段。
func toMemoUpdateEntry(item extractedEntry) (Entry, bool) {
	title := NormalizeTitle(item.Title)
	content := strings.TrimSpace(item.Content)
	if title == "" || content == "" {
		return Entry{}, false
	}
	return Entry{
		Title:    title,
		Content:  content,
		Keywords: normalizeKeywords(item.Keywords),
		Source:   SourceAutoExtract,
	}, true
}

// parseExtractionAction 解析模型决策动作，并兼容旧格式中缺省 action 的 create 输出。
func parseExtractionAction(action string) ExtractionAction {
	switch ExtractionAction(strings.ToLower(strings.TrimSpace(action))) {
	case "":
		return ExtractionActionCreate
	case ExtractionActionCreate:
		return ExtractionActionCreate
	case ExtractionActionUpdate:
		return ExtractionActionUpdate
	case ExtractionActionSkip:
		return ExtractionActionSkip
	default:
		return ""
	}
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
		return "", ErrExtractionNoJSONArray
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

	return "", ErrExtractionIncompleteJSONArray
}
