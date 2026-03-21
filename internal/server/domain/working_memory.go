package domain

import "context"

// WorkingMemoryTurn 表示一轮可复用的用户/助手对话。
type WorkingMemoryTurn struct {
	User      string
	Assistant string
}

// WorkingMemoryState 表示当前会话的工作记忆快照。
// 第一阶段只保留任务摘要、最近对话、待解决问题和最近文件引用。
type WorkingMemoryState struct {
	CurrentTask   string
	TaskSummary   string
	RecentTurns   []WorkingMemoryTurn
	OpenQuestions []string
	RecentFiles   []string
}

// WorkingMemoryRepository 定义工作记忆的存取接口。
type WorkingMemoryRepository interface {
	Get(ctx context.Context) (*WorkingMemoryState, error)
	Save(ctx context.Context, state *WorkingMemoryState) error
	Clear(ctx context.Context) error
}

// WorkingMemoryService 负责构建和维护短期工作记忆。
type WorkingMemoryService interface {
	BuildContext(ctx context.Context, messages []Message) (string, error)
	Refresh(ctx context.Context, messages []Message) error
	Clear(ctx context.Context) error
}
