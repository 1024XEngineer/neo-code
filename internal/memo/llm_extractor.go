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
	// ErrExtractionNoJSONArray 表示提取结果中找不到合法 JSON 数组或对象起始。
	ErrExtractionNoJSONArray = errors.New("memo: extraction response does not contain a JSON payload")
	// ErrExtractionIncompleteJSONArray 表示提取结果中的 JSON 数组或对象不完整。
	ErrExtractionIncompleteJSONArray = errors.New("memo: extraction response contains an incomplete JSON payload")
)

// LLMExtractor 基于 LLM 分析当前 run 对话，并输出结构化记忆或去重决策。
type LLMExtractor struct {
	generator TextGenerator
	now       func() time.Time
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
func NewLLMExtractor(generator TextGenerator) *LLMExtractor {
	return &LLMExtractor{
		generator: generator,
		now:       time.Now,
	}
}

// Extract 从当前 run 对话中提取可跨会话持久化的新增记忆条目。
func (e *LLMExtractor) Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
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

	response, err := e.generator.Generate(ctx, buildExtractionPrompt(e.now()), runMessages)
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

// ResolveDecision 结合单条候选记忆与 shortlist，解析 create/update/skip 决策。
func (e *LLMExtractor) ResolveDecision(
	ctx context.Context,
	candidate Entry,
	existing []ExtractionCandidate,
) (ExtractionDecision, error) {
	if err := ctx.Err(); err != nil {
		return ExtractionDecision{}, err
	}
	if e == nil || e.generator == nil {
		return ExtractionDecision{}, errors.New("memo: text generator is nil")
	}

	response, err := e.generator.Generate(ctx, buildResolutionPrompt(e.now(), candidate, existing), buildResolutionMessages(candidate))
	if err != nil {
		return ExtractionDecision{}, err
	}
	if err := ctx.Err(); err != nil {
		return ExtractionDecision{}, err
	}

	jsonText, err := extractJSONObject(response)
	if err != nil {
		return ExtractionDecision{}, err
	}

	var extracted extractedEntry
	if err := json.Unmarshal([]byte(jsonText), &extracted); err != nil {
		return ExtractionDecision{}, fmt.Errorf("memo: parse resolution response: %w", err)
	}
	decision, ok := toExtractionDecision(extracted)
	if !ok {
		return ExtractionDecision{}, errors.New("memo: resolution response is invalid")
	}
	return decision, nil
}

// buildExtractionPrompt 构造仅负责“当前 run 提取”的 system prompt。
func buildExtractionPrompt(now time.Time) string {
	currentDate := now.In(time.Local).Format("2006-01-02")
	return strings.TrimSpace(fmt.Sprintf(`
你是一个记忆提取助手（memory extraction assistant）。
分析当前 run 对话中值得跨会话持久记住的信息，并返回严格 JSON 数组。

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

// buildResolutionPrompt 构造单条候选记忆的去重决策提示。
func buildResolutionPrompt(now time.Time, candidate Entry, existing []ExtractionCandidate) string {
	currentDate := now.In(time.Local).Format("2006-01-02")
	candidateJSON := marshalPromptJSON(struct {
		Type     Type     `json:"type"`
		Title    string   `json:"title"`
		Content  string   `json:"content"`
		Keywords []string `json:"keywords,omitempty"`
	}{
		Type:     candidate.Type,
		Title:    candidate.Title,
		Content:  candidate.Content,
		Keywords: candidate.Keywords,
	})
	existingJSON := marshalPromptJSON(existing)
	return strings.TrimSpace(fmt.Sprintf(`
你是一个记忆去重决策助手（memory dedupe assistant）。
分析一条新的候选记忆与已有记忆 shortlist，返回严格 JSON 对象。

当前本地日期：%s
如果候选内容中的日期是相对日期，先换算成绝对日期再判断。

规则：
1. 若候选记忆与 shortlist 中某条记忆语义相同，返回 {"action":"skip","ref":"..."}。
2. 若候选记忆补充或修正了某条 source="extractor_auto" 的已有记忆，返回 {"action":"update","ref":"...","title":"...","content":"...","keywords":[...]}。
3. 不允许更新 source 不是 "extractor_auto" 的已有记忆；遇到这类情况只能返回 skip。
4. 若 shortlist 中没有合适对象，返回 {"action":"create","type":"...","title":"...","content":"...","keywords":[...]}。
5. 输出必须是单个 JSON 对象，不要输出任何额外解释。

候选记忆（JSON）：
%s

已有记忆 shortlist（JSON）：
%s
`, currentDate, candidateJSON, existingJSON))
}

// buildResolutionMessages 为去重决策构造最小 provider 输入，避免空消息列表触发兼容问题。
func buildResolutionMessages(candidate Entry) []providertypes.Message {
	text := fmt.Sprintf("请为这条候选记忆返回 create、update 或 skip 决策：%s", candidate.Title)
	return []providertypes.Message{
		{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(text)},
		},
	}
}

// marshalPromptJSON 将结构化数据压缩为 prompt 内联 JSON。
func marshalPromptJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
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
		ref := strings.TrimSpace(item.Ref)
		if ref == "" {
			return ExtractionDecision{}, false
		}
		return ExtractionDecision{
			Action: action,
			Ref:    ref,
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

// extractJSONObject 从模型返回文本中提取最外层 JSON 对象。
func extractJSONObject(text string) (string, error) {
	start := strings.Index(text, "{")
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
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : index+1]), nil
			}
		}
	}

	return "", ErrExtractionIncompleteJSONArray
}
