package state

import providertypes "neo-code/internal/provider/types"

// RuntimeMsg 封装 runtime 事件流消息。
type RuntimeMsg struct {
	Event any
}

// RuntimeClosedMsg 表示 runtime 事件通道已关闭。
type RuntimeClosedMsg struct{}

// RunFinishedMsg 表示一次 Run 调用结束。
type RunFinishedMsg struct {
	Err error
}

// ModelCatalogRefreshMsg 表示模型目录刷新结果。
type ModelCatalogRefreshMsg struct {
	ProviderID string
	Models     []providertypes.ModelDescriptor
	Err        error
}

// CompactFinishedMsg 表示 compact 调用结束。
type CompactFinishedMsg struct {
	Err error
}

// PermissionResolvedMsg 表示权限审批结果已回传。
type PermissionResolvedMsg struct {
	RequestID string
	Decision  string
	Err       error
}

// LocalCommandResultMsg 表示本地命令执行结果。
type LocalCommandResultMsg struct {
	Notice          string
	Err             error
	ProviderChanged bool
	ModelChanged    bool
}

// WorkspaceCommandResultMsg 表示工作区命令执行结果。
type WorkspaceCommandResultMsg struct {
	Command string
	Output  string
	Err     error
}

// PermissionResolutionFinishedMsg 表示一次权限审批提交完成结果。
type PermissionResolutionFinishedMsg struct {
	RequestID string
	Decision  string
	Err       error
}
