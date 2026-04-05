package compact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

// Mode identifies the compact execution mode.
type Mode string

const (
	// ModeManual runs the explicit user-triggered compact flow.
	ModeManual Mode = "manual"
	// ModeReactive runs the provider-error-triggered compact flow.
	ModeReactive Mode = "reactive"
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
	Mode                 Mode
	ArchivedMessages     []provider.Message
	RetainedMessages     []provider.Message
	ArchivedMessageCount int
	Config               config.CompactConfig
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
	generator       SummaryGenerator
	now             func() time.Time
	randomToken     func() (string, error)
	userHomeDir     func() (string, error)
	mkdirAll        func(path string, perm os.FileMode) error
	writeFile       func(name string, data []byte, perm os.FileMode) error
	rename          func(oldPath, newPath string) error
	remove          func(path string) error
	planner         compactionPlanner
	summaryVerifier compactSummaryValidator
}

// NewRunner returns the default compact runner.
func NewRunner(generator SummaryGenerator) *Service {
	return &Service{
		generator:       generator,
		now:             time.Now,
		randomToken:     randomTranscriptToken,
		userHomeDir:     os.UserHomeDir,
		mkdirAll:        os.MkdirAll,
		writeFile:       os.WriteFile,
		rename:          os.Rename,
		remove:          os.Remove,
		planner:         compactionPlanner{},
		summaryVerifier: compactSummaryValidator{},
	}
}

// Run 执行 compact 流程，并在压缩前优先持久化原始 transcript。
func (s *Service) Run(ctx context.Context, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if input.Mode != ModeManual && input.Mode != ModeReactive {
		return Result{}, fmt.Errorf("compact: unsupported mode %q", input.Mode)
	}

	cfg := normalizeCompactConfig(input.Config)
	if input.Mode == ModeReactive {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}

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

	store := s.transcriptStore()
	transcriptID, transcriptPath, err := store.Save(messages, strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Workdir))
	if err != nil {
		return Result{}, err
	}
	base.TranscriptID = transcriptID
	base.TranscriptPath = transcriptPath

	plan, err := s.planner.Plan(input.Mode, messages, cfg)
	if err != nil {
		return Result{}, err
	}
	if !plan.Applied {
		return base, nil
	}

	summary, err := s.buildSummary(ctx, input.Mode, plan, cfg)
	if err != nil {
		return Result{}, err
	}

	next := make([]provider.Message, 0, len(plan.Retained)+1)
	next = append(next, provider.Message{Role: provider.RoleAssistant, Content: summary})
	next = append(next, plan.Retained...)

	afterChars := countMessageChars(next)
	result := base
	result.Messages = next
	result.Applied = true
	result.Metrics.AfterChars = afterChars
	if beforeChars > 0 {
		result.Metrics.SavedRatio = float64(beforeChars-afterChars) / float64(beforeChars)
	}
	return result, nil
}

// buildSummary 调用摘要生成器并委托校验器收敛最终摘要内容。
func (s *Service) buildSummary(ctx context.Context, mode Mode, plan compactionPlan, cfg config.CompactConfig) (string, error) {
	if s.generator == nil {
		return "", errors.New("compact: summary generator is nil")
	}

	summary, err := s.generator.Generate(ctx, SummaryInput{
		Mode:                 mode,
		ArchivedMessages:     cloneMessages(plan.Archived),
		RetainedMessages:     cloneMessages(plan.Retained),
		ArchivedMessageCount: plan.ArchivedMessageCount,
		Config:               cfg,
	})
	if err != nil {
		return "", err
	}

	return s.summaryVerifier.Validate(summary, cfg.MaxSummaryChars)
}

// transcriptStore 基于 Service 当前依赖构造 transcript 持久化服务。
func (s *Service) transcriptStore() transcriptStore {
	return transcriptStore{
		now:         s.now,
		randomToken: s.randomToken,
		userHomeDir: s.userHomeDir,
		mkdirAll:    s.mkdirAll,
		writeFile:   s.writeFile,
		rename:      s.rename,
		remove:      s.remove,
	}
}

// normalizeCompactConfig 在 compact 执行前补齐缺失配置并收敛默认策略。
func normalizeCompactConfig(cfg config.CompactConfig) config.CompactConfig {
	defaults := config.Default().Context.Compact
	cfg.ApplyDefaults(defaults)
	if strings.TrimSpace(cfg.ManualStrategy) == "" {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}
	return cfg
}
