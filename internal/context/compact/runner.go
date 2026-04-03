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
	"unicode/utf8"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

// Mode identifies the compact execution mode.
type Mode string

const (
	// ModeManual runs the explicit user-triggered compact flow.
	ModeManual Mode = "manual"
)

// ErrorMode classifies compact result errors.
type ErrorMode string

const (
	ErrorModeNone ErrorMode = "none"
)

// Input is a single compact execution request.
type Input struct {
	Mode      Mode
	SessionID string
	Workdir   string
	Messages  []provider.Message
	Config    config.CompactConfig
}

// SummaryInput describes the historical context that must be summarized.
type SummaryInput struct {
	Mode             Mode
	ArchivedMessages []provider.Message
	RetainedMessages []provider.Message
	RemovedSpans     int
	Config           config.CompactConfig
}

// Metrics reports compact input/output size changes.
type Metrics struct {
	BeforeChars int     `json:"before_chars"`
	AfterChars  int     `json:"after_chars"`
	SavedRatio  float64 `json:"saved_ratio"`
	TriggerMode string  `json:"trigger_mode"`
}

// Result is the compact execution result.
type Result struct {
	Messages       []provider.Message `json:"messages"`
	Metrics        Metrics            `json:"metrics"`
	TranscriptID   string             `json:"transcript_id"`
	TranscriptPath string             `json:"transcript_path"`
	Applied        bool               `json:"applied"`
	ErrorMode      ErrorMode          `json:"error_mode"`
}

// SummaryGenerator produces the semantic compact summary.
type SummaryGenerator interface {
	Generate(ctx context.Context, input SummaryInput) (string, error)
}

// Runner defines the compact execution contract.
type Runner interface {
	Run(ctx context.Context, input Input) (Result, error)
}

// Service is the default compact implementation.
type Service struct {
	generator   SummaryGenerator
	now         func() time.Time
	randomToken func() (string, error)
	userHomeDir func() (string, error)
	mkdirAll    func(path string, perm os.FileMode) error
	writeFile   func(name string, data []byte, perm os.FileMode) error
	rename      func(oldPath, newPath string) error
	remove      func(path string) error
}

// NewRunner returns the default compact runner.
func NewRunner(generator SummaryGenerator) *Service {
	return &Service{
		generator:   generator,
		now:         time.Now,
		randomToken: randomTranscriptToken,
		userHomeDir: os.UserHomeDir,
		mkdirAll:    os.MkdirAll,
		writeFile:   os.WriteFile,
		rename:      os.Rename,
		remove:      os.Remove,
	}
}

// Run executes manual compact and persists the original transcript first.
func (s *Service) Run(ctx context.Context, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if input.Mode != ModeManual {
		return Result{}, fmt.Errorf("compact: unsupported mode %q", input.Mode)
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

	transcriptID, transcriptPath, err := s.saveTranscript(messages, strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Workdir))
	if err != nil {
		return Result{}, err
	}
	base.TranscriptID = transcriptID
	base.TranscriptPath = transcriptPath

	next, applied, err := s.manualCompact(ctx, messages, cfg)
	if err != nil {
		return Result{}, err
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

type span struct {
	start int
	end   int
}

// collectSpans groups assistant tool_calls + following tool results as one span.
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

func (s *Service) manualCompact(ctx context.Context, messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	strategy := strings.ToLower(strings.TrimSpace(cfg.ManualStrategy))
	switch strategy {
	case config.CompactManualStrategyKeepRecent:
		return s.manualCompactKeepRecent(ctx, messages, cfg)
	case config.CompactManualStrategyFullReplace:
		return s.manualCompactFullReplace(ctx, messages, cfg)
	default:
		return nil, false, fmt.Errorf("compact: manual strategy %q is not supported", cfg.ManualStrategy)
	}
}

func (s *Service) manualCompactKeepRecent(ctx context.Context, messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	spans := collectSpans(messages)
	if len(spans) <= cfg.ManualKeepRecentSpans {
		return cloneMessages(messages), false, nil
	}

	keepStart := spans[len(spans)-cfg.ManualKeepRecentSpans].start
	removed := cloneMessages(messages[:keepStart])
	kept := cloneMessages(messages[keepStart:])

	summary, err := s.buildSummary(ctx, removed, kept, len(spans)-cfg.ManualKeepRecentSpans, cfg)
	if err != nil {
		return nil, false, err
	}

	next := make([]provider.Message, 0, len(kept)+1)
	next = append(next, provider.Message{Role: provider.RoleAssistant, Content: summary})
	next = append(next, kept...)
	return next, true, nil
}

func (s *Service) manualCompactFullReplace(ctx context.Context, messages []provider.Message, cfg config.CompactConfig) ([]provider.Message, bool, error) {
	if len(messages) == 0 {
		return nil, false, nil
	}
	spans := collectSpans(messages)
	summary, err := s.buildSummary(ctx, cloneMessages(messages), nil, len(spans), cfg)
	if err != nil {
		return nil, false, err
	}

	return []provider.Message{{Role: provider.RoleAssistant, Content: summary}}, true, nil
}

func (s *Service) buildSummary(
	ctx context.Context,
	archived []provider.Message,
	retained []provider.Message,
	removedSpans int,
	cfg config.CompactConfig,
) (string, error) {
	if s.generator == nil {
		return "", errors.New("compact: summary generator is nil")
	}

	summary, err := s.generator.Generate(ctx, SummaryInput{
		Mode:             ModeManual,
		ArchivedMessages: cloneMessages(archived),
		RetainedMessages: cloneMessages(retained),
		RemovedSpans:     removedSpans,
		Config:           cfg,
	})
	if err != nil {
		return "", err
	}

	return validateSummary(summary, cfg.MaxSummaryChars)
}

func validateSummary(summary string, maxChars int) (string, error) {
	summary = normalizeSummary(summary)
	if maxChars > 0 {
		runes := []rune(summary)
		if len(runes) > maxChars {
			summary = normalizeSummary(string(runes[:maxChars]))
		}
	}

	if err := validateSummaryStructure(summary); err != nil {
		return "", err
	}
	return summary, nil
}

var summarySections = []string{
	"done",
	"in_progress",
	"decisions",
	"code_changes",
	"constraints",
}

func normalizeSummary(summary string) string {
	summary = strings.ReplaceAll(summary, "\r\n", "\n")
	return strings.TrimSpace(summary)
}

func validateSummaryStructure(summary string) error {
	summary = normalizeSummary(summary)
	if summary == "" {
		return errors.New("compact: summary is empty")
	}

	lines := strings.Split(summary, "\n")
	index := nextNonEmptyLine(lines, 0)
	if index >= len(lines) || strings.TrimSpace(lines[index]) != "[compact_summary]" {
		return errors.New("compact: summary must start with [compact_summary]")
	}
	index++

	for _, section := range summarySections {
		index = nextNonEmptyLine(lines, index)
		if index >= len(lines) || strings.TrimSpace(lines[index]) != section+":" {
			return fmt.Errorf("compact: summary missing required section %q", section)
		}
		index++

		bullets := 0
		for index < len(lines) {
			rawLine := strings.TrimRight(lines[index], " \t")
			line := strings.TrimSpace(rawLine)
			bulletLine := strings.TrimLeft(rawLine, " \t")
			switch {
			case line == "":
				index++
			case isSummarySectionHeader(line):
				if bullets == 0 {
					return fmt.Errorf("compact: summary section %q requires at least one bullet", section)
				}
				goto nextSection
			case bulletLine == "-":
				return fmt.Errorf("compact: summary section %q contains an empty bullet", section)
			case !strings.HasPrefix(bulletLine, "- "):
				return fmt.Errorf("compact: summary section %q contains invalid line %q", section, line)
			case strings.TrimSpace(strings.TrimPrefix(bulletLine, "- ")) == "":
				return fmt.Errorf("compact: summary section %q contains an empty bullet", section)
			default:
				bullets++
				index++
			}
		}
		if bullets == 0 {
			return fmt.Errorf("compact: summary section %q requires at least one bullet", section)
		}

	nextSection:
	}

	index = nextNonEmptyLine(lines, index)
	if index < len(lines) {
		return fmt.Errorf("compact: summary contains unexpected trailing content %q", strings.TrimSpace(lines[index]))
	}
	return nil
}

func nextNonEmptyLine(lines []string, start int) int {
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	return start
}

func isSummarySectionHeader(line string) bool {
	for _, section := range summarySections {
		if line == section+":" {
			return true
		}
	}
	return false
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
		total += utf8.RuneCountInString(message.Role)
		total += utf8.RuneCountInString(message.Content)
		total += utf8.RuneCountInString(message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += utf8.RuneCountInString(call.ID)
			total += utf8.RuneCountInString(call.Name)
			total += utf8.RuneCountInString(call.Arguments)
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
