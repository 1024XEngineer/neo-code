/**
 * Gateway JSON-RPC 协议常量，从 Go internal/gateway/protocol/jsonrpc.go 对齐。
 */

// JSON-RPC 版本
export const JSONRPC_VERSION = '2.0'

// RPC 方法名
export const Method = {
  Authenticate: 'gateway.authenticate',
  Ping: 'gateway.ping',
  BindStream: 'gateway.bindStream',
  Run: 'gateway.run',
  Cancel: 'gateway.cancel',
  Compact: 'gateway.compact',
  ListSessions: 'gateway.listSessions',
  LoadSession: 'gateway.loadSession',
  ResolvePermission: 'gateway.resolvePermission',
  ExecuteSystemTool: 'gateway.executeSystemTool',
  ActivateSessionSkill: 'gateway.activateSessionSkill',
  DeactivateSessionSkill: 'gateway.deactivateSessionSkill',
  ListSessionSkills: 'gateway.listSessionSkills',
  ListAvailableSkills: 'gateway.listAvailableSkills',
  DeleteSession: 'gateway.deleteSession',
  RenameSession: 'gateway.renameSession',
  ListFiles: 'gateway.listFiles',
  ListModels: 'gateway.listModels',
  SetSessionModel: 'gateway.setSessionModel',
  GetSessionModel: 'gateway.getSessionModel',
  ListProviders: 'gateway.listProviders',
  CreateCustomProvider: 'gateway.createCustomProvider',
  DeleteCustomProvider: 'gateway.deleteCustomProvider',
  SelectProviderModel: 'gateway.selectProviderModel',
  ListMCPServers: 'gateway.listMCPServers',
  UpsertMCPServer: 'gateway.upsertMCPServer',
  SetMCPServerEnabled: 'gateway.setMCPServerEnabled',
  DeleteMCPServer: 'gateway.deleteMCPServer',
  Event: 'gateway.event',
  ListWorkspaces: 'gateway.listWorkspaces',
  CreateWorkspace: 'gateway.createWorkspace',
  SwitchWorkspace: 'gateway.switchWorkspace',
  RenameWorkspace: 'gateway.renameWorkspace',
  DeleteWorkspace: 'gateway.deleteWorkspace',
} as const

// 帧类型
export const FrameType = {
  Ack: 'ack',
  Error: 'error',
  Event: 'event',
} as const

// 帧动作
export const FrameAction = {
  Run: 'run',
  ListProviders: 'list_providers',
  CreateCustomProvider: 'create_custom_provider',
  DeleteCustomProvider: 'delete_custom_provider',
  SelectProviderModel: 'select_provider_model',
  ListMCPServers: 'list_mcp_servers',
  UpsertMCPServer: 'upsert_mcp_server',
  SetMCPServerEnabled: 'set_mcp_server_enabled',
  DeleteMCPServer: 'delete_mcp_server',
} as const

// 运行时事件类型（从 Go internal/tui/services/runtime_contract.go 对齐）
export const EventType = {
  UserMessage: 'user_message',
  AgentChunk: 'agent_chunk',
  AgentDone: 'agent_done',
  ToolStart: 'tool_start',
  ToolResult: 'tool_result',
  ToolChunk: 'tool_chunk',
  ToolCallThinking: 'tool_call_thinking',
  RunCanceled: 'run_canceled',
  Error: 'error',
  PermissionRequested: 'permission_requested',
  PermissionResolved: 'permission_resolved',
  CompactStart: 'compact_start',
  CompactApplied: 'compact_applied',
  CompactError: 'compact_error',
  TokenUsage: 'token_usage',
  PhaseChanged: 'phase_changed',
  StopReasonDecided: 'stop_reason_decided',
  InputNormalized: 'input_normalized',
  SkillActivated: 'skill_activated',
  SkillDeactivated: 'skill_deactivated',
  SkillMissing: 'skill_missing',
  TodoUpdated: 'todo_updated',
  TodoConflict: 'todo_conflict',
  TodoSummaryInjected: 'todo_summary_injected',
  AssetSaved: 'asset_saved',
  AssetSaveFailed: 'asset_save_failed',
  ProgressEvaluated: 'progress_evaluated',
  VerificationStarted: 'verification_started',
  VerificationStageFinished: 'verification_stage_finished',
  VerificationFinished: 'verification_finished',
  VerificationCompleted: 'verification_completed',
  VerificationFailed: 'verification_failed',
  AcceptanceDecided: 'acceptance_decided',
} as const

// 权限审批决策
export const PermissionDecision = {
  AllowOnce: 'allow_once',
  AllowSession: 'allow_session',
  Reject: 'reject',
} as const

// 停止原因
export const StopReason = {
  UserInterrupt: 'user_interrupt',
  FatalError: 'fatal_error',
  BudgetExceeded: 'budget_exceeded',
  MaxTurnExceeded: 'max_turn_exceeded',
  Accepted: 'accepted',
  RetryExhausted: 'retry_exhausted',
} as const

// --- 类型定义 ---

/** JSON-RPC 响应 */
export interface JSONRPCResponse {
  jsonrpc: typeof JSONRPC_VERSION
  id: string | number
  result?: unknown
  error?: JSONRPCError | null
}

/** JSON-RPC 错误 */
export interface JSONRPCError {
  code: number
  message: string
  data?: { gateway_code?: string }
}

/** JSON-RPC 通知（服务端推送） */
export interface JSONRPCNotification {
  jsonrpc: typeof JSONRPC_VERSION
  method: string
  params?: unknown
}

/** 网关消息帧 */
export interface MessageFrame {
  type: string
  action?: string
  session_id?: string
  run_id?: string
  payload?: unknown
}

/** 通用 RPC 响应包装（MessageFrame 格式） */
export interface RPCResult<T> {
  type: string
  action: string
  session_id?: string
  run_id?: string
  payload: T
}

/** 运行时事件包裹 */
export interface RuntimeEventEnvelope {
  runtime_event_type: string
  turn?: number
  phase?: string
  timestamp?: string
  payload_version?: number
  payload?: unknown
}

/** gateway.authenticate 参数 */
export interface AuthenticateParams {
  token: string
}

/** gateway.bindStream 参数 */
export interface BindStreamParams {
  session_id: string
  run_id?: string
  channel?: string
}

/** gateway.run 参数 */
export interface RunParams {
  session_id?: string
  new_session?: boolean
  run_id?: string
  input_text?: string
  input_parts?: RunInputPart[]
  workdir?: string
  mode?: string
}

/** gateway.run 输入分片 */
export interface RunInputPart {
  type: string
  text?: string
  media?: { uri: string; mime_type: string; file_name?: string }
}

/** gateway.cancel 参数 */
export interface CancelParams {
  session_id?: string
  run_id: string
}

/** gateway.loadSession 参数 */
export interface LoadSessionParams {
  session_id: string
}

/** gateway.resolvePermission 参数 */
export interface ResolvePermissionParams {
  request_id: string
  decision: string
}

/** 会话摘要 */
export interface SessionSummary {
  id: string
  title: string
  created_at: string
  updated_at: string
}

/** 会话消息 */
export interface SessionMessage {
  role: string
  content: string
  tool_calls?: ToolCall[]
  tool_call_id?: string
  is_error?: boolean
}

/** 工具调用 */
export interface ToolCall {
  id: string
  name: string
  arguments: string
}

/** 会话详情 */
export interface Session {
  id: string
  title: string
  created_at: string
  updated_at: string
  workdir?: string
  provider?: string
  model?: string
  agent_mode?: string
  messages?: SessionMessage[]
}

/** Token 用量 */
export interface TokenUsage {
  input_tokens: number
  output_tokens: number
  input_source?: string
  output_source?: string
  has_unknown_usage?: boolean
  session_input_tokens: number
  session_output_tokens: number
}

/** gateway.run ack 响应 */
export type RunAckResult = RPCResult<{ message: string }>

/** gateway.listSessions 响应 */
export type ListSessionsResult = RPCResult<{ sessions: SessionSummary[] }>

/** gateway.cancel 响应 */
export type CancelResult = RPCResult<{ canceled: boolean; run_id: string }>

/** gateway.deleteSession 参数 */
export interface DeleteSessionParams {
  session_id: string
}

/** gateway.deleteSession 响应 */
export type DeleteSessionResult = RPCResult<{ deleted: boolean; session_id: string }>

/** gateway.renameSession 参数 */
export interface RenameSessionParams {
  session_id: string
  title: string
}

/** gateway.renameSession 响应 */
export type RenameSessionResult = RPCResult<{ session_id: string; title: string }>

/** gateway.listFiles 参数 */
export interface ListFilesParams {
  session_id?: string
  workdir?: string
  path?: string
}

/** 文件条目 */
export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size?: number
  mod_time?: string
}

/** gateway.listFiles 响应 */
export type ListFilesResult = RPCResult<{ files: FileEntry[] }>

/** 模型条目 */
export interface ModelEntry {
  id: string
  name: string
  provider: string
}

/** gateway.listModels 响应 */
export type ListModelsResult = RPCResult<{ models: ModelEntry[] }>

/** gateway.setSessionModel 参数 */
export interface SetSessionModelParams {
  session_id: string
  provider_id?: string
  model_id: string
}

/** gateway.setSessionModel 响应 */
export type SetSessionModelResult = RPCResult<{ session_id: string; model_id: string }>

/** gateway.getSessionModel 参数 */
export interface GetSessionModelParams {
  session_id: string
}

/** gateway.getSessionModel 响应 */
export type GetSessionModelResult = RPCResult<{ model_id: string; model_name?: string; provider?: string }>

/** 模型能力提示 */
export interface ProviderModelCapabilityHints {
  tool_calling?: string
  image_input?: string
}

/** Provider 模型描述 */
export interface ProviderModelDescriptor {
  id: string
  name: string
  description?: string
  context_window?: number
  max_output_tokens?: number
  capability_hints?: ProviderModelCapabilityHints
}

/** Provider 选项 */
export interface ProviderOption {
  id: string
  name: string
  driver: string
  base_url?: string
  api_key_env: string
  source: string
  selected: boolean
  models?: ProviderModelDescriptor[]
}

/** gateway.listProviders 响应 */
export type ListProvidersResult = RPCResult<{ providers: ProviderOption[] }>

/** gateway.createCustomProvider 参数 */
export interface CreateProviderParams {
  name: string
  driver: string
  base_url?: string
  chat_api_mode?: string
  chat_endpoint_path?: string
  api_key_env: string
  api_key?: string
  model_source?: string
  discovery_endpoint_path?: string
  models?: ProviderModelDescriptor[]
}

/** gateway.createCustomProvider 响应 */
export type CreateProviderResult = RPCResult<{ provider_id: string; model_id: string }>

/** gateway.deleteCustomProvider 参数 */
export interface DeleteProviderParams {
  provider_id: string
}

/** gateway.deleteCustomProvider 响应 */
export type DeleteProviderResult = RPCResult<{ deleted: boolean; provider_id: string }>

/** gateway.selectProviderModel 参数 */
export interface SelectProviderModelParams {
  provider_id: string
  model_id?: string
}

/** gateway.selectProviderModel 响应 */
export type SelectProviderModelResult = RPCResult<{ provider_id: string; model_id: string }>

/** MCP server stdio 参数 */
export interface MCPStdioParams {
  command?: string
  args?: string[]
  workdir?: string
  start_timeout_sec?: number
  call_timeout_sec?: number
  restart_backoff_sec?: number
}

/** MCP server 环境变量 */
export interface MCPEnvVarParams {
  name: string
  value?: string
  value_env?: string
}

/** MCP server 配置 */
export interface MCPServerParams {
  id: string
  enabled?: boolean
  source?: string
  version?: string
  stdio?: MCPStdioParams
  env?: MCPEnvVarParams[]
}

/** gateway.listMCPServers 响应 */
export type ListMCPServersResult = RPCResult<{ servers: MCPServerParams[] }>

/** gateway.upsertMCPServer 参数 */
export interface UpsertMCPServerParams {
  server: MCPServerParams
}

/** gateway.upsertMCPServer 响应 */
export type UpsertMCPServerResult = RPCResult<{ server: MCPServerParams }>

/** gateway.setMCPServerEnabled 参数 */
export interface SetMCPServerEnabledParams {
  id: string
  enabled: boolean
}

/** gateway.setMCPServerEnabled 响应 */
export type SetMCPServerEnabledResult = RPCResult<{ id: string; enabled: boolean }>

/** gateway.deleteMCPServer 参数 */
export interface DeleteMCPServerParams {
  id: string
}

/** gateway.deleteMCPServer 响应 */
export type DeleteMCPServerResult = RPCResult<{ deleted: boolean; id: string }>

/** 技能来源元信息 */
export interface SkillSource {
  kind: string
  layer?: string
  root_dir?: string
  skill_dir?: string
  file_path?: string
}

/** 技能描述元信息 */
export interface SkillDescriptor {
  id: string
  name?: string
  description?: string
  version?: string
  source?: SkillSource
  scope?: string
}

/** 会话技能状态 */
export interface SessionSkillState {
  skill_id: string
  missing?: boolean
  descriptor?: SkillDescriptor
}

/** 可用技能状态 */
export interface AvailableSkillState {
  descriptor: SkillDescriptor
  active: boolean
}

/** gateway.activateSessionSkill 参数 */
export interface ActivateSessionSkillParams {
  session_id: string
  skill_id: string
}

/** gateway.activateSessionSkill 响应 */
export type ActivateSessionSkillResult = RPCResult<{ session_id: string; skill_id: string; message: string }>

/** gateway.deactivateSessionSkill 参数 */
export interface DeactivateSessionSkillParams {
  session_id: string
  skill_id: string
}

/** gateway.deactivateSessionSkill 响应 */
export type DeactivateSessionSkillResult = RPCResult<{ session_id: string; skill_id: string; message: string }>

/** gateway.listSessionSkills 参数 */
export interface ListSessionSkillsParams {
  session_id: string
}

/** gateway.listSessionSkills 响应 */
export type ListSessionSkillsResult = RPCResult<{ skills: SessionSkillState[] }>

/** gateway.listAvailableSkills 参数 */
export interface ListAvailableSkillsParams {
  session_id?: string
}

/** gateway.listAvailableSkills 响应 */
export type ListAvailableSkillsResult = RPCResult<{ skills: AvailableSkillState[] }>

/** 权限请求载荷 */
export interface PermissionRequestPayload {
  request_id: string
  tool_call_id: string
  tool_name: string
  tool_category: string
  action_type: string
  operation: string
  target_type: string
  target: string
  decision: string
  reason: string
  rule_id?: string
}

/** 工作区记录 */
export interface Workspace {
  hash: string
  path: string
  name: string
  created_at: string
  updated_at: string
}

/** gateway.listWorkspaces 响应 */
export type ListWorkspacesResult = RPCResult<{ workspaces: Workspace[] }>

/** gateway.createWorkspace 参数 */
export interface CreateWorkspaceParams {
  path: string
  name?: string
}

/** gateway.createWorkspace 响应 */
export type CreateWorkspaceResult = RPCResult<{ workspace: Workspace }>

/** gateway.switchWorkspace 参数 */
export interface SwitchWorkspaceParams {
  workspace_hash: string
}

/** gateway.switchWorkspace 响应 */
export type SwitchWorkspaceResult = RPCResult<{ workspace_hash: string }>

/** gateway.renameWorkspace 参数 */
export interface RenameWorkspaceParams {
  workspace_hash: string
  name: string
}

/** gateway.renameWorkspace 响应 */
export type RenameWorkspaceResult = RPCResult<{ hash: string; name: string }>

/** gateway.deleteWorkspace 参数 */
export interface DeleteWorkspaceParams {
  workspace_hash: string
  remove_data?: boolean
}

/** gateway.deleteWorkspace 响应 */
export type DeleteWorkspaceResult = RPCResult<{ hash: string }>
