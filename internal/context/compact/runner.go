package compact

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

// Mode 表示 compact 执行模式。
type Mode string

const (
	// ModeMicro 只替换较早且较长的 tool 结果，保留最近窗口的细节上下文。
	ModeMicro Mode = "micro"
	// ModeManual 显式触发人工压缩，按策略写入 summary 并重排上下文。
	ModeManual Mode = "manual"
)

// ErrorMode 表示 compact 结果中的错误分类。
type ErrorMode string

const (
	ErrorModeNone ErrorMode = "none"
)

// Input 是一次 compact 执行的输入。
type Input struct {
	Mode      Mode
	SessionID string
	Workdir   string
	Messages  []provider.Message
	Config    config.CompactConfig
}

// Metrics 是 compact 过程的统计信息。
type Metrics struct {
	BeforeChars int     `json:"before_chars"`
	AfterChars  int     `json:"after_chars"`
	SavedRatio  float64 `json:"saved_ratio"`
	TriggerMode string  `json:"trigger_mode"`
}

// Result 是 compact 执行结果。
type Result struct {
	Messages       []provider.Message `json:"messages"`
	Metrics        Metrics            `json:"metrics"`
	TranscriptID   string             `json:"transcript_id"`
	TranscriptPath string             `json:"transcript_path"`
	Applied        bool               `json:"applied"`
	ErrorMode      ErrorMode          `json:"error_mode"`
}

// Runner 定义 compact 执行契约。
type Runner interface {
	Run(ctx context.Context, input Input) (Result, error)
}

// Service 是 compact 模块的默认实现。
type Service struct {
	now         func() time.Time
	randomToken func() (string, error)
	userHomeDir func() (string, error)
	mkdirAll    func(path string, perm os.FileMode) error
	writeFile   func(name string, data []byte, perm os.FileMode) error
	rename      func(oldPath, newPath string) error
	remove      func(path string) error
}

// NewRunner 返回默认 compact 执行器。
func NewRunner() *Service {
	return &Service{
		now:         time.Now,
		randomToken: randomTranscriptToken,
		userHomeDir: os.UserHomeDir,
		mkdirAll:    os.MkdirAll,
		writeFile:   os.WriteFile,
		rename:      os.Rename,
		remove:      os.Remove,
	}
}

// Run 执行 compact 主流程：
// 1. 归一化配置并生成基础结果。
// 2. 校验是否满足模式触发条件。
// 3. 落盘当前会话 transcript，便于追溯。
// 4. 按模式执行压缩并计算统计信息。
func (s *Service) Run(ctx context.Context, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	cfg := normalizeCompactConfig(input.Config)
	messages := cloneMessages(input.Messages)

	beforeChars := countMessageChars(messages)
	base := Result{
		Messages:  messages,
		Applied:   false,
		ErrorMode: ErrorModeNone,
		Metrics: Metrics{
			BeforeChars: beforeChars,
			AfterChars:  beforeChars,
			SavedRatio:  0,
			TriggerMode: string(input.Mode),
		},
	}

	// micro 模式只在开启配置且存在可压缩候选时执行，避免无意义改写。
	switch input.Mode {
	case ModeMicro:
		if !cfg.MicroEnabled || !hasMicroCandidate(messages, cfg) {
			return base, nil
		}
	case ModeManual:
		// manual 模式由用户显式触发，总是进入评估。
	default:
		return Result{}, fmt.Errorf("compact: unsupported mode %q", input.Mode)
	}

	// 先保存原始 transcript，再做内容替换，确保失败时仍可回溯完整上下文。
	transcriptID, transcriptPath, err := s.saveTranscript(messages, strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Workdir))
	if err != nil {
		return Result{}, err
	}
	base.TranscriptID = transcriptID
	base.TranscriptPath = transcriptPath

	var (
		next    []provider.Message
		applied bool
	)

	switch input.Mode {
	case ModeMicro:
		next, applied = microCompact(messages, cfg)
	case ModeManual:
		next, applied, err = manualCompact(messages, cfg)
		if err != nil {
			return Result{}, err
		}
	}

	afterChars := countMessageChars(next)
	result := base
	result.Messages = next
	result.Applied = applied
	result.Metrics.AfterChars = afterChars
	if beforeChars > 0 {
		result.Metrics.SavedRatio = float64(beforeChars-afterChars) / float64(beforeChars)
	}
	return result, nil
}

// hasMicroCandidate 判断是否存在可被 micro 模式替换的旧 tool 输出。
// 规则：仅检查“非最近 N 条 tool 结果”且正文长度达到阈值的候选。
func hasMicroCandidate(messages []provider.Message, cfg config.CompactConfig) bool {
	toolIndices := make([]int, 0, len(messages))
	for i, message := range messages {
		if message.Role == provider.RoleTool {
			toolIndices = append(toolIndices, i)
		}
	}
	if len(toolIndices) <= cfg.ToolResultKeepRecent {
		return false
	}
	candidateCount := len(toolIndices) - cfg.ToolResultKeepRecent
	for i := 0; i < candidateCount; i++ {
		if len(messages[toolIndices[i]].Content) >= cfg.ToolResultPlaceholderMinChars {
			return true
		}
	}
	return false
}

// microCompact 仅对历史较早的长 tool 输出替换为占位摘要，保留最近输出细节。
func microCompact(messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool) {
	next := cloneMessages(messages)

	toolIndices := make([]int, 0, len(next))
	for i, message := range next {
		if message.Role == provider.RoleTool {
			toolIndices = append(toolIndices, i)
		}
	}

	if len(toolIndices) <= cfg.ToolResultKeepRecent {
		return next, false
	}

	toolNameByCallID := buildToolNameIndex(next)
	candidateCount := len(toolIndices) - cfg.ToolResultKeepRecent
	applied := false
	for i := 0; i < candidateCount; i++ {
		idx := toolIndices[i]
		message := next[idx]
		if len(message.Content) < cfg.ToolResultPlaceholderMinChars {
			continue
		}

		toolName := strings.TrimSpace(toolNameByCallID[message.ToolCallID])
		if toolName == "" {
			toolName = "unknown_tool"
		}
		placeholder := fmt.Sprintf("[Previous tool used: %s]", toolName)
		if message.Content == placeholder {
			continue
		}
		message.Content = placeholder
		next[idx] = message
		applied = true
	}

	return next, applied
}

// buildToolNameIndex 从 assistant 的 tool_calls 中提取 call_id -> tool_name 索引。
func buildToolNameIndex(messages []provider.Message) map[string]string {
	index := make(map[string]string)
	for _, message := range messages {
		if message.Role != provider.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		for _, call := range message.ToolCalls {
			id := strings.TrimSpace(call.ID)
			name := strings.TrimSpace(call.Name)
			if id == "" || name == "" {
				continue
			}
			index[id] = name
		}
	}
	return index
}

type span struct {
	start int
	end   int
}

// collectSpans 将消息序列切分为“对话跨度”。
// assistant(tool_calls)+连续 tool_results 会被视为单个跨度，避免打断调用链。
func collectSpans(messages []provider.Message) []span {
	spans := make([]span, 0, len(messages))
	for i := 0; i < len(messages); {
		start := i
		i++

		if messages[start].Role == provider.RoleAssistant && len(messages[start].ToolCalls) > 0 {
			for i < len(messages) && messages[i].Role == provider.RoleTool {
				i++
			}
		}

		spans = append(spans, span{start: start, end: i})
	}
	return spans
}

// manualCompact 按配置策略执行手动压缩。
func manualCompact(messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	strategy := strings.ToLower(strings.TrimSpace(cfg.ManualStrategy))
	switch strategy {
	case config.CompactManualStrategyKeepRecent:
		return manualCompactKeepRecent(messages, cfg)
	case config.CompactManualStrategyFullReplace:
		return manualCompactFullReplace(messages, cfg)
	default:
		return nil, false, fmt.Errorf("compact: manual strategy %q is not supported", cfg.ManualStrategy)
	}
}

// manualCompactKeepRecent 保留最近 N 个跨度，将更早历史折叠为一条 summary。
func manualCompactKeepRecent(messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	spans := collectSpans(messages)
	if len(spans) <= cfg.ManualKeepRecentSpans {
		return cloneMessages(messages), false, nil
	}

	keepStart := spans[len(spans)-cfg.ManualKeepRecentSpans].start
	removed := cloneMessages(messages[:keepStart])
	kept := cloneMessages(messages[keepStart:])

	summary, err := buildSummary(removed, len(spans)-cfg.ManualKeepRecentSpans, cfg)
	if err != nil {
		return nil, false, err
	}

	next := make([]provider.Message, 0, len(kept)+1)
	next = append(next, provider.Message{Role: provider.RoleAssistant, Content: summary})
	next = append(next, kept...)
	return next, true, nil
}

// manualCompactFullReplace 将整段历史替换为单条 summary。
func manualCompactFullReplace(messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	if len(messages) == 0 {
		return nil, false, nil
	}
	spans := collectSpans(messages)
	summary, err := buildSummary(cloneMessages(messages), len(spans), cfg)
	if err != nil {
		return nil, false, err
	}

	return []provider.Message{{Role: provider.RoleAssistant, Content: summary}}, true, nil
}

// buildSummary 生成手动压缩 summary 文本，并保证 done/in_progress 结构有效。
func buildSummary(removed []provider.Message, removedSpans int, cfg config.CompactConfig) (string, error) {
	toolNames := make([]string, 0, 8)
	seenTools := map[string]struct{}{}
	for _, message := range removed {
		for _, call := range message.ToolCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			if _, exists := seenTools[name]; exists {
				continue
			}
			seenTools[name] = struct{}{}
			toolNames = append(toolNames, name)
		}
	}
	toolSummary := "none"
	if len(toolNames) > 0 {
		toolSummary = strings.Join(toolNames, ", ")
	}

	summary := fmt.Sprintf(strings.Join([]string{
		"[compact_summary]",
		"done:",
		"- Archived %d historical spans (%d messages).",
		"",
		"in_progress:",
		"- Continue from the retained recent context window.",
		"",
		"decisions:",
		"- manual_strategy=%s",
		"- manual_keep_recent_spans=%d",
		"",
		"code_changes:",
		"- Older context outside the recent window was replaced by this summary.",
		"- Historical tool calls in archived spans: %s",
		"",
		"constraints:",
		"- Assistant tool_calls and tool_result pairs remain intact in retained spans.",
	}, "\n"), removedSpans, len(removed), cfg.ManualStrategy, cfg.ManualKeepRecentSpans, toolSummary)

	return validateSummary(summary, cfg.MaxSummaryChars)
}

// validateSummary 校验 summary 的最小结构，并在需要时按字符数裁剪。
// maxChars 的语义是“字符数（rune）”，不是字节数。
func validateSummary(summary string, maxChars int) (string, error) {
	summary = strings.TrimSpace(summary)
	if maxChars > 0 {
		runes := []rune(summary)
		if len(runes) > maxChars {
			summary = strings.TrimSpace(string(runes[:maxChars]))
		}
	}

	hasDone := sectionHasContent(summary, "done")
	hasInProgress := sectionHasContent(summary, "in_progress")
	if !hasDone && !hasInProgress {
		return "", errors.New("compact: summary requires done or in_progress content")
	}
	return summary, nil
}

// sectionHasContent 判断目标 section 是否至少包含一条列表项内容。
func sectionHasContent(summary string, section string) bool {
	pattern := fmt.Sprintf(`(?ms)^%s:\s*\n\s*-\s+.+?(\n\w+?:|\z)`, regexp.QuoteMeta(section))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(summary)
}

// transcriptLine 是 transcript JSONL 的单行结构。
type transcriptLine struct {
	Index      int                 `json:"index"`
	Timestamp  string              `json:"timestamp"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	IsError    bool                `json:"is_error,omitempty"`
}

// saveTranscript 将 compact 前消息落盘到项目目录，使用 tmp+rename 保证原子提交。
func (s *Service) saveTranscript(messages []provider.Message, sessionID string, workdir string) (string, string, error) {
	home, err := s.userHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("compact: resolve user home: %w", err)
	}

	projectHash := hashProject(workdir)
	dir := filepath.Join(home, ".neocode", "projects", projectHash, ".transcripts")
	if err := s.mkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("compact: create transcript dir: %w", err)
	}

	sessionID = sanitizeID(sessionID)
	if sessionID == "" {
		sessionID = "draft"
	}
	tokenFn := s.randomToken
	if tokenFn == nil {
		tokenFn = randomTranscriptToken
	}
	randomToken, err := tokenFn()
	if err != nil {
		return "", "", fmt.Errorf("compact: generate transcript token: %w", err)
	}
	transcriptID := fmt.Sprintf("transcript_%d_%s_%s", s.now().UnixNano(), randomToken, sessionID)
	transcriptPath := filepath.Join(dir, transcriptID+".jsonl")
	tmpPath := transcriptPath + ".tmp"

	now := s.now().UTC().Format(time.RFC3339Nano)
	var builder strings.Builder
	for i, message := range messages {
		line := transcriptLine{
			Index:      i,
			Timestamp:  now,
			Role:       message.Role,
			Content:    message.Content,
			ToolCalls:  append([]provider.ToolCall(nil), message.ToolCalls...),
			ToolCallID: message.ToolCallID,
			IsError:    message.IsError,
		}
		payload, err := json.Marshal(line)
		if err != nil {
			return "", "", fmt.Errorf("compact: marshal transcript line: %w", err)
		}
		builder.Write(payload)
		builder.WriteByte('\n')
	}

	if err := s.writeFile(tmpPath, []byte(builder.String()), transcriptFileMode()); err != nil {
		return "", "", fmt.Errorf("compact: write transcript: %w", err)
	}
	if err := s.rename(tmpPath, transcriptPath); err != nil {
		_ = s.remove(tmpPath)
		return "", "", fmt.Errorf("compact: commit transcript: %w", err)
	}

	return transcriptID, transcriptPath, nil
}

// transcriptFileMode 在非 Windows 平台使用 0600，避免 transcript 对其他用户可读。
func transcriptFileMode() os.FileMode {
	if goruntime.GOOS == "windows" {
		return 0o644
	}
	return 0o600
}

// randomTranscriptToken 生成短随机后缀，降低同时间戳命名冲突概率。
func randomTranscriptToken() (string, error) {
	entropy := make([]byte, 4)
	if _, err := cryptorand.Read(entropy); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy), nil
}

// hashProject 将 workdir 映射到固定哈希目录名，避免路径泄漏和非法字符问题。
func hashProject(workdir string) string {
	clean := strings.TrimSpace(filepath.Clean(workdir))
	if clean == "" {
		clean = "unknown"
	}
	sum := sha1.Sum([]byte(strings.ToLower(clean)))
	return hex.EncodeToString(sum[:8])
}

var nonIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeID 清洗 session id，避免生成文件名时出现非法字符。
func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return nonIDChars.ReplaceAllString(value, "_")
}

// cloneMessages 深拷贝消息切片，避免原地改写调用方持有的数据。
func cloneMessages(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]provider.Message, 0, len(messages))
	for _, message := range messages {
		next := message
		next.ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		out = append(out, next)
	}
	return out
}

// countMessageChars 统计消息字符预算（当前按字符串字节长度计算）。
func countMessageChars(messages []provider.Message) int {
	total := 0
	for _, message := range messages {
		total += len(message.Role)
		total += len(message.Content)
		total += len(message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += len(call.ID)
			total += len(call.Name)
			total += len(call.Arguments)
		}
	}
	return total
}

// normalizeCompactConfig 填补 compact 配置默认值并兜底策略字段。
func normalizeCompactConfig(cfg config.CompactConfig) config.CompactConfig {
	defaults := config.Default().Context.Compact
	cfg.ApplyDefaults(defaults)
	if strings.TrimSpace(cfg.ManualStrategy) == "" {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}
	return cfg
}
