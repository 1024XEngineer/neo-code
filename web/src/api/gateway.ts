import { type WSClient } from './wsClient'
import {
  Method,
  type RPCResult,
  type AuthenticateParams,
  type BindStreamParams,
  type RunParams,
  type CancelParams,
  type LoadSessionParams,
  type ListSessionTodosParams,
  type ListSessionTodosResult,
  type ListCheckpointsParams,
  type ListCheckpointsResult,
  type RestoreCheckpointParams,
  type RestoreCheckpointResult,
  type UndoRestoreParams,
  type UndoRestoreResult,
  type CheckpointDiffParams,
  type CheckpointDiffResult,
  type ResolvePermissionParams,
  type Session,
  type RunAckResult,
  type ListSessionsResult,
  type CancelResult,
  type DeleteSessionParams,
  type DeleteSessionResult,
  type RenameSessionParams,
  type RenameSessionResult,
  type ListFilesParams,
  type ListFilesResult,
  type ListModelsResult,
  type SetSessionModelParams,
  type SetSessionModelResult,
  type GetSessionModelParams,
  type GetSessionModelResult,
  type ListProvidersResult,
  type CreateProviderParams,
  type CreateProviderResult,
  type DeleteProviderParams,
  type DeleteProviderResult,
  type SelectProviderModelParams,
  type SelectProviderModelResult,
  type ListMCPServersResult,
  type UpsertMCPServerParams,
  type UpsertMCPServerResult,
  type SetMCPServerEnabledParams,
  type SetMCPServerEnabledResult,
  type DeleteMCPServerParams,
  type DeleteMCPServerResult,
  type ActivateSessionSkillParams,
  type ActivateSessionSkillResult,
  type DeactivateSessionSkillParams,
  type DeactivateSessionSkillResult,
  type ListSessionSkillsParams,
  type ListSessionSkillsResult,
  type ListAvailableSkillsParams,
  type ListAvailableSkillsResult,
  type ListWorkspacesResult,
  type CreateWorkspaceParams,
  type CreateWorkspaceResult,
  type SwitchWorkspaceParams,
  type SwitchWorkspaceResult,
  type RenameWorkspaceParams,
  type RenameWorkspaceResult,
  type DeleteWorkspaceParams,
  type DeleteWorkspaceResult,
} from './protocol'

/** Gateway 业务 API 客户端，基于 WebSocket 全双工通道 */
export class GatewayAPI {
  private ws: WSClient

  constructor(ws: WSClient) {
    this.ws = ws
  }

  /** 认证，返回 ack 结果 */
  async authenticate(token: string) {
    return this.ws.call(Method.Authenticate, { token } satisfies AuthenticateParams)
  }

  /** 绑定事件流到指定会话 */
  async bindStream(params: BindStreamParams) {
    return this.ws.call(Method.BindStream, params)
  }

  /** 发起一次 run，返回 ack 含 session_id 和 run_id */
  async run(params: RunParams) {
    return this.ws.call<RunAckResult>(Method.Run, params)
  }

  /** 取消运行，返回取消结果 */
  async cancel(params: CancelParams) {
    return this.ws.call<CancelResult>(Method.Cancel, params)
  }

  /** 压缩上下文 */
  async compact(sessionId: string, runId: string) {
    return this.ws.call<RPCResult<{ message: string }>>(Method.Compact, { session_id: sessionId, run_id: runId })
  }

  /** 列出所有会话 */
  async listSessions() {
    return this.ws.call<ListSessionsResult>(Method.ListSessions)
  }

  /** 加载会话详情 */
  async loadSession(sessionId: string) {
    return this.ws.call<RPCResult<Session>>(Method.LoadSession, { session_id: sessionId } satisfies LoadSessionParams)
  }

  async listSessionTodos(sessionId: string) {
    return this.ws.call<ListSessionTodosResult>(Method.ListSessionTodos, { session_id: sessionId } satisfies ListSessionTodosParams)
  }

  async listCheckpoints(params: ListCheckpointsParams) {
    return this.ws.call<ListCheckpointsResult>(Method.ListCheckpoints, params)
  }

  async restoreCheckpoint(params: RestoreCheckpointParams) {
    return this.ws.call<RestoreCheckpointResult>(Method.RestoreCheckpoint, params)
  }

  async undoRestore(sessionId: string) {
    return this.ws.call<UndoRestoreResult>(Method.UndoRestore, { session_id: sessionId } satisfies UndoRestoreParams)
  }

  async checkpointDiff(params: CheckpointDiffParams) {
    return this.ws.call<CheckpointDiffResult>(Method.CheckpointDiff, params)
  }

  /** 解析权限请求 */
  async resolvePermission(params: ResolvePermissionParams) {
    return this.ws.call(Method.ResolvePermission, params)
  }

  /** 执行系统工具 */
  async executeSystemTool(sessionId: string, runId: string, toolName: string, args: any, workdir?: string) {
    return this.ws.call(Method.ExecuteSystemTool, {
      session_id: sessionId,
      run_id: runId,
      tool_name: toolName,
      arguments: args,
      workdir,
    })
  }

  /** Ping 网关 */
  async ping() {
    return this.ws.call(Method.Ping)
  }

  /** 删除/归档会话 */
  async deleteSession(sessionId: string) {
    return this.ws.call<DeleteSessionResult>(Method.DeleteSession, { session_id: sessionId } satisfies DeleteSessionParams)
  }

  /** 重命名会话 */
  async renameSession(sessionId: string, title: string) {
    return this.ws.call<RenameSessionResult>(Method.RenameSession, { session_id: sessionId, title } satisfies RenameSessionParams)
  }

  /** 列出工作目录文件树 */
  async listFiles(params: ListFilesParams = {}) {
    return this.ws.call<ListFilesResult>(Method.ListFiles, params)
  }

  /** 列出可用模型 */
  async listModels(sessionId?: string) {
    return this.ws.call<ListModelsResult>(Method.ListModels, sessionId ? { session_id: sessionId } : undefined)
  }

  /** 设置会话模型 */
  async setSessionModel(sessionId: string, modelId: string, providerId?: string) {
    const params: SetSessionModelParams = { session_id: sessionId, model_id: modelId }
    if (providerId) params.provider_id = providerId
    return this.ws.call<SetSessionModelResult>(Method.SetSessionModel, params)
  }

  /** 获取当前会话模型 */
  async getSessionModel(sessionId: string) {
    return this.ws.call<GetSessionModelResult>(Method.GetSessionModel, { session_id: sessionId } satisfies GetSessionModelParams)
  }

  /** 列出可管理 provider */
  async listProviders() {
    return this.ws.call<ListProvidersResult>(Method.ListProviders)
  }

  /** 创建自定义 provider */
  async createCustomProvider(params: CreateProviderParams) {
    return this.ws.call<CreateProviderResult>(Method.CreateCustomProvider, params)
  }

  /** 删除自定义 provider */
  async deleteCustomProvider(providerId: string) {
    return this.ws.call<DeleteProviderResult>(Method.DeleteCustomProvider, { provider_id: providerId } satisfies DeleteProviderParams)
  }

  /** 全局选择 provider/model */
  async selectProviderModel(params: SelectProviderModelParams) {
    return this.ws.call<SelectProviderModelResult>(Method.SelectProviderModel, params)
  }

  /** 列出 MCP server 配置 */
  async listMCPServers() {
    return this.ws.call<ListMCPServersResult>(Method.ListMCPServers)
  }

  /** 新增或更新 MCP server */
  async upsertMCPServer(params: UpsertMCPServerParams) {
    return this.ws.call<UpsertMCPServerResult>(Method.UpsertMCPServer, params)
  }

  /** 启停 MCP server */
  async setMCPServerEnabled(id: string, enabled: boolean) {
    return this.ws.call<SetMCPServerEnabledResult>(Method.SetMCPServerEnabled, { id, enabled } satisfies SetMCPServerEnabledParams)
  }

  /** 删除 MCP server */
  async deleteMCPServer(id: string) {
    return this.ws.call<DeleteMCPServerResult>(Method.DeleteMCPServer, { id } satisfies DeleteMCPServerParams)
  }

  /** 查询当前可用技能列表 */
  async listAvailableSkills(sessionId?: string) {
    return this.ws.call<ListAvailableSkillsResult>(Method.ListAvailableSkills, sessionId ? { session_id: sessionId } satisfies ListAvailableSkillsParams : undefined)
  }

  /** 查询指定会话的激活技能列表 */
  async listSessionSkills(sessionId: string) {
    return this.ws.call<ListSessionSkillsResult>(Method.ListSessionSkills, { session_id: sessionId } satisfies ListSessionSkillsParams)
  }

  /** 在指定会话中激活一个技能 */
  async activateSessionSkill(sessionId: string, skillId: string) {
    return this.ws.call<ActivateSessionSkillResult>(Method.ActivateSessionSkill, { session_id: sessionId, skill_id: skillId } satisfies ActivateSessionSkillParams)
  }

  /** 在指定会话中停用一个技能 */
  async deactivateSessionSkill(sessionId: string, skillId: string) {
    return this.ws.call<DeactivateSessionSkillResult>(Method.DeactivateSessionSkill, { session_id: sessionId, skill_id: skillId } satisfies DeactivateSessionSkillParams)
  }

  /** 列出所有工作区 */
  async listWorkspaces() {
    return this.ws.call<ListWorkspacesResult>(Method.ListWorkspaces)
  }

  /** 创建工作区 */
  async createWorkspace(path: string, name?: string) {
    return this.ws.call<CreateWorkspaceResult>(Method.CreateWorkspace, { path, name } satisfies CreateWorkspaceParams)
  }

  /** 切换工作区 */
  async switchWorkspace(workspaceHash: string) {
    return this.ws.call<SwitchWorkspaceResult>(Method.SwitchWorkspace, { workspace_hash: workspaceHash } satisfies SwitchWorkspaceParams)
  }

  /** 重命名工作区 */
  async renameWorkspace(workspaceHash: string, name: string) {
    return this.ws.call<RenameWorkspaceResult>(Method.RenameWorkspace, { workspace_hash: workspaceHash, name } satisfies RenameWorkspaceParams)
  }

  /** 删除工作区 */
  async deleteWorkspace(workspaceHash: string, removeData?: boolean) {
    return this.ws.call<DeleteWorkspaceResult>(Method.DeleteWorkspace, { workspace_hash: workspaceHash, remove_data: removeData } satisfies DeleteWorkspaceParams)
  }
}
