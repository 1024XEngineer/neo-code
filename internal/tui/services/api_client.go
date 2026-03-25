package services

import (
	"context"
	"strings"

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

// ChatClient 定义 TUI 侧依赖的最小聊天与记忆接口。
type ChatClient interface {
	Chat(ctx context.Context, messages []Message, model string) (<-chan string, error)
	GetMemoryStats(ctx context.Context) (*MemoryStats, error)
	ClearMemory(ctx context.Context) error
	ClearSessionMemory(ctx context.Context) error
	GetTodoList(ctx context.Context) ([]Todo, error)
	AddTodo(ctx context.Context, content string, priority TodoPriority) (*Todo, error)
	UpdateTodoStatus(ctx context.Context, id string, status TodoStatus) error
	RemoveTodo(ctx context.Context, id string) error
	DefaultModel() string
}

// WorkingSessionSummaryProvider 为支持恢复摘要的客户端扩展接口。
type WorkingSessionSummaryProvider interface {
	GetWorkingSessionSummary(ctx context.Context) (string, error)
}

// MemoryStats 是 TUI 展示 memory 面板所需的聚合统计信息。
type MemoryStats struct {
	PersistentItems int
	SessionItems    int
	TotalItems      int
	TopK            int
	MinScore        float64
	Path            string
	ByType          map[string]int
}

type localChatClient struct {
	roleSvc    domain.RoleService
	memorySvc  domain.MemoryService
	workingSvc domain.WorkingMemoryService
	todoSvc    domain.TodoService
	config     *configs.AppConfiguration
}

// NewLocalChatClient 将本地服务组装为 TUI 使用的聊天客户端。
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
	memorySvc := service.NewMemoryService(
		persistentRepo,
		sessionRepo,
		cfg.Memory.TopK,
		cfg.Memory.MinMatchScore,
		cfg.Memory.MaxPromptChars,
		storePath,
		cfg.Memory.PersistTypes,
	)
	workingSvc := service.NewWorkingMemoryService(workingRepo, cfg.History.ShortTermTurns, workspaceRoot)

	roleRepo := repository.NewFileRoleStore("./data/roles.json")
	roleSvc := service.NewRoleService(roleRepo, strings.TrimSpace(cfg.Persona.FilePath))

	todoRepo := repository.NewInMemoryTodoRepository()
	todoSvc := service.NewTodoService(todoRepo)
	tools.GlobalRegistry.Register(tools.NewTodoTool(todoSvc))

	// 当前仍以内进程方式组装服务，后续替换为真实 transport 时可继续复用该接口。
	return &localChatClient{
		roleSvc:    roleSvc,
		memorySvc:  memorySvc,
		workingSvc: workingSvc,
		todoSvc:    todoSvc,
		config:     cfg,
	}, nil
}

// Chat 通过本地聊天服务发送消息。
func (c *localChatClient) Chat(ctx context.Context, messages []Message, model string) (<-chan string, error) {
	chatProvider, err := provider.NewChatProvider(model)
	if err != nil {
		return nil, err
	}
	schemaPrompt, err := tools.GlobalSchemaPrompt()
	if err != nil {
		return nil, err
	}
	chatSvc := service.NewChatService(c.memorySvc, c.workingSvc, c.todoSvc, c.roleSvc, chatProvider, schemaPrompt)
	return chatSvc.Send(ctx, &domain.ChatRequest{Messages: messages, Model: model})
}

// GetMemoryStats 返回 TUI 所需的当前记忆统计信息。
func (c *localChatClient) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	stats, err := c.memorySvc.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return &MemoryStats{
		PersistentItems: stats.PersistentItems,
		SessionItems:    stats.SessionItems,
		TotalItems:      stats.TotalItems,
		TopK:            stats.TopK,
		MinScore:        stats.MinScore,
		Path:            stats.Path,
		ByType:          stats.ByType,
	}, nil
}

// ClearMemory 通过本地记忆服务清空长期记忆。
func (c *localChatClient) ClearMemory(ctx context.Context) error {
	return c.memorySvc.Clear(ctx)
}

// ClearSessionMemory 清空会话记忆和工作记忆状态。
func (c *localChatClient) ClearSessionMemory(ctx context.Context) error {
	if err := c.memorySvc.ClearSession(ctx); err != nil {
		return err
	}
	if c.workingSvc != nil {
		return c.workingSvc.Clear(ctx)
	}
	return nil
}

// GetTodoList 返回当前任务清单。
func (c *localChatClient) GetTodoList(ctx context.Context) ([]domain.Todo, error) {
	if c.todoSvc == nil {
		return nil, nil
	}
	return c.todoSvc.ListTodos(ctx)
}

// AddTodo 添加一个新任务。
func (c *localChatClient) AddTodo(ctx context.Context, content string, priority domain.TodoPriority) (*domain.Todo, error) {
	if c.todoSvc == nil {
		return nil, nil
	}
	return c.todoSvc.AddTodo(ctx, content, priority)
}

// UpdateTodoStatus 更新任务状态。
func (c *localChatClient) UpdateTodoStatus(ctx context.Context, id string, status domain.TodoStatus) error {
	if c.todoSvc == nil {
		return nil
	}
	return c.todoSvc.UpdateTodoStatus(ctx, id, status)
}

// RemoveTodo 移除特定任务。
func (c *localChatClient) RemoveTodo(ctx context.Context, id string) error {
	if c.todoSvc == nil {
		return nil
	}
	return c.todoSvc.RemoveTodo(ctx, id)
}

// DefaultModel 返回 TUI 使用的默认模型。
func (c *localChatClient) DefaultModel() string {
	return provider.DefaultModelForConfig(c.config)
}

// GetWorkingSessionSummary 返回当前工作区上次保存的会话摘要。
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
