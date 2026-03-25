package services

import (
	"context"
	"strings"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/repository"
	"go-llm-demo/internal/server/infra/tools"
	"go-llm-demo/internal/server/service"
)

type Message = domain.Message
type Todo = domain.Todo
type TodoStatus = domain.TodoStatus
type TodoPriority = domain.TodoPriority

const (
	TodoPending    = domain.TodoPending
	TodoInProgress = domain.TodoInProgress
	TodoCompleted  = domain.TodoCompleted

	TodoPriorityHigh   = domain.TodoPriorityHigh
	TodoPriorityMedium = domain.TodoPriorityMedium
	TodoPriorityLow    = domain.TodoPriorityLow
)

var (
	ParseTodoPriority = domain.ParseTodoPriority
)

// ChatClient defines the local interface the TUI depends on.
type ChatClient interface {
	Chat(ctx context.Context, messages []Message, model string) (<-chan string, error)
	GetMemoryStats(ctx context.Context) (*MemoryStats, error)
	GetWorkingSessionSummary(ctx context.Context) (string, error)
	GetProjectMemorySources(ctx context.Context) ([]ProjectMemorySource, error)
	ListMemoryItems(ctx context.Context) ([]MemoryListItem, error)
	Remember(ctx context.Context, text string) error
	MemoryMode(ctx context.Context) string
	ClearMemory(ctx context.Context) error
	ClearSessionMemory(ctx context.Context) error
	GetTodoList(ctx context.Context) ([]Todo, error)
	AddTodo(ctx context.Context, content string, priority TodoPriority) (*Todo, error)
	UpdateTodoStatus(ctx context.Context, id string, status TodoStatus) error
	RemoveTodo(ctx context.Context, id string) error
	DefaultModel() string
}

// WorkingSessionSummaryProvider exposes persisted working session summaries.
type WorkingSessionSummaryProvider interface {
	GetWorkingSessionSummary(ctx context.Context) (string, error)
}

// MemoryStats contains the memory statistics shown in the TUI.
type MemoryStats struct {
	PersistentItems int
	SessionItems    int
	TotalItems      int
	TopK            int
	MinScore        float64
	Path            string
	ByType          map[string]int
	ProjectSources  []string
}

type ProjectMemorySource = domain.ProjectMemorySource

type MemoryListItem struct {
	Type      string
	Scope     string
	Summary   string
	UpdatedAt time.Time
}

type localChatClient struct {
	roleSvc          domain.RoleService
	memorySvc        domain.MemoryService
	workingSvc       domain.WorkingMemoryService
	projectMemorySvc domain.ProjectMemoryService
	todoSvc          domain.TodoService
	config           *configs.AppConfiguration
	memoryMode       string
}

// NewLocalChatClient wires local services for TUI use.
func NewLocalChatClient() (ChatClient, error) {
	cfg := configs.GlobalAppConfig
	if cfg == nil {
		return nil, context.Canceled
	}

	storePath := strings.TrimSpace(cfg.Memory.StoragePath)
	if storePath == "" {
		storePath = "./data/memory_rules.json"
	}
	maxItems := cfg.Memory.MaxItems
	if maxItems <= 0 {
		maxItems = 1000
	}

	workspaceRoot := tools.GetWorkspaceRoot()
	persistentRepo := repository.NewFileMemoryStore(storePath, maxItems)
	sessionRepo := repository.NewSessionMemoryStore(maxItems)

	workingStatePath := ""
	if cfg.History.PersistSessionState {
		workingStatePath = BuildWorkspaceStatePath(cfg.History.WorkspaceStateDir, workspaceRoot)
	}
	workingRepo := repository.NewWorkingMemoryStore(workingStatePath)

	memoryExtractor, err := buildMemoryExtractor(cfg)
	if err != nil {
		return nil, err
	}
	memorySvc := service.NewMemoryServiceWithExtractor(
		persistentRepo,
		sessionRepo,
		memoryExtractor,
		cfg.Memory.TopK,
		cfg.Memory.MinMatchScore,
		cfg.Memory.MaxPromptChars,
		storePath,
		cfg.Memory.PersistTypes,
	)
	workingSvc := service.NewWorkingMemoryService(workingRepo, cfg.History.ShortTermTurns, workspaceRoot)
	projectMemorySvc := service.NewProjectMemoryService(workspaceRoot, cfg.Memory.ProjectFiles, cfg.Memory.ProjectPromptChars)

	roleRepo := repository.NewFileRoleStore("./data/roles.json")
	roleSvc := service.NewRoleService(roleRepo, strings.TrimSpace(cfg.Persona.FilePath))

	todoRepo := repository.NewInMemoryTodoRepository()
	todoSvc := service.NewTodoService(todoRepo)
	tools.GlobalRegistry.Register(tools.NewTodoTool(todoSvc))

	return &localChatClient{
		roleSvc:          roleSvc,
		memorySvc:        memorySvc,
		workingSvc:       workingSvc,
		projectMemorySvc: projectMemorySvc,
		todoSvc:          todoSvc,
		config:           cfg,
		memoryMode:       strings.TrimSpace(cfg.Memory.Extractor),
	}, nil
}

func buildMemoryExtractor(cfg *configs.AppConfiguration) (domain.MemoryExtractor, error) {
	if cfg == nil {
		return service.BuildMemoryExtractor(service.MemoryExtractorModeRule, nil, service.LLMMemoryExtractorOptions{})
	}

	mode := strings.TrimSpace(cfg.Memory.Extractor)
	if mode == "" || strings.EqualFold(mode, service.MemoryExtractorModeRule) {
		return service.BuildMemoryExtractor(service.MemoryExtractorModeRule, nil, service.LLMMemoryExtractorOptions{})
	}

	model := strings.TrimSpace(cfg.Memory.ExtractorModel)
	if model == "" {
		model = strings.TrimSpace(cfg.AI.Model)
	}

	extractorProvider, err := provider.NewChatProvider(model)
	if err != nil {
		if strings.EqualFold(mode, service.MemoryExtractorModeAuto) {
			return service.BuildMemoryExtractor(service.MemoryExtractorModeAuto, nil, service.LLMMemoryExtractorOptions{})
		}
		return nil, err
	}

	return service.BuildMemoryExtractor(mode, extractorProvider, service.LLMMemoryExtractorOptions{
		Timeout: time.Duration(cfg.Memory.ExtractorTimeoutSecond) * time.Second,
	})
}

// Chat sends a chat request through the local services.
func (c *localChatClient) Chat(ctx context.Context, messages []Message, model string) (<-chan string, error) {
	chatProvider, err := provider.NewChatProvider(model)
	if err != nil {
		return nil, err
	}
	chatSvc := service.NewChatService(c.memorySvc, c.workingSvc, c.projectMemorySvc, c.todoSvc, c.roleSvc, chatProvider)
	return chatSvc.Send(ctx, &domain.ChatRequest{Messages: messages, Model: model})
}

// GetMemoryStats returns memory statistics for the TUI.
func (c *localChatClient) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	stats, err := c.memorySvc.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	projectSources := make([]string, 0)
	if c.projectMemorySvc != nil {
		sources, sourceErr := c.projectMemorySvc.ListSources(ctx)
		if sourceErr != nil {
			return nil, sourceErr
		}
		for _, source := range sources {
			projectSources = append(projectSources, source.Path)
		}
	}
	return &MemoryStats{
		PersistentItems: stats.PersistentItems,
		SessionItems:    stats.SessionItems,
		TotalItems:      stats.TotalItems,
		TopK:            stats.TopK,
		MinScore:        stats.MinScore,
		Path:            stats.Path,
		ByType:          stats.ByType,
		ProjectSources:  projectSources,
	}, nil
}

// GetProjectMemorySources returns currently loaded explicit project memory files.
func (c *localChatClient) GetProjectMemorySources(ctx context.Context) ([]ProjectMemorySource, error) {
	if c.projectMemorySvc == nil {
		return nil, nil
	}
	return c.projectMemorySvc.ListSources(ctx)
}

// ListMemoryItems returns memory items for inspection.
func (c *localChatClient) ListMemoryItems(ctx context.Context) ([]MemoryListItem, error) {
	if c.memorySvc == nil {
		return nil, nil
	}

	impl, ok := c.memorySvc.(memoryListProvider)
	if ok {
		return impl.ListMemoryItems(ctx)
	}

	return nil, nil
}

// Remember stores an explicit durable preference or rule from a manual command.
func (c *localChatClient) Remember(ctx context.Context, text string) error {
	if c.memorySvc == nil {
		return nil
	}
	if impl, ok := c.memorySvc.(manualMemoryWriter); ok {
		return impl.SaveManualMemory(ctx, text)
	}
	return c.memorySvc.Save(ctx, strings.TrimSpace(text), "Noted. Remember this for future coding assistance.")
}

// MemoryMode returns the configured memory extraction mode.
func (c *localChatClient) MemoryMode(context.Context) string {
	if strings.TrimSpace(c.memoryMode) == "" {
		return service.MemoryExtractorModeRule
	}
	return c.memoryMode
}

// ClearMemory clears persistent memory.
func (c *localChatClient) ClearMemory(ctx context.Context) error {
	return c.memorySvc.Clear(ctx)
}

// ClearSessionMemory clears session memory and working memory state.
func (c *localChatClient) ClearSessionMemory(ctx context.Context) error {
	if err := c.memorySvc.ClearSession(ctx); err != nil {
		return err
	}
	if c.workingSvc != nil {
		return c.workingSvc.Clear(ctx)
	}
	return nil
}

// GetTodoList returns the current todo list.
func (c *localChatClient) GetTodoList(ctx context.Context) ([]domain.Todo, error) {
	if c.todoSvc == nil {
		return nil, nil
	}
	return c.todoSvc.ListTodos(ctx)
}

// AddTodo adds a new todo item.
func (c *localChatClient) AddTodo(ctx context.Context, content string, priority domain.TodoPriority) (*domain.Todo, error) {
	if c.todoSvc == nil {
		return nil, nil
	}
	return c.todoSvc.AddTodo(ctx, content, priority)
}

// UpdateTodoStatus updates a todo status.
func (c *localChatClient) UpdateTodoStatus(ctx context.Context, id string, status domain.TodoStatus) error {
	if c.todoSvc == nil {
		return nil
	}
	return c.todoSvc.UpdateTodoStatus(ctx, id, status)
}

// RemoveTodo removes a todo item.
func (c *localChatClient) RemoveTodo(ctx context.Context, id string) error {
	if c.todoSvc == nil {
		return nil
	}
	return c.todoSvc.RemoveTodo(ctx, id)
}

// DefaultModel returns the TUI default model.
func (c *localChatClient) DefaultModel() string {
	return provider.DefaultModelForConfig(c.config)
}

// GetWorkingSessionSummary returns the persisted working session summary for the workspace.
func (c *localChatClient) GetWorkingSessionSummary(ctx context.Context) (string, error) {
	if c.workingSvc == nil || c.config == nil || !c.config.History.ResumeLastSession {
		return "", nil
	}
	state, err := c.workingSvc.Get(ctx)
	if err != nil || state == nil {
		return "", err
	}
	return formatWorkingSessionSummary(state), nil
}

func formatWorkingSessionSummary(state *domain.WorkingMemoryState) string {
	if state == nil {
		return ""
	}
	lines := make([]string, 0, 6)
	if strings.TrimSpace(state.CurrentTask) != "" {
		lines = append(lines, "已恢复上次工作现场：")
		lines = append(lines, "- 当前目标: "+domain.SummarizeText(state.CurrentTask, 120))
	}
	if strings.TrimSpace(state.LastCompletedAction) != "" {
		lines = append(lines, "- 上次完成: "+domain.SummarizeText(state.LastCompletedAction, 120))
	}
	if strings.TrimSpace(state.CurrentInProgress) != "" {
		lines = append(lines, "- 当前进行中: "+domain.SummarizeText(state.CurrentInProgress, 120))
	}
	if strings.TrimSpace(state.NextStep) != "" {
		lines = append(lines, "- 下一步: "+domain.SummarizeText(state.NextStep, 120))
	}
	if len(state.RecentFiles) > 0 {
		lines = append(lines, "- 最近文件: "+strings.Join(state.RecentFiles, ", "))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
