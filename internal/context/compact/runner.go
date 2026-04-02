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

type Mode string

const (
	ModeMicro  Mode = "micro"
	ModeManual Mode = "manual"
)

type ErrorMode string

const (
	ErrorModeNone ErrorMode = "none"
)

type Input struct {
	Mode      Mode
	SessionID string
	Workdir   string
	Messages  []provider.Message
	Config    config.CompactConfig
}

type Metrics struct {
	BeforeChars int     `json:"before_chars"`
	AfterChars  int     `json:"after_chars"`
	SavedRatio  float64 `json:"saved_ratio"`
	TriggerMode string  `json:"trigger_mode"`
}

type Result struct {
	Messages       []provider.Message `json:"messages"`
	Metrics        Metrics            `json:"metrics"`
	TranscriptID   string             `json:"transcript_id"`
	TranscriptPath string             `json:"transcript_path"`
	Applied        bool               `json:"applied"`
	ErrorMode      ErrorMode          `json:"error_mode"`
}

type Runner interface {
	Run(ctx context.Context, input Input) (Result, error)
}

type Service struct {
	now         func() time.Time
	randomToken func() (string, error)
	userHomeDir func() (string, error)
	mkdirAll    func(path string, perm os.FileMode) error
	writeFile   func(name string, data []byte, perm os.FileMode) error
	rename      func(oldPath, newPath string) error
	remove      func(path string) error
}

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

func sectionHasContent(summary string, section string) bool {
	pattern := fmt.Sprintf(`(?ms)^%s:\s*\n\s*-\s+.+?(\n\w+?:|\z)`, regexp.QuoteMeta(section))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(summary)
}

type transcriptLine struct {
	Index      int                 `json:"index"`
	Timestamp  string              `json:"timestamp"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	IsError    bool                `json:"is_error,omitempty"`
}

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

func transcriptFileMode() os.FileMode {
	if goruntime.GOOS == "windows" {
		return 0o644
	}
	return 0o600
}

func randomTranscriptToken() (string, error) {
	entropy := make([]byte, 4)
	if _, err := cryptorand.Read(entropy); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy), nil
}

func hashProject(workdir string) string {
	clean := strings.TrimSpace(filepath.Clean(workdir))
	if clean == "" {
		clean = "unknown"
	}
	sum := sha1.Sum([]byte(strings.ToLower(clean)))
	return hex.EncodeToString(sum[:8])
}

var nonIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return nonIDChars.ReplaceAllString(value, "_")
}

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

func normalizeCompactConfig(cfg config.CompactConfig) config.CompactConfig {
	defaults := config.Default().Context.Compact
	cfg.ApplyDefaults(defaults)
	if strings.TrimSpace(cfg.ManualStrategy) == "" {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}
	return cfg
}
