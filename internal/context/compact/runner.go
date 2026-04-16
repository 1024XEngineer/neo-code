package compact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// Mode identifies the compact execution mode.
type Mode string

const (
	// ModeManual runs the explicit user-triggered compact flow.
	ModeManual Mode = "manual"
	// ModeAuto runs the token-threshold-triggered compact flow.
	ModeAuto Mode = "auto"
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
	Mode               Mode
	SessionID          string
	Workdir            string
	Messages           []providertypes.Message
	TaskState          agentsession.TaskState
	Config             config.CompactConfig
	SessionInputTokens int
}

// SummaryInput describes the historical context that must be summarized.
type SummaryInput struct {
	Mode                 Mode
	CurrentTaskState     agentsession.TaskState
	ArchivedMessages     []providertypes.Message
	RetainedMessages     []providertypes.Message
	ArchivedMessageCount int
	Config               config.CompactConfig
}

// SummaryOutput 描述 compact 生成器返回的任务状态和展示摘要。
type SummaryOutput struct {
	TaskState      agentsession.TaskState
	DisplaySummary string
}

// Metrics reports compact input/output size changes.
type Metrics struct {
	BeforeChars  int     `json:"before_chars"`
	AfterChars   int     `json:"after_chars"`
	BeforeTokens int     `json:"before_tokens,omitempty"`
	SavedRatio   float64 `json:"saved_ratio"`
	TriggerMode  string  `json:"trigger_mode"`
}

// Result is the compact execution result.
type Result struct {
	Messages       []providertypes.Message `json:"messages"`
	TaskState      agentsession.TaskState  `json:"task_state"`
	Metrics        Metrics                 `json:"metrics"`
	TranscriptID   string                  `json:"transcript_id"`
	TranscriptPath string                  `json:"transcript_path"`
	Applied        bool                    `json:"applied"`
	ErrorMode      ErrorMode               `json:"error_mode"`
}

// SummaryGenerator produces the semantic compact summary.
type SummaryGenerator interface {
	Generate(ctx context.Context, input SummaryInput) (SummaryOutput, error)
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
	readDir         func(dir string) ([]os.DirEntry, error)
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
		readDir:         os.ReadDir,
		planner:         compactionPlanner{},
		summaryVerifier: compactSummaryValidator{},
	}
}

// Run 执行 compact 流程，并在压缩前优先持久化原始 transcript。
func (s *Service) Run(ctx context.Context, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if input.Mode != ModeManual && input.Mode != ModeAuto && input.Mode != ModeReactive {
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
		TaskState: agentsession.NormalizeTaskState(input.TaskState),
		Applied:   false,
		ErrorMode: ErrorModeNone,
		Metrics: Metrics{
			BeforeChars:  beforeChars,
			AfterChars:   beforeChars,
			BeforeTokens: input.SessionInputTokens,
			SavedRatio:   0,
			TriggerMode:  string(input.Mode),
		},
	}

	store := s.transcriptStore()
	transcriptID, transcriptPath, err := store.Save(messages, strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Workdir))
	if err != nil {
		return Result{}, err
	}
	base.TranscriptID = transcriptID
	base.TranscriptPath = transcriptPath

	// 清理过期的 transcript 文件，忽略错误以不影响主流程
	_ = store.Cleanup(strings.TrimSpace(input.Workdir), 0)

	plan, err := s.planner.Plan(input.Mode, messages, cfg)
	if err != nil {
		return Result{}, err
	}
	plan = sanitizeCompactionPlan(plan)
	if !plan.Applied {
		return base, nil
	}

	output, err := s.buildSummary(ctx, input.Mode, input.TaskState, plan, cfg)
	if err != nil {
		return Result{}, err
	}
	output.TaskState = agentsession.NormalizeTaskState(output.TaskState)
	output.TaskState.LastUpdatedAt = s.now()

	next := make([]providertypes.Message, 0, len(plan.Retained)+1)
	next = append(next, providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart(output.DisplaySummary)}})
	next = append(next, plan.Retained...)

	afterChars := countMessageChars(next)
	result := base
	result.Messages = next
	result.TaskState = output.TaskState
	result.Applied = true
	result.Metrics.AfterChars = afterChars
	if beforeChars > 0 {
		result.Metrics.SavedRatio = float64(beforeChars-afterChars) / float64(beforeChars)
	}
	return result, nil
}

// buildSummary 调用摘要生成器并委托校验器收敛最终摘要内容。
func (s *Service) buildSummary(
	ctx context.Context,
	mode Mode,
	currentTaskState agentsession.TaskState,
	plan compactionPlan,
	cfg config.CompactConfig,
) (SummaryOutput, error) {
	if s.generator == nil {
		return SummaryOutput{}, errors.New("compact: summary generator is nil")
	}

	output, err := s.generator.Generate(ctx, SummaryInput{
		Mode:                 mode,
		CurrentTaskState:     currentTaskState.Clone(),
		ArchivedMessages:     cloneMessages(plan.Archived),
		RetainedMessages:     cloneMessages(plan.Retained),
		ArchivedMessageCount: plan.ArchivedMessageCount,
		Config:               cfg,
	})
	if err != nil {
		return SummaryOutput{}, err
	}

	output.TaskState = agentsession.NormalizeTaskState(output.TaskState)
	if err := validateGeneratedTaskState(output.TaskState); err != nil {
		return SummaryOutput{}, err
	}

	output.DisplaySummary, err = s.summaryVerifier.Validate(output.DisplaySummary, cfg.MaxSummaryChars)
	if err != nil {
		return SummaryOutput{}, err
	}
	return output, nil
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
		readDir:     s.readDir,
	}
}

// normalizeCompactConfig 在 compact 执行前补齐缺失配置并收敛默认策略。
func normalizeCompactConfig(cfg config.CompactConfig) config.CompactConfig {
	defaults := config.StaticDefaults().Context.Compact
	cfg.ApplyDefaults(defaults)
	if strings.TrimSpace(cfg.ManualStrategy) == "" {
		cfg.ManualStrategy = config.CompactManualStrategyKeepRecent
	}
	return cfg
}

// sanitizeCompactionPlan 在真正生成 compact 前移除旧的展示摘要，避免摘要的摘要。
func sanitizeCompactionPlan(plan compactionPlan) compactionPlan {
	plan.Archived = filterCompactSummaryMessages(plan.Archived)
	plan.Retained = filterCompactSummaryMessages(plan.Retained)
	plan.ArchivedMessageCount = len(plan.Archived)
	plan.Applied = len(plan.Archived) > 0
	return plan
}

// filterCompactSummaryMessages 过滤历史中的 compact 展示摘要，防止它们再次参与状态生成。
func filterCompactSummaryMessages(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}

	filtered := make([]providertypes.Message, 0, len(messages))
	for _, message := range messages {
		if isCompactSummaryMessage(message) {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

// isCompactSummaryMessage 判断一条消息是否为 compact 展示摘要。
func isCompactSummaryMessage(message providertypes.Message) bool {
	if message.Role != providertypes.RoleAssistant {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(renderTranscriptParts(message.Parts)), "[compact_summary]")
}

// validateGeneratedTaskState 确保 compact 生成结果真正建立了 durable task state，避免“摘要成功但状态为空”。
func validateGeneratedTaskState(state agentsession.TaskState) error {
	if !state.Established() {
		return errors.New("compact: generated task_state is empty")
	}
	return nil
}
