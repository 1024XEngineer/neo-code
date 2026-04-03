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

// Mode 定义 compact 执行模式。
type Mode string

const (
	// ModeMicro：轻量压缩，仅替换较早的大体积 tool 结果内容为占位文本。
	ModeMicro Mode = "micro"
	// ModeManual：手动压缩，按策略进行摘要替换或保留最近窗口。
	ModeManual Mode = "manual"
)

// ErrorMode 描述压缩流程中的降级模式（当前仅保留占位定义）。
type ErrorMode string

const (
	ErrorModeNone ErrorMode = "none"
)

// Input 是 compact 运行输入。
type Input struct {
	Mode      Mode
	SessionID string
	Workdir   string
	Messages  []provider.Message
	Config    config.CompactConfig
}

// Metrics 记录压缩前后体量变化与触发方式。
type Metrics struct {
	BeforeChars int     `json:"before_chars"`
	AfterChars  int     `json:"after_chars"`
	SavedRatio  float64 `json:"saved_ratio"`
	TriggerMode string  `json:"trigger_mode"`
}

// Result 是 compact 运行结果。
type Result struct {
	Messages       []provider.Message `json:"messages"`
	Metrics        Metrics            `json:"metrics"`
	TranscriptID   string             `json:"transcript_id"`
	TranscriptPath string             `json:"transcript_path"`
	Applied        bool               `json:"applied"`
	ErrorMode      ErrorMode          `json:"error_mode"`
}

// Runner 抽象 compact 执行入口。
type Runner interface {
	Run(ctx context.Context, input Input) (Result, error)
}

// Service 提供可注入依赖，便于测试控制时间、随机数与文件系统副作用。
type Service struct {
	now         func() time.Time
	randomToken func() (string, error)
	userHomeDir func() (string, error)
	mkdirAll    func(path string, perm os.FileMode) error
	writeFile   func(name string, data []byte, perm os.FileMode) error
	rename      func(oldPath, newPath string) error
	remove      func(path string) error
}

// NewRunner 创建默认 compact 执行器。
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

// Run 执行一次 compact：
// 1) 标准化配置并计算基线指标；
// 2) 判断模式与触发条件；
// 3) 先落盘完整转录，再对消息做压缩；
// 4) 返回新消息和压缩收益指标。
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

	// micro 模式需要满足“启用 + 存在可替换候选”才会生效。
	switch input.Mode {
	case ModeMicro:
		if !cfg.MicroEnabled || !hasMicroCandidate(messages, cfg) {
			return base, nil
		}
	case ModeManual:
		// manual compact is always evaluated when explicitly requested.
	default:
		return Result{}, fmt.Errorf("compact: unsupported mode %q", input.Mode)
	}

	// 无论后续是否实际应用压缩，都先保存转录，保证可追溯。
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

	// 根据模式分派具体压缩策略。
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

// hasMicroCandidate 判断是否存在可被 micro 替换的早期 tool 结果。
// 最近 N 条 tool 结果永远保留原文，避免影响当前推理连续性。
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

// microCompact 将较早且足够长的 tool 输出替换为占位文本。
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
		// 占位文本保留“调用过哪个工具”的关键信息，压缩体量同时保留语义线索。
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

// buildToolNameIndex 将 tool_call_id 映射到工具名，供 micro 占位文本使用。
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

// collectSpans 按“assistant(tool_calls)+tool_results”聚合消息片段。
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

// manualCompact 按配置的手动策略进行压缩。
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

// manualCompactKeepRecent 保留最近若干片段，旧片段汇总为一条 assistant 摘要消息。
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

// manualCompactFullReplace 将全部历史替换为一条摘要消息。
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

// buildSummary 生成结构化摘要，至少包含 done 或 in_progress 段落。
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

// validateSummary 做摘要最小合法性校验，并施加最大长度约束。
func validateSummary(summary string, maxChars int) (string, error) {
	summary = strings.TrimSpace(summary)
	if maxChars > 0 && len(summary) > maxChars {
		summary = strings.TrimSpace(summary[:maxChars])
	}

	hasDone := sectionHasContent(summary, "done")
	hasInProgress := sectionHasContent(summary, "in_progress")
	if !hasDone && !hasInProgress {
		return "", errors.New("compact: summary requires done or in_progress content")
	}
	return summary, nil
}

// sectionHasContent 使用正则判断某个段落是否存在项目符号内容。
func sectionHasContent(summary string, section string) bool {
	pattern := fmt.Sprintf(`(?ms)^%s:\s*\n\s*-\s+.+?(\n\w+?:|\z)`, regexp.QuoteMeta(section))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(summary)
}

// transcriptLine 是写入 jsonl 转录文件的行结构。
type transcriptLine struct {
	Index      int                 `json:"index"`
	Timestamp  string              `json:"timestamp"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	IsError    bool                `json:"is_error,omitempty"`
}

// saveTranscript 以原子写入方式保存压缩前消息转录：
// 先写 .tmp，再 rename，避免中断导致半文件状态。
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

// transcriptFileMode 在非 Windows 上使用更严格权限保护转录内容。
func transcriptFileMode() os.FileMode {
	if goruntime.GOOS == "windows" {
		return 0o644
	}
	return 0o600
}

// randomTranscriptToken 生成短随机 token，用于转录文件名去重。
func randomTranscriptToken() (string, error) {
	entropy := make([]byte, 4)
	if _, err := cryptorand.Read(entropy); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy), nil
}

// hashProject 基于工作目录计算稳定哈希，用于项目级转录目录隔离。
func hashProject(workdir string) string {
	clean := strings.TrimSpace(filepath.Clean(workdir))
	if clean == "" {
		clean = "unknown"
	}
	sum := sha1.Sum([]byte(strings.ToLower(clean)))
	return hex.EncodeToString(sum[:8])
}

var nonIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeID 清理 session id，确保可安全用于文件名。
func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return nonIDChars.ReplaceAllString(value, "_")
}

// cloneMessages 深拷贝消息切片，避免压缩过程污染调用方原始数据。
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

// countMessageChars 估算消息体量，作为压缩收益指标。
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

// normalizeCompactConfig 将 compact 配置补齐默认值并规范关键字段。
func normalizeCompactConfig(cfg config.CompactConfig) config.CompactConfig {
	defaults := config.Default().Context.Compact
	cfg.ApplyDefaults(defaults)
	if strings.TrimSpace(cfg.ManualStrategy) == "" {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}
	return cfg
}
