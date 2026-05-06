package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// JSONRPCVersion 表示当前网关控制面固定使用的 JSON-RPC 协议版本。
	JSONRPCVersion = "2.0"
)

const (
	// MethodGatewayAuthenticate 表示连接握手认证方法。
	MethodGatewayAuthenticate = "gateway.authenticate"
	// MethodGatewayPing 表示网关探活方法。
	MethodGatewayPing = "gateway.ping"
	// MethodGatewayBindStream 表示客户端向网关声明流式订阅绑定的方法。
	MethodGatewayBindStream = "gateway.bindStream"
	// MethodGatewayRun 表示通过网关触发一次运行时执行。
	MethodGatewayRun = "gateway.run"
	// MethodGatewayCompact 表示通过网关触发一次会话压缩。
	MethodGatewayCompact = "gateway.compact"
	// MethodGatewayExecuteSystemTool 表示通过网关触发一次系统工具执行。
	MethodGatewayExecuteSystemTool = "gateway.executeSystemTool"
	// MethodGatewayActivateSessionSkill 表示通过网关在会话内激活技能。
	MethodGatewayActivateSessionSkill = "gateway.activateSessionSkill"
	// MethodGatewayDeactivateSessionSkill 表示通过网关在会话内停用技能。
	MethodGatewayDeactivateSessionSkill = "gateway.deactivateSessionSkill"
	// MethodGatewayListSessionSkills 表示通过网关查询会话激活技能列表。
	MethodGatewayListSessionSkills = "gateway.listSessionSkills"
	// MethodGatewayListAvailableSkills 表示通过网关查询可用技能列表。
	MethodGatewayListAvailableSkills = "gateway.listAvailableSkills"
	// MethodGatewayCancel 表示取消当前活跃运行。
	MethodGatewayCancel = "gateway.cancel"
	// MethodGatewayListSessions 表示查询会话摘要列表。
	MethodGatewayListSessions = "gateway.listSessions"
	// MethodGatewayCreateSession 表示通过网关创建会话。
	MethodGatewayCreateSession = "gateway.createSession"
	// MethodGatewayLoadSession 表示加载单个会话详情。
	MethodGatewayLoadSession = "gateway.loadSession"
	// MethodGatewayListSessionTodos 表示查询会话 Todo 快照。
	MethodGatewayListSessionTodos = "session.todos.list"
	// MethodGatewayGetRuntimeSnapshot 表示查询会话 runtime 快照。
	MethodGatewayGetRuntimeSnapshot = "runtime.snapshot.get"
	// MethodGatewayListCheckpoints 表示查询会话 checkpoint 列表。
	MethodGatewayListCheckpoints = "checkpoint.list"
	// MethodGatewayRestoreCheckpoint 表示恢复到指定 checkpoint。
	MethodGatewayRestoreCheckpoint = "checkpoint.restore"
	// MethodGatewayUndoRestore 表示撤销最近一次 checkpoint 恢复。
	MethodGatewayUndoRestore = "checkpoint.undoRestore"
	// MethodGatewayCheckpointDiff 表示查询 checkpoint diff。
	MethodGatewayCheckpointDiff = "checkpoint.diff"
	// MethodGatewayResolvePermission 表示提交权限审批决策。
	MethodGatewayResolvePermission = "gateway.resolvePermission"
	// MethodGatewayDeleteSession 表示删除/归档会话。
	MethodGatewayDeleteSession = "gateway.deleteSession"
	// MethodGatewayRenameSession 表示重命名会话。
	MethodGatewayRenameSession = "gateway.renameSession"
	// MethodGatewayListFiles 表示列出工作目录文件树。
	MethodGatewayListFiles = "gateway.listFiles"
	// MethodGatewayListModels 表示列出可用模型。
	MethodGatewayListModels = "gateway.listModels"
	// MethodGatewaySetSessionModel 表示设置会话模型。
	MethodGatewaySetSessionModel = "gateway.setSessionModel"
	// MethodGatewayGetSessionModel 表示获取当前会话模型。
	MethodGatewayGetSessionModel = "gateway.getSessionModel"
	// MethodGatewayListProviders 表示列出可管理 provider。
	MethodGatewayListProviders = "gateway.listProviders"
	// MethodGatewayCreateCustomProvider 表示创建自定义 provider。
	MethodGatewayCreateCustomProvider = "gateway.createCustomProvider"
	// MethodGatewayDeleteCustomProvider 表示删除自定义 provider。
	MethodGatewayDeleteCustomProvider = "gateway.deleteCustomProvider"
	// MethodGatewaySelectProviderModel 表示设置全局 provider/model。
	MethodGatewaySelectProviderModel = "gateway.selectProviderModel"
	// MethodGatewayListMCPServers 表示列出 MCP server 配置。
	MethodGatewayListMCPServers = "gateway.listMCPServers"
	// MethodGatewayUpsertMCPServer 表示新增或更新 MCP server 配置。
	MethodGatewayUpsertMCPServer = "gateway.upsertMCPServer"
	// MethodGatewaySetMCPServerEnabled 表示启停 MCP server。
	MethodGatewaySetMCPServerEnabled = "gateway.setMCPServerEnabled"
	// MethodGatewayDeleteMCPServer 表示删除 MCP server。
	MethodGatewayDeleteMCPServer = "gateway.deleteMCPServer"
	// MethodGatewayEvent 表示网关向客户端推送运行时事件的通知方法。
	MethodGatewayEvent = "gateway.event"
	// MethodWakeOpenURL 表示 URL Scheme 唤醒方法。
	MethodWakeOpenURL            = "wake.openUrl"
	MethodGatewayListWorkspaces  = "gateway.listWorkspaces"
	MethodGatewayCreateWorkspace = "gateway.createWorkspace"
	MethodGatewaySwitchWorkspace = "gateway.switchWorkspace"
	MethodGatewayRenameWorkspace = "gateway.renameWorkspace"
	MethodGatewayDeleteWorkspace = "gateway.deleteWorkspace"
)

const (
	// JSONRPCCodeParseError 表示请求体不是合法 JSON。
	JSONRPCCodeParseError = -32700
	// JSONRPCCodeInvalidRequest 表示请求结构不符合 JSON-RPC 规范。
	JSONRPCCodeInvalidRequest = -32600
	// JSONRPCCodeMethodNotFound 表示方法未注册。
	JSONRPCCodeMethodNotFound = -32601
	// JSONRPCCodeInvalidParams 表示参数不合法。
	JSONRPCCodeInvalidParams = -32602
	// JSONRPCCodeInternalError 表示服务端内部错误。
	JSONRPCCodeInternalError = -32603
)

const (
	// GatewayCodeInvalidFrame 表示请求帧结构非法。
	GatewayCodeInvalidFrame = "invalid_frame"
	// GatewayCodeInvalidAction 表示动作参数非法。
	GatewayCodeInvalidAction = "invalid_action"
	// GatewayCodeInvalidMultimodalPayload 表示多模态载荷非法。
	GatewayCodeInvalidMultimodalPayload = "invalid_multimodal_payload"
	// GatewayCodeMissingRequiredField 表示缺少必填字段。
	GatewayCodeMissingRequiredField = "missing_required_field"
	// GatewayCodeUnsupportedAction 表示动作尚未实现。
	GatewayCodeUnsupportedAction = "unsupported_action"
	// GatewayCodeInternalError 表示网关内部错误。
	GatewayCodeInternalError = "internal_error"
	// GatewayCodeTimeout 表示网关处理请求时发生超时。
	GatewayCodeTimeout = "timeout"
	// GatewayCodeUnsafePath 表示路径存在安全风险。
	GatewayCodeUnsafePath = "unsafe_path"
	// GatewayCodeUnauthorized 表示请求未通过认证校验。
	GatewayCodeUnauthorized = "unauthorized"
	// GatewayCodeAccessDenied 表示请求已认证但未通过 ACL 校验。
	GatewayCodeAccessDenied     = "access_denied"
	GatewayCodeResourceNotFound = "resource_not_found"
)

// JSONRPCRequest 表示控制面接收到的 JSON-RPC 请求。
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse 表示控制面输出的 JSON-RPC 响应。
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCNotification 表示控制面向客户端主动推送的 JSON-RPC 通知。
type JSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCError 表示 JSON-RPC 错误载荷。
type JSONRPCError struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    *JSONRPCErrorData `json:"data,omitempty"`
}

// JSONRPCErrorData 表示网关扩展错误字段。
type JSONRPCErrorData struct {
	GatewayCode string `json:"gateway_code,omitempty"`
}

// NormalizedRequest 表示从 JSON-RPC 归一化后的内部请求模型。
type NormalizedRequest struct {
	ID        json.RawMessage
	RequestID string
	Action    string
	SessionID string
	RunID     string
	Workdir   string
	Mode      string
	Payload   any
}

// AuthenticateParams 表示 gateway.authenticate 的标准化参数。
type AuthenticateParams struct {
	Token string `json:"token"`
}

// BindStreamParams 表示 gateway.bindStream 的标准化参数载荷。
type BindStreamParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

// RunInputMedia 用于承载 gateway.run 中图片分片的媒体元数据。
type RunInputMedia struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type"`
	FileName string `json:"file_name,omitempty"`
}

// RunInputPart 表示 gateway.run 中的单个输入分片。
type RunInputPart struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	Media *RunInputMedia `json:"media,omitempty"`
}

// RunParams 表示 gateway.run 的参数载荷。
type RunParams struct {
	SessionID  string         `json:"session_id,omitempty"`
	NewSession bool           `json:"new_session,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	InputText  string         `json:"input_text,omitempty"`
	InputParts []RunInputPart `json:"input_parts,omitempty"`
	Workdir    string         `json:"workdir,omitempty"`
	Mode       string         `json:"mode,omitempty"`
}

// CancelParams 表示 gateway.cancel 可选参数。
type CancelParams struct {
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

// CompactParams 表示 gateway.compact 参数。
type CompactParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
}

// ExecuteSystemToolParams 表示 gateway.executeSystemTool 参数。
type ExecuteSystemToolParams struct {
	SessionID string          `json:"session_id,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ActivateSessionSkillParams 表示 gateway.activateSessionSkill 参数。
type ActivateSessionSkillParams struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
}

// DeactivateSessionSkillParams 表示 gateway.deactivateSessionSkill 参数。
type DeactivateSessionSkillParams struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
}

// ListSessionSkillsParams 表示 gateway.listSessionSkills 参数。
type ListSessionSkillsParams struct {
	SessionID string `json:"session_id"`
}

// ListAvailableSkillsParams 表示 gateway.listAvailableSkills 参数。
type ListAvailableSkillsParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// LoadSessionParams 表示 gateway.loadSession 参数。
type LoadSessionParams struct {
	SessionID string `json:"session_id"`
}

// CreateSessionParams 表示 gateway.createSession 参数。
type CreateSessionParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// ListSessionTodosParams 表示 session.todos.list 参数。
type ListSessionTodosParams struct {
	SessionID string `json:"session_id"`
}

// GetRuntimeSnapshotParams 表示 runtime.snapshot.get 参数。
type GetRuntimeSnapshotParams struct {
	SessionID string `json:"session_id"`
}

// ListCheckpointsParams 表示 checkpoint.list 参数。
type ListCheckpointsParams struct {
	SessionID      string `json:"session_id"`
	Limit          int    `json:"limit,omitempty"`
	RestorableOnly bool   `json:"restorable_only,omitempty"`
}

// RestoreCheckpointParams 表示 checkpoint.restore 参数。
type RestoreCheckpointParams struct {
	SessionID    string `json:"session_id"`
	CheckpointID string `json:"checkpoint_id"`
	Force        bool   `json:"force,omitempty"`
}

// UndoRestoreParams 表示 checkpoint.undoRestore 参数。
type UndoRestoreParams struct {
	SessionID string `json:"session_id"`
}

// CheckpointDiffParams 表示 checkpoint.diff 参数。
type CheckpointDiffParams struct {
	SessionID    string `json:"session_id"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

// ResolvePermissionParams 表示 gateway.resolvePermission 参数。
type ResolvePermissionParams struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

// DeleteSessionParams 表示 gateway.deleteSession 参数。
type DeleteSessionParams struct {
	SessionID string `json:"session_id"`
}

// RenameSessionParams 表示 gateway.renameSession 参数。
type RenameSessionParams struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// ListFilesParams 表示 gateway.listFiles 参数。
type ListFilesParams struct {
	SessionID string `json:"session_id,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	Path      string `json:"path,omitempty"`
}

// ListModelsParams 表示 gateway.listModels 参数。
type ListModelsParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// SetSessionModelParams 表示 gateway.setSessionModel 参数。
type SetSessionModelParams struct {
	SessionID  string `json:"session_id"`
	ProviderID string `json:"provider_id,omitempty"`
	ModelID    string `json:"model_id"`
}

// GetSessionModelParams 表示 gateway.getSessionModel 参数。
type GetSessionModelParams struct {
	SessionID string `json:"session_id"`
}

// CreateCustomProviderParams 表示 gateway.createCustomProvider 参数。
type CreateCustomProviderParams struct {
	Name                  string                    `json:"name"`
	Driver                string                    `json:"driver"`
	BaseURL               string                    `json:"base_url,omitempty"`
	ChatAPIMode           string                    `json:"chat_api_mode,omitempty"`
	ChatEndpointPath      string                    `json:"chat_endpoint_path,omitempty"`
	APIKeyEnv             string                    `json:"api_key_env"`
	APIKey                string                    `json:"api_key,omitempty"`
	ModelSource           string                    `json:"model_source,omitempty"`
	DiscoveryEndpointPath string                    `json:"discovery_endpoint_path,omitempty"`
	Models                []ProviderModelDescriptor `json:"models,omitempty"`
}

// ProviderModelDescriptor 表示 provider 管理接口中的模型描述。
type ProviderModelDescriptor struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Description     string                       `json:"description,omitempty"`
	ContextWindow   int                          `json:"context_window,omitempty"`
	MaxOutputTokens int                          `json:"max_output_tokens,omitempty"`
	CapabilityHints ProviderModelCapabilityHints `json:"capability_hints,omitempty"`
}

// ProviderModelCapabilityHints 表示 provider 管理接口中的模型能力提示。
type ProviderModelCapabilityHints struct {
	ToolCalling string `json:"tool_calling,omitempty"`
	ImageInput  string `json:"image_input,omitempty"`
}

// DeleteCustomProviderParams 表示 gateway.deleteCustomProvider 参数。
type DeleteCustomProviderParams struct {
	ProviderID string `json:"provider_id"`
}

// SelectProviderModelParams 表示 gateway.selectProviderModel 参数。
type SelectProviderModelParams struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id,omitempty"`
}

// MCPStdioParams 表示 MCP server stdio 参数。
type MCPStdioParams struct {
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	Workdir           string   `json:"workdir,omitempty"`
	StartTimeoutSec   int      `json:"start_timeout_sec,omitempty"`
	CallTimeoutSec    int      `json:"call_timeout_sec,omitempty"`
	RestartBackoffSec int      `json:"restart_backoff_sec,omitempty"`
}

// MCPEnvVarParams 表示 MCP server 环境变量参数。
type MCPEnvVarParams struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	ValueEnv string `json:"value_env,omitempty"`
}

// MCPServerParams 表示 MCP server 配置参数。
type MCPServerParams struct {
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled,omitempty"`
	Source  string            `json:"source,omitempty"`
	Version string            `json:"version,omitempty"`
	Stdio   MCPStdioParams    `json:"stdio,omitempty"`
	Env     []MCPEnvVarParams `json:"env,omitempty"`
}

// UpsertMCPServerParams 表示 gateway.upsertMCPServer 参数。
type UpsertMCPServerParams struct {
	Server MCPServerParams `json:"server"`
}

// SetMCPServerEnabledParams 表示 gateway.setMCPServerEnabled 参数。
type SetMCPServerEnabledParams struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// DeleteMCPServerParams 表示 gateway.deleteMCPServer 参数。
type DeleteMCPServerParams struct {
	ID string `json:"id"`
}

// ListWorkspacesParams 表示 gateway.listWorkspaces 参数。
type ListWorkspacesParams struct{}

// CreateWorkspaceParams 表示 gateway.createWorkspace 参数。
type CreateWorkspaceParams struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// SwitchWorkspaceParams 表示 gateway.switchWorkspace 参数。
type SwitchWorkspaceParams struct {
	WorkspaceHash string `json:"workspace_hash"`
}

// RenameWorkspaceParams 表示 gateway.renameWorkspace 参数。
type RenameWorkspaceParams struct {
	WorkspaceHash string `json:"workspace_hash"`
	Name          string `json:"name"`
}

// DeleteWorkspaceParams 表示 gateway.deleteWorkspace 参数。
type DeleteWorkspaceParams struct {
	WorkspaceHash string `json:"workspace_hash"`
	RemoveData    bool   `json:"remove_data,omitempty"`
}

// NormalizeJSONRPCRequest 将 JSON-RPC 请求归一化为内部请求模型，并做方法级参数解析。
func NormalizeJSONRPCRequest(request JSONRPCRequest) (NormalizedRequest, *JSONRPCError) {
	normalized := NormalizedRequest{}

	requestID, idErr := normalizeJSONRPCID(request.ID)
	normalized.RequestID = requestID
	if idErr != nil {
		return normalized, idErr
	}
	normalized.ID = cloneJSONRawMessage(request.ID)

	if strings.TrimSpace(request.JSONRPC) != JSONRPCVersion {
		return normalized, NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid jsonrpc version",
			GatewayCodeInvalidFrame,
		)
	}

	method := strings.TrimSpace(request.Method)
	if method == "" {
		return normalized, NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"missing required field: method",
			GatewayCodeMissingRequiredField,
		)
	}

	switch method {
	case MethodGatewayAuthenticate:
		params, parseErr := decodeAuthenticateParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "authenticate"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayPing:
		normalized.Action = "ping"
		return normalized, nil
	case MethodGatewayBindStream:
		params, parseErr := decodeBindStreamParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "bind_stream"
		normalized.SessionID = params.SessionID
		normalized.RunID = params.RunID
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayRun:
		params, parseErr := decodeRunParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "run"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Workdir = strings.TrimSpace(params.Workdir)
		normalized.Mode = strings.TrimSpace(params.Mode)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayCompact:
		params, parseErr := decodeCompactParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "compact"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayExecuteSystemTool:
		params, parseErr := decodeExecuteSystemToolParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "execute_system_tool"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Workdir = strings.TrimSpace(params.Workdir)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayActivateSessionSkill:
		params, parseErr := decodeActivateSessionSkillParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "activate_session_skill"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeactivateSessionSkill:
		params, parseErr := decodeDeactivateSessionSkillParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "deactivate_session_skill"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessionSkills:
		params, parseErr := decodeListSessionSkillsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_session_skills"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListAvailableSkills:
		params, parseErr := decodeListAvailableSkillsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_available_skills"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayCancel:
		params, parseErr := decodeCancelParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "cancel"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessions:
		normalized.Action = "list_sessions"
		return normalized, nil
	case MethodGatewayCreateSession:
		params, parseErr := decodeCreateSessionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "create_session"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayLoadSession:
		params, parseErr := decodeLoadSessionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "load_session"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessionTodos:
		params, parseErr := decodeListSessionTodosParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "session_todos_list"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayGetRuntimeSnapshot:
		params, parseErr := decodeGetRuntimeSnapshotParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "runtime_snapshot_get"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListCheckpoints:
		params, parseErr := decodeListCheckpointsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "checkpoint_list"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayRestoreCheckpoint:
		params, parseErr := decodeRestoreCheckpointParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "checkpoint_restore"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayUndoRestore:
		params, parseErr := decodeUndoRestoreParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "checkpoint_undo_restore"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayCheckpointDiff:
		params, parseErr := decodeCheckpointDiffParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "checkpoint_diff"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayResolvePermission:
		params, parseErr := decodeResolvePermissionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "resolve_permission"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeleteSession:
		params, parseErr := decodeDeleteSessionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "delete_session"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayRenameSession:
		params, parseErr := decodeRenameSessionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "rename_session"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListFiles:
		params, parseErr := decodeListFilesParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_files"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Workdir = strings.TrimSpace(params.Workdir)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListModels:
		params, parseErr := decodeListModelsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_models"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewaySetSessionModel:
		params, parseErr := decodeSetSessionModelParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "set_session_model"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayGetSessionModel:
		params, parseErr := decodeGetSessionModelParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "get_session_model"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListProviders:
		normalized.Action = "list_providers"
		return normalized, nil
	case MethodGatewayCreateCustomProvider:
		params, parseErr := decodeCreateCustomProviderParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "create_custom_provider"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeleteCustomProvider:
		params, parseErr := decodeDeleteCustomProviderParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "delete_custom_provider"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewaySelectProviderModel:
		params, parseErr := decodeSelectProviderModelParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "select_provider_model"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListMCPServers:
		normalized.Action = "list_mcp_servers"
		return normalized, nil
	case MethodGatewayUpsertMCPServer:
		params, parseErr := decodeUpsertMCPServerParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "upsert_mcp_server"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewaySetMCPServerEnabled:
		params, parseErr := decodeSetMCPServerEnabledParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "set_mcp_server_enabled"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeleteMCPServer:
		params, parseErr := decodeDeleteMCPServerParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "delete_mcp_server"
		normalized.Payload = params
		return normalized, nil
	case MethodWakeOpenURL:
		intent, parseErr := decodeWakeIntentParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = MethodWakeOpenURL
		normalized.SessionID = strings.TrimSpace(intent.SessionID)
		normalized.Workdir = strings.TrimSpace(intent.Workdir)
		normalized.Payload = intent
		return normalized, nil
	case MethodGatewayListWorkspaces:
		normalized.Action = "workspace.list"
		return normalized, nil
	case MethodGatewayCreateWorkspace:
		params, parseErr := decodeCreateWorkspaceParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "workspace.create"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewaySwitchWorkspace:
		params, parseErr := decodeSwitchWorkspaceParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "workspace.switch"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayRenameWorkspace:
		params, parseErr := decodeRenameWorkspaceParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "workspace.rename"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeleteWorkspace:
		params, parseErr := decodeDeleteWorkspaceParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "workspace.delete"
		normalized.Payload = params
		return normalized, nil
	default:
		return normalized, NewJSONRPCError(
			JSONRPCCodeMethodNotFound,
			"method not found",
			GatewayCodeUnsupportedAction,
		)
	}
}

// decodeGetRuntimeSnapshotParams 对 runtime.snapshot.get 的 params 执行反序列化与字段校验。
func decodeGetRuntimeSnapshotParams(raw json.RawMessage) (GetRuntimeSnapshotParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return GetRuntimeSnapshotParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}
	var params GetRuntimeSnapshotParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return GetRuntimeSnapshotParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for runtime.snapshot.get",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	if params.SessionID == "" {
		return GetRuntimeSnapshotParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeListSessionTodosParams 对 session.todos.list 的 params 执行反序列化与字段校验。
func decodeListSessionTodosParams(raw json.RawMessage) (ListSessionTodosParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}
	var params ListSessionTodosParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for session.todos.list",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	if params.SessionID == "" {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeListCheckpointsParams 对 checkpoint.list 的 params 执行反序列化与字段校验。
func decodeListCheckpointsParams(raw json.RawMessage) (ListCheckpointsParams, *JSONRPCError) {
	return decodeParams(raw, "checkpoint.list", func(p *ListCheckpointsParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.Limit < 0 {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "invalid field: params.limit", GatewayCodeInvalidAction)
		}
		return nil
	})
}

// decodeRestoreCheckpointParams 对 checkpoint.restore 的 params 执行反序列化与字段校验。
func decodeRestoreCheckpointParams(raw json.RawMessage) (RestoreCheckpointParams, *JSONRPCError) {
	return decodeParams(raw, "checkpoint.restore", func(p *RestoreCheckpointParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.CheckpointID = strings.TrimSpace(p.CheckpointID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.CheckpointID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.checkpoint_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeUndoRestoreParams 对 checkpoint.undoRestore 的 params 执行反序列化与字段校验。
func decodeUndoRestoreParams(raw json.RawMessage) (UndoRestoreParams, *JSONRPCError) {
	return decodeParams(raw, "checkpoint.undoRestore", func(p *UndoRestoreParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeCheckpointDiffParams 对 checkpoint.diff 的 params 执行反序列化与字段校验。
func decodeCheckpointDiffParams(raw json.RawMessage) (CheckpointDiffParams, *JSONRPCError) {
	return decodeParams(raw, "checkpoint.diff", func(p *CheckpointDiffParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.CheckpointID = strings.TrimSpace(p.CheckpointID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// NewJSONRPCResultResponse 创建 JSON-RPC 成功响应，并将 result 编码为 RawMessage。
func NewJSONRPCResultResponse(id json.RawMessage, result any) (JSONRPCResponse, *JSONRPCError) {
	rawResult, err := json.Marshal(result)
	if err != nil {
		return JSONRPCResponse{}, NewJSONRPCError(
			JSONRPCCodeInternalError,
			"failed to encode jsonrpc result",
			GatewayCodeInternalError,
		)
	}

	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      cloneJSONRawMessage(id),
		Result:  json.RawMessage(rawResult),
	}, nil
}

// NewJSONRPCErrorResponse 创建 JSON-RPC 错误响应。
func NewJSONRPCErrorResponse(id json.RawMessage, rpcError *JSONRPCError) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      cloneJSONRawMessage(id),
		Error:   rpcError,
	}
}

// NewJSONRPCNotification 创建 JSON-RPC 通知载荷，供网关向客户端推送事件使用。
func NewJSONRPCNotification(method string, params any) JSONRPCNotification {
	return JSONRPCNotification{
		JSONRPC: JSONRPCVersion,
		Method:  strings.TrimSpace(method),
		Params:  params,
	}
}

// NewJSONRPCError 创建带 gateway_code 的 JSON-RPC 错误对象。
func NewJSONRPCError(code int, message, gatewayCode string) *JSONRPCError {
	errorPayload := &JSONRPCError{
		Code:    code,
		Message: message,
	}
	if strings.TrimSpace(gatewayCode) != "" {
		errorPayload.Data = &JSONRPCErrorData{GatewayCode: gatewayCode}
	}
	return errorPayload
}

// GatewayCodeFromJSONRPCError 从 JSON-RPC 错误载荷中提取稳定 gateway_code。
func GatewayCodeFromJSONRPCError(rpcError *JSONRPCError) string {
	if rpcError == nil || rpcError.Data == nil {
		return ""
	}
	return strings.TrimSpace(rpcError.Data.GatewayCode)
}

// MapGatewayCodeToJSONRPCCode 将稳定网关错误码映射到 JSON-RPC 错误码。
func MapGatewayCodeToJSONRPCCode(gatewayCode string) int {
	switch strings.TrimSpace(gatewayCode) {
	case GatewayCodeUnsupportedAction:
		return JSONRPCCodeMethodNotFound
	case GatewayCodeInvalidAction,
		GatewayCodeInvalidFrame,
		GatewayCodeInvalidMultimodalPayload,
		GatewayCodeMissingRequiredField,
		GatewayCodeUnsafePath,
		GatewayCodeUnauthorized,
		GatewayCodeAccessDenied,
		GatewayCodeResourceNotFound:
		return JSONRPCCodeInvalidParams
	case GatewayCodeInternalError:
		return JSONRPCCodeInternalError
	case GatewayCodeTimeout:
		return JSONRPCCodeInternalError
	default:
		return JSONRPCCodeInternalError
	}
}

// normalizeJSONRPCID 校验并提取请求 ID，确保控制面请求具备可关联标识。
func normalizeJSONRPCID(id json.RawMessage) (string, *JSONRPCError) {
	trimmed := bytes.TrimSpace(id)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"missing required field: id",
			GatewayCodeMissingRequiredField,
		)
	}

	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid field: id",
			GatewayCodeInvalidFrame,
		)
	}

	switch value := decoded.(type) {
	case string:
		identifier := strings.TrimSpace(value)
		if identifier == "" {
			return "", NewJSONRPCError(
				JSONRPCCodeInvalidRequest,
				"invalid field: id",
				GatewayCodeInvalidFrame,
			)
		}
		return identifier, nil
	case float64:
		identifier := strings.TrimSpace(string(trimmed))
		if identifier == "" {
			return "", NewJSONRPCError(
				JSONRPCCodeInvalidRequest,
				"invalid field: id",
				GatewayCodeInvalidFrame,
			)
		}
		return identifier, nil
	default:
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid field: id",
			GatewayCodeInvalidFrame,
		)
	}
}

// decodeStrictJSON 使用 DisallowUnknownFields 对 params 做严格反序列化。
func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing json values")
	}
	return nil
}

// decodeAuthenticateParams 对 gateway.authenticate 的 params 执行反序列化与最小校验。
func decodeAuthenticateParams(raw json.RawMessage) (AuthenticateParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.authenticate", func(p *AuthenticateParams) *JSONRPCError {
		p.Token = strings.TrimSpace(p.Token)
		return nil
	})
}

// decodeWakeIntentParams 对 wake.openUrl 的 params 执行延迟反序列化与最小校验。
func decodeWakeIntentParams(raw json.RawMessage) (WakeIntent, *JSONRPCError) {
	return decodeParams(raw, "wake.openUrl", func(p *WakeIntent) *JSONRPCError {
		p.Action = strings.ToLower(strings.TrimSpace(p.Action))
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.Workdir = strings.TrimSpace(p.Workdir)
		if len(p.Params) == 0 {
			p.Params = nil
		}
		return nil
	})
}

// decodeBindStreamParams 对 gateway.bindStream 的 params 执行反序列化与最小参数校验。
func decodeBindStreamParams(raw json.RawMessage) (BindStreamParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.bindStream", func(p *BindStreamParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.RunID = strings.TrimSpace(p.RunID)
		p.Channel = strings.ToLower(strings.TrimSpace(p.Channel))
		if p.Channel == "" {
			p.Channel = "all"
		}
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		switch p.Channel {
		case "all", "ipc", "ws", "sse":
		default:
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "invalid field: params.channel", GatewayCodeInvalidAction)
		}
		return nil
	})
}

// decodeRunParams 对 gateway.run 的 params 执行反序列化与字段清理。
func decodeRunParams(raw json.RawMessage) (RunParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.run", func(p *RunParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.RunID = strings.TrimSpace(p.RunID)
		p.InputText = strings.TrimSpace(p.InputText)
		p.Workdir = strings.TrimSpace(p.Workdir)
		if len(p.InputParts) == 0 {
			p.InputParts = nil
		} else {
			for i := range p.InputParts {
				p.InputParts[i].Type = strings.ToLower(strings.TrimSpace(p.InputParts[i].Type))
				p.InputParts[i].Text = strings.TrimSpace(p.InputParts[i].Text)
				if m := p.InputParts[i].Media; m != nil {
					m.URI = strings.TrimSpace(m.URI)
					m.MimeType = strings.TrimSpace(m.MimeType)
					m.FileName = strings.TrimSpace(m.FileName)
				}
			}
		}
		return nil
	})
}

// decodeCancelParams 对 gateway.cancel 的 params 执行反序列化，缺省或 null 视为空参数。
func decodeCancelParams(raw json.RawMessage) (CancelParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.cancel", func(p *CancelParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.RunID = strings.TrimSpace(p.RunID)
		if p.RunID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.run_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeCompactParams 对 gateway.compact 的 params 执行反序列化与必填字段校验。
func decodeCompactParams(raw json.RawMessage) (CompactParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.compact", func(p *CompactParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.RunID = strings.TrimSpace(p.RunID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeExecuteSystemToolParams 对 gateway.executeSystemTool 的 params 执行反序列化与字段校验。
func decodeExecuteSystemToolParams(raw json.RawMessage) (ExecuteSystemToolParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.executeSystemTool", func(p *ExecuteSystemToolParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.RunID = strings.TrimSpace(p.RunID)
		p.Workdir = strings.TrimSpace(p.Workdir)
		p.ToolName = strings.TrimSpace(p.ToolName)
		p.Arguments = cloneJSONRawMessage(bytes.TrimSpace(p.Arguments))
		if p.ToolName == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.tool_name", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeActivateSessionSkillParams 对 gateway.activateSessionSkill 的 params 执行反序列化与字段校验。
func decodeActivateSessionSkillParams(raw json.RawMessage) (ActivateSessionSkillParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.activateSessionSkill", func(p *ActivateSessionSkillParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.SkillID = strings.TrimSpace(p.SkillID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.SkillID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.skill_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeDeactivateSessionSkillParams 对 gateway.deactivateSessionSkill 的 params 执行反序列化与字段校验。
func decodeDeactivateSessionSkillParams(raw json.RawMessage) (DeactivateSessionSkillParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.deactivateSessionSkill", func(p *DeactivateSessionSkillParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.SkillID = strings.TrimSpace(p.SkillID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.SkillID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.skill_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeListSessionSkillsParams 对 gateway.listSessionSkills 的 params 执行反序列化与字段校验。
func decodeListSessionSkillsParams(raw json.RawMessage) (ListSessionSkillsParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.listSessionSkills", func(p *ListSessionSkillsParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeListAvailableSkillsParams 对 gateway.listAvailableSkills 的 params 执行反序列化与字段清理。
func decodeListAvailableSkillsParams(raw json.RawMessage) (ListAvailableSkillsParams, *JSONRPCError) {
	return decodeParamsOptional(raw, "gateway.listAvailableSkills", func(p *ListAvailableSkillsParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		return nil
	})
}

// decodeLoadSessionParams 对 gateway.loadSession 的 params 执行反序列化与必填字段校验。
func decodeLoadSessionParams(raw json.RawMessage) (LoadSessionParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.loadSession", func(p *LoadSessionParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeCreateSessionParams 对 gateway.createSession 的 params 执行反序列化与字段清理。
func decodeCreateSessionParams(raw json.RawMessage) (CreateSessionParams, *JSONRPCError) {
	return decodeParamsOptional(raw, "gateway.createSession", func(p *CreateSessionParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		return nil
	})
}

// decodeResolvePermissionParams 对 gateway.resolvePermission 的 params 执行反序列化与决策校验。
func decodeResolvePermissionParams(raw json.RawMessage) (ResolvePermissionParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.resolvePermission", func(p *ResolvePermissionParams) *JSONRPCError {
		p.RequestID = strings.TrimSpace(p.RequestID)
		p.Decision = strings.ToLower(strings.TrimSpace(p.Decision))
		if p.RequestID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.request_id", GatewayCodeMissingRequiredField)
		}
		switch p.Decision {
		case "allow_once", "allow_session", "reject":
		default:
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "invalid field: params.decision", GatewayCodeInvalidAction)
		}
		return nil
	})
}

// decodeDeleteSessionParams 对 gateway.deleteSession 的 params 执行反序列化与校验。
func decodeDeleteSessionParams(raw json.RawMessage) (DeleteSessionParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.deleteSession", func(p *DeleteSessionParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeRenameSessionParams 对 gateway.renameSession 的 params 执行反序列化与校验。
func decodeRenameSessionParams(raw json.RawMessage) (RenameSessionParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.renameSession", func(p *RenameSessionParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.Title = strings.TrimSpace(p.Title)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.Title == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.title", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeListFilesParams 对 gateway.listFiles 的 params 执行反序列化与校验。
func decodeListFilesParams(raw json.RawMessage) (ListFilesParams, *JSONRPCError) {
	return decodeParamsOptional(raw, "gateway.listFiles", func(p *ListFilesParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.Workdir = strings.TrimSpace(p.Workdir)
		p.Path = strings.TrimSpace(p.Path)
		return nil
	})
}

// decodeListModelsParams 对 gateway.listModels 的 params 执行反序列化与校验。
func decodeListModelsParams(raw json.RawMessage) (ListModelsParams, *JSONRPCError) {
	return decodeParamsOptional(raw, "gateway.listModels", func(p *ListModelsParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		return nil
	})
}

// decodeSetSessionModelParams 对 gateway.setSessionModel 的 params 执行反序列化与校验。
func decodeSetSessionModelParams(raw json.RawMessage) (SetSessionModelParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.setSessionModel", func(p *SetSessionModelParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		p.ProviderID = strings.TrimSpace(p.ProviderID)
		p.ModelID = strings.TrimSpace(p.ModelID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		if p.ModelID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.model_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeGetSessionModelParams 对 gateway.getSessionModel 的 params 执行反序列化与校验。
func decodeGetSessionModelParams(raw json.RawMessage) (GetSessionModelParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.getSessionModel", func(p *GetSessionModelParams) *JSONRPCError {
		p.SessionID = strings.TrimSpace(p.SessionID)
		if p.SessionID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.session_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeCreateCustomProviderParams 对 gateway.createCustomProvider 的 params 执行反序列化与字段清理。
func decodeCreateCustomProviderParams(raw json.RawMessage) (CreateCustomProviderParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.createCustomProvider", func(p *CreateCustomProviderParams) *JSONRPCError {
		p.Name = strings.TrimSpace(p.Name)
		p.Driver = strings.TrimSpace(p.Driver)
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		p.ChatAPIMode = strings.TrimSpace(p.ChatAPIMode)
		p.ChatEndpointPath = strings.TrimSpace(p.ChatEndpointPath)
		p.APIKeyEnv = strings.TrimSpace(p.APIKeyEnv)
		p.APIKey = strings.TrimSpace(p.APIKey)
		p.ModelSource = strings.TrimSpace(p.ModelSource)
		p.DiscoveryEndpointPath = strings.TrimSpace(p.DiscoveryEndpointPath)
		if p.Name == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.name", GatewayCodeMissingRequiredField)
		}
		if p.Driver == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.driver", GatewayCodeMissingRequiredField)
		}
		if p.APIKeyEnv == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.api_key_env", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeDeleteCustomProviderParams 对 gateway.deleteCustomProvider 的 params 执行反序列化与字段校验。
func decodeDeleteCustomProviderParams(raw json.RawMessage) (DeleteCustomProviderParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.deleteCustomProvider", func(p *DeleteCustomProviderParams) *JSONRPCError {
		p.ProviderID = strings.TrimSpace(p.ProviderID)
		if p.ProviderID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.provider_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeSelectProviderModelParams 对 gateway.selectProviderModel 的 params 执行反序列化与字段校验。
func decodeSelectProviderModelParams(raw json.RawMessage) (SelectProviderModelParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.selectProviderModel", func(p *SelectProviderModelParams) *JSONRPCError {
		p.ProviderID = strings.TrimSpace(p.ProviderID)
		p.ModelID = strings.TrimSpace(p.ModelID)
		if p.ProviderID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.provider_id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeUpsertMCPServerParams 对 gateway.upsertMCPServer 的 params 执行反序列化与字段校验。
func decodeUpsertMCPServerParams(raw json.RawMessage) (UpsertMCPServerParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.upsertMCPServer", func(p *UpsertMCPServerParams) *JSONRPCError {
		p.Server.ID = strings.TrimSpace(p.Server.ID)
		p.Server.Source = strings.TrimSpace(p.Server.Source)
		p.Server.Version = strings.TrimSpace(p.Server.Version)
		p.Server.Stdio.Command = strings.TrimSpace(p.Server.Stdio.Command)
		p.Server.Stdio.Workdir = strings.TrimSpace(p.Server.Stdio.Workdir)
		if p.Server.ID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.server.id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeSetMCPServerEnabledParams 对 gateway.setMCPServerEnabled 的 params 执行反序列化与字段校验。
func decodeSetMCPServerEnabledParams(raw json.RawMessage) (SetMCPServerEnabledParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.setMCPServerEnabled", func(p *SetMCPServerEnabledParams) *JSONRPCError {
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeDeleteMCPServerParams 对 gateway.deleteMCPServer 的 params 执行反序列化与字段校验。
func decodeDeleteMCPServerParams(raw json.RawMessage) (DeleteMCPServerParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.deleteMCPServer", func(p *DeleteMCPServerParams) *JSONRPCError {
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.id", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

// decodeParams 是各 decodeXxxParams 的泛型骨架：检查空值、严格反序列化、执行校验。

func decodeListWorkspacesParams(raw json.RawMessage) (ListWorkspacesParams, *JSONRPCError) {
	return decodeParamsOptional(raw, "gateway.listWorkspaces", func(p *ListWorkspacesParams) *JSONRPCError {
		return nil
	})
}

func decodeCreateWorkspaceParams(raw json.RawMessage) (CreateWorkspaceParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.createWorkspace", func(p *CreateWorkspaceParams) *JSONRPCError {
		p.Path = strings.TrimSpace(p.Path)
		p.Name = strings.TrimSpace(p.Name)
		if p.Path == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.path", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

func decodeSwitchWorkspaceParams(raw json.RawMessage) (SwitchWorkspaceParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.switchWorkspace", func(p *SwitchWorkspaceParams) *JSONRPCError {
		p.WorkspaceHash = strings.TrimSpace(p.WorkspaceHash)
		if p.WorkspaceHash == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.workspace_hash", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

func decodeRenameWorkspaceParams(raw json.RawMessage) (RenameWorkspaceParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.renameWorkspace", func(p *RenameWorkspaceParams) *JSONRPCError {
		p.WorkspaceHash = strings.TrimSpace(p.WorkspaceHash)
		p.Name = strings.TrimSpace(p.Name)
		if p.WorkspaceHash == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.workspace_hash", GatewayCodeMissingRequiredField)
		}
		if p.Name == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.name", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

func decodeDeleteWorkspaceParams(raw json.RawMessage) (DeleteWorkspaceParams, *JSONRPCError) {
	return decodeParams(raw, "gateway.deleteWorkspace", func(p *DeleteWorkspaceParams) *JSONRPCError {
		p.WorkspaceHash = strings.TrimSpace(p.WorkspaceHash)
		if p.WorkspaceHash == "" {
			return NewJSONRPCError(JSONRPCCodeInvalidParams, "missing required field: params.workspace_hash", GatewayCodeMissingRequiredField)
		}
		return nil
	})
}

func decodeParams[T any](raw json.RawMessage, name string, validate func(*T) *JSONRPCError) (T, *JSONRPCError) {
	return decodeParamsInternal(raw, name, validate, false)
}

// decodeParamsOptional 与 decodeParams 相同，但允许 params 为空或 null（适用于可选参数方法）。
func decodeParamsOptional[T any](raw json.RawMessage, name string, validate func(*T) *JSONRPCError) (T, *JSONRPCError) {
	return decodeParamsInternal(raw, name, validate, true)
}

func decodeParamsInternal[T any](raw json.RawMessage, name string, validate func(*T) *JSONRPCError, allowEmpty bool) (T, *JSONRPCError) {
	var zero T
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if allowEmpty {
			return zero, nil
		}
		return zero, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}
	var params T
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return zero, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			fmt.Sprintf("invalid params for %s", name),
			GatewayCodeInvalidFrame,
		)
	}
	if validate != nil {
		if err := validate(&params); err != nil {
			return zero, err
		}
	}
	return params, nil
}

// cloneJSONRawMessage 复制 RawMessage，避免共享底层切片导致的并发风险。
func cloneJSONRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}
