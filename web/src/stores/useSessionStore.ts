import { create } from 'zustand'
import { type GatewayAPI } from '@/api/gateway'
import { type SessionSummary as APISessionSummary } from '@/api/protocol'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { parseDateTime } from '@/utils/format'

/** 判断 sessionId 是否有效（非空且不是临时草稿前缀） */
export function isValidSessionId(id: string): boolean {
  return !!id && !id.startsWith('new_')
}

/** 会话摘要（UI 层展示用） */
export interface SessionSummary {
  id: string
  title: string
  time: string
  createdAt?: string
  updatedAt?: string
}

/** 项目分组 */
export interface Project {
  id: string
  name: string
  sessions: SessionSummary[]
}

/** 会话状态 */
interface SessionState {
  /** 项目列表 */
  projects: Project[]
  /** 当前活跃会话 ID */
  currentSessionId: string
  /** 当前活跃会话所在项目 ID */
  currentProjectId: string
  /** 是否正在加载 */
  loading: boolean
  /** 上一次 switchSession 的 abort controller */
  _switchAbort: AbortController | null
  /** 初始化时是否已完成 bindStream（避免 fetchSessions 和 initializeActiveSession 重复绑定） */
  _initialBindDone: boolean

  // Actions
  setProjects: (projects: Project[]) => void
  setCurrentSessionId: (id: string) => void
  setCurrentProjectId: (id: string) => void
  setLoading: (loading: boolean) => void
  /** 从后端拉取会话列表并映射为项目分组 */
  fetchSessions: (gatewayAPI: GatewayAPI, force?: boolean) => Promise<void>
  /** 切换到指定会话：清空消息 → 绑定流 → 加载历史消息 */
  switchSession: (sessionId: string, gatewayAPI: GatewayAPI) => Promise<void>
  /** 创建新会话：清空消息，等待 run 成功后由事件回写真实 session_id */
  createSession: () => void
  /** 初始化当前活跃会话：如存在有效会话则绑定流 */
  initializeActiveSession: (gatewayAPI: GatewayAPI) => Promise<void>
  /** 准备新的聊天输入状态 */
  prepareNewChat: () => void
  /** 重置内部状态（工作区切换时调用，确保 fetchSessions 不使用过期数据） */
  resetForWorkspaceSwitch: () => void
  /** 从本地 projects 列表中移除一个 session（乐观更新） */
  removeSessionLocally: (sessionId: string) => void
}

/** 将后端扁平会话列表映射为项目分组结构 */
function mapSessionsToProjects(apiSessions: APISessionSummary[]): Project[] {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const weekAgo = new Date(today.getTime() - 7 * 86400000)

  const groups: Record<string, APISessionSummary[]> = {
    '今天': [],
    '昨天': [],
    '最近7天': [],
    '更早': [],
  }

  for (const s of apiSessions) {
    const sessionTime = selectSessionDisplayTime(s)
    if (sessionTime >= today) {
      groups['今天'].push(s)
    } else if (sessionTime >= yesterday) {
      groups['昨天'].push(s)
    } else if (sessionTime >= weekAgo) {
      groups['最近7天'].push(s)
    } else {
      groups['更早'].push(s)
    }
  }

  const projects: Project[] = []
  for (const [name, sessions] of Object.entries(groups)) {
    if (sessions.length === 0) continue
    projects.push({
      id: `group_${name}`,
      name,
      sessions: sessions.map((s) => ({
        id: s.id,
        title: s.title || '未命名会话',
        time: selectSessionDisplayTime(s).toISOString(),
        createdAt: s.created_at,
        updatedAt: s.updated_at,
      })),
    })
  }

  return projects
}

/** 选择会话展示时间：优先取 created/updated 中较新的有效时间，规避单字段异常。 */
function selectSessionDisplayTime(session: APISessionSummary): Date {
  const created = parseDateTime(session.created_at)
  const updated = parseDateTime(session.updated_at)
  const createdValid = !isNaN(created.getTime())
  const updatedValid = !isNaN(updated.getTime())
  if (createdValid && updatedValid) {
    return updated.getTime() >= created.getTime() ? updated : created
  }
  if (updatedValid) return updated
  if (createdValid) return created
  // 当后端时间字段都无效时，不伪造“当前时间”，避免损坏记录冒充“刚刚更新”并扰乱排序。
  return new Date(0)
}

export type BackendMessage = {
  role: string
  content: string
  tool_calls?: Array<{ id: string; name: string; arguments: string }>
  tool_call_id?: string
  is_error?: boolean
}

/** 并发拉取 session 详情 + todos + checkpoints + runtime snapshot，并把后者写入对应 store。
 *  todos / checkpoints / runtime snapshot 失败用 .catch 兜底，不阻断主流程的 loadSession。 */
export async function loadSessionWithInsights(gatewayAPI: GatewayAPI, sessionId: string) {
  const [sessionFrame, todosResult, checkpointsResult, runtimeSnapshotResult] = await Promise.all([
    gatewayAPI.loadSession(sessionId),
    (gatewayAPI.listSessionTodos?.(sessionId) ?? Promise.resolve(null)).catch(() => null),
    (gatewayAPI.listCheckpoints?.({ session_id: sessionId, limit: 50 }) ?? Promise.resolve(null)).catch(() => null),
    (gatewayAPI.getRuntimeSnapshot?.(sessionId) ?? Promise.resolve(null)).catch(() => null),
  ])
  const insightStore = useRuntimeInsightStore.getState()
  if (todosResult?.payload) {
    insightStore.setTodoSnapshot(todosResult.payload)
  }
  if (checkpointsResult?.payload) {
    insightStore.setCheckpoints(checkpointsResult.payload)
  }
  const pendingQuestion = runtimeSnapshotResult?.payload?.pending_user_question
  if (pendingQuestion) {
    useChatStore.getState().setPendingUserQuestion(pendingQuestion)
  } else {
    useChatStore.getState().clearPendingUserQuestion()
  }
  return sessionFrame
}

/** isInternalHistoryMessage 识别仅供 runtime/provider 续跑使用、不能回放到 Web 聊天流的内部控制消息。 */
function isInternalHistoryMessage(msg: BackendMessage): boolean {
  const role = msg.role.trim().toLowerCase()
  if (role !== 'system' && role !== 'assistant') return false

  const content = msg.content.trim()
  if (!content) return false
  return /^<acceptance_continue\b[\s\S]*<\/acceptance_continue>$/.test(content)
}

/** 将后端历史消息映射为前端 ChatMessage 列表，正确合并 tool_result 回 tool_call */
export function mapHistoryMessages(backendMessages: BackendMessage[]): Array<ReturnType<typeof useChatStore.getState>['messages'][0]> {
  let _idCounter = 0
  // Phase 1: Collect tool results by tool_call_id
  const toolResults = new Map<string, { content: string; isError: boolean }>()
  for (const msg of backendMessages) {
    if (isInternalHistoryMessage(msg)) continue
    if (msg.tool_call_id) {
      toolResults.set(msg.tool_call_id, { content: msg.content, isError: !!msg.is_error })
    }
  }

  // Phase 2: Map messages, merging tool results into corresponding tool_calls
  const result: Array<ReturnType<typeof useChatStore.getState>['messages'][0]> = []
  for (const msg of backendMessages) {
    if (isInternalHistoryMessage(msg)) continue

    // Skip bare tool result messages — they are merged into tool_call messages
    if (msg.tool_call_id) continue

    if (msg.tool_calls && msg.tool_calls.length > 0) {
      // If assistant message also has text content, emit that first
      if (msg.content && msg.role === 'assistant') {
        result.push({
          id: `hist_${Date.now()}_${_idCounter++}`,
          role: 'assistant',
          type: 'text',
          content: msg.content,
          timestamp: Date.now(),
        })
      }
      // Map each tool call, merging its result if available
      for (const tc of msg.tool_calls) {
        const tr = toolResults.get(tc.id)
        result.push({
          id: `hist_tc_${tc.id}_${_idCounter++}`,
          role: 'tool',
          type: 'tool_call',
          content: '',
          toolName: tc.name,
          toolCallId: tc.id,
          toolArgs: tc.arguments,
          toolResult: tr?.content,
          toolStatus: tr ? (tr.isError ? 'error' as const : 'done' as const) : 'done' as const,
          timestamp: Date.now(),
        })
      }
    } else {
      result.push({
        id: `hist_${msg.role}_${Date.now()}_${_idCounter++}`,
        role: (msg.role as 'user' | 'assistant' | 'tool') || 'assistant',
        type: 'text',
        content: msg.content,
        timestamp: Date.now(),
      })
    }
  }
  return result
}

let _latestCheckpointRestoreReloadSeq = 0

/** beginCheckpointRestoreReloadSeq 申请一次 checkpoint 回退重载序号，用于丢弃过期重载结果。 */
export function beginCheckpointRestoreReloadSeq(): number {
  _latestCheckpointRestoreReloadSeq += 1
  return _latestCheckpointRestoreReloadSeq
}

/** checkpoint 回退后全量重载会话状态，统一刷新消息、insight 与文件变更面板。 */
export async function reloadSessionAfterCheckpointRestore(
  gatewayAPI: GatewayAPI,
  sessionId: string,
  reloadSeq: number = beginCheckpointRestoreReloadSeq(),
): Promise<boolean> {
  const normalizedSessionId = sessionId.trim()
  if (!normalizedSessionId) return false

  useUIStore.getState().clearFileChanges()
  useChatStore.getState().clearMessages()
  useRuntimeInsightStore.getState().reset()

  const sessionFrame = await loadSessionWithInsights(gatewayAPI, normalizedSessionId)
  if (reloadSeq !== _latestCheckpointRestoreReloadSeq) return false
  const sessionData = sessionFrame.payload as { messages?: BackendMessage[]; agent_mode?: string }

  if (sessionData.messages && sessionData.messages.length > 0) {
    const mapped = mapHistoryMessages(sessionData.messages)
    useChatStore.getState().setMessages(mapped)
  }

  const restoredMode = sessionData.agent_mode === 'plan' ? 'plan' : 'build'
  useChatStore.getState().setAgentMode(restoredMode)
  return true
}

let _fetchSessionsPromise: Promise<void> | null = null

export const useSessionStore = create<SessionState>((set, get) => ({
  projects: [],
  currentSessionId: '',
  currentProjectId: '',
  loading: false,
  _switchAbort: null,
  _initialBindDone: false,

  setProjects: (projects) => set({ projects }),
  setCurrentSessionId: (currentSessionId) => set({ currentSessionId }),
  setCurrentProjectId: (currentProjectId) => set({ currentProjectId }),
  setLoading: (loading) => set({ loading }),

  switchSession: async (sessionId: string, gatewayAPI: GatewayAPI) => {
    if (!sessionId) return

    // 生成中拒绝切换会话
    if (useChatStore.getState().isGenerating) {
      useUIStore.getState().showToast('Cannot switch session while generating; stop the current run first.', 'info')
      return
    }

    // Abort previous switchSession if still in progress
    const prevAbort = get()._switchAbort
    if (prevAbort) {
      prevAbort.abort()
    }
    const abortCtrl = new AbortController()
    set({ _switchAbort: abortCtrl, loading: true })

    const prevSessionId = get().currentSessionId

    try {
      // 1. Set transitioning flag and clear messages FIRST (before bindStream)
      const chatStore = useChatStore.getState()
      chatStore.setTransitioning(true)
      chatStore.clearMessages()
      useRuntimeInsightStore.getState().reset()

      // 2. Update session ID
      set({ currentSessionId: sessionId })

      // 3. Bind stream (events will be discarded due to isTransitioning)
      await gatewayAPI.bindStream({ session_id: sessionId, channel: 'all' })

      // 4. Load historical messages (concurrently fetch todos + checkpoints)
      const sessionFrame = await loadSessionWithInsights(gatewayAPI, sessionId)
      const sessionData = sessionFrame.payload as { messages?: BackendMessage[]; agent_mode?: string }

      // Check if this request was superseded
      if (abortCtrl.signal.aborted) return

      // 5. Load messages and stop transitioning
      if (sessionData.messages && sessionData.messages.length > 0) {
        const mapped = mapHistoryMessages(sessionData.messages)
        useChatStore.getState().setMessages(mapped)
      }
      // 恢复会话的 agent_mode
      const restoredMode = sessionData.agent_mode === 'plan' ? 'plan' : 'build'
      useChatStore.getState().setAgentMode(restoredMode)
      chatStore.setTransitioning(false)
    } catch (err) {
      if (abortCtrl.signal.aborted) return
      console.error('switchSession failed:', err)
      // Revert to previous session and re-bind its stream
      set({ currentSessionId: prevSessionId })
      if (isValidSessionId(prevSessionId)) {
        gatewayAPI.bindStream({ session_id: prevSessionId, channel: 'all' }).catch(() => {})
      }
      useChatStore.getState().setTransitioning(false)
    } finally {
      if (get()._switchAbort === abortCtrl) {
        set({ loading: false, _switchAbort: null })
      }
    }
  },

  createSession: () => {
    if (useChatStore.getState().isGenerating) {
      useUIStore.getState().showToast('Cannot start a new session while generating; stop the current run first.', 'info')
      return
    }
    useChatStore.getState().clearMessages()
    useRuntimeInsightStore.getState().reset()
    set({ currentSessionId: '', currentProjectId: '' })
  },

  initializeActiveSession: async (gatewayAPI) => {
    const state = get()
    const sessionId = state.currentSessionId
    // fetchSessions already binds the first session, so skip if already bound
    if (isValidSessionId(sessionId) && !state._initialBindDone) {
      try {
        await gatewayAPI.bindStream({ session_id: sessionId, channel: 'all' })
        set({ _initialBindDone: true })
      } catch (err) {
        console.error('initializeActiveSession bindStream failed:', err)
        useUIStore.getState().showToast('Failed to bind event stream; real-time messages may not arrive.', 'error')
      }
    }
  },

  prepareNewChat: () => {
    if (useChatStore.getState().isGenerating) {
      useUIStore.getState().showToast('Cannot start a new session while generating; stop the current run first.', 'info')
      return
    }
    useChatStore.getState().clearMessages()
    useRuntimeInsightStore.getState().reset()
    set({ currentSessionId: '', currentProjectId: '' })
  },

  resetForWorkspaceSwitch: () => {
    _fetchSessionsPromise = null
    set({ _initialBindDone: false, loading: false })
  },

  removeSessionLocally: (sessionId) => {
    const projects = get().projects
      .map((p) => ({ ...p, sessions: p.sessions.filter((s) => s.id !== sessionId) }))
      .filter((p) => p.sessions.length > 0)
    set({ projects })
  },

  fetchSessions: async (gatewayAPI, force) => {
    // 去重：若已有 fetch 在进行中，复用同一 promise（force 跳过去重）
    if (!force && _fetchSessionsPromise) return _fetchSessionsPromise

    _fetchSessionsPromise = (async () => {
      set({ loading: true })
      try {
        const result = await gatewayAPI.listSessions()
        const sessions = result.payload.sessions
        const projects = mapSessionsToProjects(sessions)
        set({ projects, loading: false })

        const state = get()
        if (!isValidSessionId(state.currentSessionId) && sessions.length > 0) {
          const firstSession = sessions[0]
          set({ currentSessionId: firstSession.id })
          try {
            await gatewayAPI.bindStream({ session_id: firstSession.id, channel: 'all' })
            set({ _initialBindDone: true })

            // Load historical messages for the auto-selected session (concurrently fetch todos + checkpoints)
            const sessionFrame = await loadSessionWithInsights(gatewayAPI, firstSession.id)
            const sessionData = sessionFrame.payload as { messages?: BackendMessage[]; agent_mode?: string }
            if (sessionData.messages && sessionData.messages.length > 0) {
              const mapped = mapHistoryMessages(sessionData.messages)
              useChatStore.getState().setMessages(mapped)
            }
            const restoredMode = sessionData.agent_mode === 'plan' ? 'plan' : 'build'
            useChatStore.getState().setAgentMode(restoredMode)
          } catch (err) {
            console.error('Auto bindStream or loadSession failed:', err)
            useUIStore.getState().showToast('Failed to load session', 'error')
          }
        }
      } catch (err) {
        console.error('fetchSessions failed:', err)
        set({ projects: [], loading: false })
      } finally {
        _fetchSessionsPromise = null
      }
    })()

    return _fetchSessionsPromise
  },
}))
