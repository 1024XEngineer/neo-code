package gateway

// FrameType 表示网关协议帧类型。
type FrameType string

const (
	// FrameTypeRequest 表示客户端发往网关的请求帧。
	FrameTypeRequest FrameType = "request"
	// FrameTypeEvent 表示网关推送给客户端的事件帧。
	FrameTypeEvent FrameType = "event"
	// FrameTypeError 表示网关推送给客户端的错误帧。
	FrameTypeError FrameType = "error"
	// FrameTypeAck 表示网关对请求的接收确认帧。
	FrameTypeAck FrameType = "ack"
)

// FrameAction 表示请求动作类型。
type FrameAction string

const (
	// FrameActionAuthenticate 表示连接级认证动作。
	FrameActionAuthenticate FrameAction = "authenticate"
	// FrameActionPing 表示探活动作，用于验证网关可用性。
	FrameActionPing FrameAction = "ping"
	// FrameActionBindStream 表示声明流式事件订阅绑定。
	FrameActionBindStream FrameAction = "bind_stream"
	// FrameActionRun 表示发起一次运行。
	FrameActionRun FrameAction = "run"
	// FrameActionCompact 表示触发一次手动压缩。
	FrameActionCompact FrameAction = "compact"
	// FrameActionExecuteSystemTool 表示触发一次系统工具执行。
	FrameActionExecuteSystemTool FrameAction = "execute_system_tool"
	// FrameActionActivateSessionSkill 表示在会话内激活一个 skill。
	FrameActionActivateSessionSkill FrameAction = "activate_session_skill"
	// FrameActionDeactivateSessionSkill 表示在会话内停用一个 skill。
	FrameActionDeactivateSessionSkill FrameAction = "deactivate_session_skill"
	// FrameActionListSessionSkills 表示查询会话内已激活 skills。
	FrameActionListSessionSkills FrameAction = "list_session_skills"
	// FrameActionListAvailableSkills 表示查询当前可用 skills 列表。
	FrameActionListAvailableSkills FrameAction = "list_available_skills"
	// FrameActionCancel 表示取消当前活跃运行。
	FrameActionCancel FrameAction = "cancel"
	// FrameActionListSessions 表示获取会话摘要列表。
	FrameActionListSessions FrameAction = "list_sessions"
	// FrameActionCreateSession 表示显式创建会话。
	FrameActionCreateSession FrameAction = "create_session"
	// FrameActionLoadSession 表示加载指定会话详情。
	FrameActionLoadSession FrameAction = "load_session"
	// FrameActionListSessionTodos 表示查询指定会话 Todo 快照。
	FrameActionListSessionTodos FrameAction = "session_todos_list"
	// FrameActionGetRuntimeSnapshot 表示查询指定会话 runtime 快照。
	FrameActionGetRuntimeSnapshot FrameAction = "runtime_snapshot_get"
	// FrameActionResolvePermission 表示提交一次权限审批决策。
	FrameActionResolvePermission FrameAction = "resolve_permission"
	// FrameActionDeleteSession 表示删除/归档会话。
	FrameActionDeleteSession FrameAction = "delete_session"
	// FrameActionRenameSession 表示重命名会话。
	FrameActionRenameSession FrameAction = "rename_session"
	// FrameActionListFiles 表示列出工作目录文件树。
	FrameActionListFiles FrameAction = "list_files"
	// FrameActionListModels 表示列出可用模型。
	FrameActionListModels FrameAction = "list_models"
	// FrameActionSetSessionModel 表示设置会话模型。
	FrameActionSetSessionModel FrameAction = "set_session_model"
	// FrameActionGetSessionModel 表示获取当前会话模型。
	FrameActionGetSessionModel FrameAction = "get_session_model"
	// FrameActionListProviders 表示列出可管理 provider。
	FrameActionListProviders FrameAction = "list_providers"
	// FrameActionCreateCustomProvider 表示创建自定义 provider。
	FrameActionCreateCustomProvider FrameAction = "create_custom_provider"
	// FrameActionDeleteCustomProvider 表示删除自定义 provider。
	FrameActionDeleteCustomProvider FrameAction = "delete_custom_provider"
	// FrameActionSelectProviderModel 表示设置全局 provider/model。
	FrameActionSelectProviderModel FrameAction = "select_provider_model"
	// FrameActionListMCPServers 表示列出 MCP server 配置。
	FrameActionListMCPServers FrameAction = "list_mcp_servers"
	// FrameActionUpsertMCPServer 表示新增或更新 MCP server 配置。
	FrameActionUpsertMCPServer FrameAction = "upsert_mcp_server"
	// FrameActionSetMCPServerEnabled 表示启停 MCP server。
	FrameActionSetMCPServerEnabled FrameAction = "set_mcp_server_enabled"
	// FrameActionDeleteMCPServer 表示删除 MCP server。
	FrameActionDeleteMCPServer FrameAction = "delete_mcp_server"
	// FrameActionWakeOpenURL 表示处理 URL Scheme 唤醒请求。
	FrameActionWakeOpenURL FrameAction = "wake.openUrl"
	// FrameActionListCheckpoints 表示查询会话 checkpoint 列表。
	FrameActionListCheckpoints FrameAction = "checkpoint_list"
	// FrameActionRestoreCheckpoint 表示恢复到指定 checkpoint。
	FrameActionRestoreCheckpoint FrameAction = "checkpoint_restore"
	// FrameActionUndoRestore 表示撤销最近一次 checkpoint 恢复。
	FrameActionUndoRestore FrameAction = "checkpoint_undo_restore"
	// FrameActionCheckpointDiff 表示查询两个相邻代码检查点之间的差异。
	FrameActionCheckpointDiff FrameAction = "checkpoint_diff"
	// FrameActionWorkspaceList 表示列出所有工作区。
	FrameActionWorkspaceList FrameAction = "workspace.list"
	// FrameActionWorkspaceCreate 表示创建/注册一个新工作区。
	FrameActionWorkspaceCreate FrameAction = "workspace.create"
	// FrameActionWorkspaceSwitch 表示切换当前连接的工作区。
	FrameActionWorkspaceSwitch FrameAction = "workspace.switch"
	// FrameActionWorkspaceRename 表示重命名工作区。
	FrameActionWorkspaceRename FrameAction = "workspace.rename"
	// FrameActionWorkspaceDelete 表示删除工作区。
	FrameActionWorkspaceDelete FrameAction = "workspace.delete"
)

// InputPartType 表示多模态输入分片类型。
type InputPartType string

const (
	// InputPartTypeText 表示文本分片。
	InputPartTypeText InputPartType = "text"
	// InputPartTypeImage 表示图片分片。
	InputPartTypeImage InputPartType = "image"
)

// Media 表示非文本输入的媒体描述。
type Media struct {
	// URI 是媒体资源地址。
	URI string `json:"uri"`
	// MimeType 是媒体 MIME 类型。
	MimeType string `json:"mime_type"`
	// FileName 是媒体文件名。
	FileName string `json:"file_name,omitempty"`
}

// InputPart 表示网关协议中的多模态输入分片。
type InputPart struct {
	// Type 表示分片类型，如 text / image。
	Type InputPartType `json:"type"`
	// Text 是文本分片内容，仅 text 类型使用。
	Text string `json:"text,omitempty"`
	// Media 是非文本分片媒体信息，仅 image 等类型使用。
	Media *Media `json:"media,omitempty"`
}

// FrameError 表示协议帧中的错误信息。
type FrameError struct {
	// Code 是稳定错误码，供客户端做分支判断。
	Code string `json:"code"`
	// Message 是面向用户或日志的错误描述。
	Message string `json:"message"`
}

// MessageFrame 是网关与客户端之间的统一通信帧。
type MessageFrame struct {
	// Type 是帧类型。
	Type FrameType `json:"type"`
	// Action 是请求动作，非 request 帧可为空。
	Action FrameAction `json:"action,omitempty"`
	// RequestID 是客户端请求幂等标识。
	RequestID string `json:"request_id,omitempty"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// InputText 是文本输入内容。
	InputText string `json:"input_text,omitempty"`
	// InputParts 是多模态输入分片。
	InputParts []InputPart `json:"input_parts,omitempty"`
	// Workdir 是本次请求的工作目录覆盖值。
	Workdir string `json:"workdir,omitempty"`
	// Mode 是本次请求的 Agent 工作模式（build / plan）。
	Mode string `json:"mode,omitempty"`
	// Payload 是动作扩展负载或事件负载。
	Payload any `json:"payload,omitempty"`
	// Error 是错误帧负载。
	Error *FrameError `json:"error,omitempty"`
	// SkipSessionHydration 为 true 时跳过基于连接绑定的 session_id 回填，用于新建会话场景。
	SkipSessionHydration bool `json:"-"`
}
