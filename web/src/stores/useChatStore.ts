import { create } from 'zustand'
import { type TokenUsage, type PermissionRequestPayload, type AcceptanceDecidedPayload } from '@/api/protocol'
import { type VerificationRunRecord } from '@/stores/useRuntimeInsightStore'
import { resetEventBridgeCursors } from '@/utils/eventBridge'

/** 聊天消息 */
export interface ChatMessage {
  id: string
  /** 消息角色：user / assistant / tool */
  role: 'user' | 'assistant' | 'tool'
  /** 消息类型：text / thinking / tool_call / code / welcome / system / verification / acceptance */
  type: 'text' | 'thinking' | 'tool_call' | 'code' | 'welcome' | 'system' | 'verification' | 'acceptance'
  /** 文本内容 */
  content: string
  /** 工具调用信息 */
  toolName?: string
  toolCallId?: string
  toolArgs?: string
  toolResult?: string
  toolStatus?: 'running' | 'done' | 'error'
  /** 与该 tool_call 关联的 checkpoint ID(由 CheckpointCreated 事件时序关联) */
  checkpointId?: string
  /** Checkpoint 撤回状态:available 可撤回 / restoring 正在撤回 / restored 已撤回 */
  checkpointStatus?: 'available' | 'restoring' | 'restored'
  /** Verification 摘要数据(仅 type === 'verification' 使用) */
  verificationData?: VerificationRunRecord
  /** Acceptance 决策数据(仅 type === 'acceptance' 使用) */
  acceptanceData?: AcceptanceDecidedPayload
  /** Thinking 数据(仅 type === 'thinking' 使用) */
  thinkingData?: { collapsed: boolean }
  /** 代码语言 */
  language?: string
  /** 代码文件名 */
  filename?: string
  /** 时间戳 */
  timestamp: number
  /** 是否正在流式生成 */
  streaming?: boolean
}

/** 聊天状态 */
interface ChatState {
  /** 消息列表 */
  messages: ChatMessage[]
  /** 是否正在生成 */
  isGenerating: boolean
  /** 当前 AI 回复缓冲 ID（流式追加用） */
  streamingMessageId: string
  /** 当前 thinking 流式消息 ID */
  streamingThinkingMessageId: string
  /** 权限请求列表 */
  permissionRequests: PermissionRequestPayload[]
  /** Token 用量 */
  tokenUsage: TokenUsage | null
  /** 当前运行阶段 */
  phase: string
  /** 停止原因 */
  stopReason: string
  /** 会话切换中标记（eventBridge 据此丢弃中间窗口期事件） */
  isTransitioning: boolean
  /** 当前 Agent 工作模式 */
  agentMode: 'build' | 'plan'
  /** Build 模式下的权限审批策略 */
  permissionMode: 'default' | 'bypass'

  // Actions
  addMessage: (msg: ChatMessage) => void
  removeMessage: (id: string) => void
  /** 从指定消息（含）开始截断 messages 数组并清理生成相关状态 */
  truncateFromMessage: (messageId: string) => void
  appendChunk: (text: string) => void
  /** 原子操作：创建流式 assistant 消息 + 加入列表 + 设置 streamingMessageId */
  startStreamingMessage: () => string
  finalizeMessage: (id: string, content: string) => void
  /** 创建流式 thinking 消息并设置 streamingThinkingMessageId */
  startThinkingMessage: () => string
  /** 追加 thinking 文本到当前流式 thinking 消息 */
  appendThinkingChunk: (text: string) => void
  /** 终结 thinking 消息（collapsed=true）并清空 streamingThinkingMessageId */
  finalizeThinkingMessage: () => void
  updateToolCall: (toolCallId: string, result: string, status: ChatMessage['toolStatus']) => void
  appendToolOutput: (toolCallId: string, chunk: string) => void
  /** 把 checkpointId 关联到一条 tool_call 消息(由 CheckpointCreated 时序关联触发) */
  attachCheckpointToToolCall: (toolCallId: string, checkpointId: string) => void
  /** 更新某条已挂 checkpoint 的 tool_call 消息的撤回状态 */
  setCheckpointStatus: (toolCallId: string, status: NonNullable<ChatMessage['checkpointStatus']>) => void
  /** 将所有 available 的 checkpoint 标记为 restored */
  markAllCheckpointsRestored: () => void
  /** 将所有 restored 的 checkpoint 标记回 available */
  markAllCheckpointsAvailable: () => void
  /** 更新一条 verification 消息的 data(verification 进行中持续更新同一条消息) */
  updateVerificationMessage: (messageId: string, data: VerificationRunRecord) => void
  setGenerating: (v: boolean) => void
  setStreamingMessageId: (id: string) => void
  /** 重置生成状态：终结当前流式消息 + 清除 isGenerating */
  resetGeneratingState: () => void
  setTransitioning: (v: boolean) => void
  addPermissionRequest: (req: PermissionRequestPayload) => void
  removePermissionRequest: (requestId: string) => void
  updateTokenUsage: (usage: TokenUsage) => void
  setPhase: (phase: string) => void
  setStopReason: (reason: string) => void
  clearMessages: () => void
  addSystemMessage: (content: string) => void
  setAgentMode: (mode: 'build' | 'plan') => void
  setPermissionMode: (mode: 'default' | 'bypass') => void
}

let msgIdCounter = 0
function nextMsgId(): string {
  return `msg_${++msgIdCounter}_${Date.now()}`
}

/** 创建用户消息 */
export function createUserMessage(text: string): ChatMessage {
  return {
    id: nextMsgId(),
    role: 'user',
    type: 'text',
    content: text,
    timestamp: Date.now(),
  }
}

/** 创建 AI 流式消息 */
export function createAssistantMessage(): ChatMessage {
  return {
    id: nextMsgId(),
    role: 'assistant',
    type: 'text',
    content: '',
    timestamp: Date.now(),
    streaming: true,
  }
}

/** 创建系统消息（用于展示 slash command 执行结果） */
export function createSystemMessage(text: string): ChatMessage {
  return {
    id: nextMsgId(),
    role: 'assistant',
    type: 'system',
    content: text,
    timestamp: Date.now(),
  }
}

/** 创建工具调用消息 */
export function createToolCallMessage(toolName: string, toolCallId: string, args: string): ChatMessage {
  return {
    id: nextMsgId(),
    role: 'tool',
    type: 'tool_call',
    content: '',
    toolName,
    toolCallId,
    toolArgs: args,
    toolStatus: 'running',
    timestamp: Date.now(),
  }
}

/** 创建 thinking 流式消息 */
function createThinkingMessage(): ChatMessage {
  return {
    id: nextMsgId(),
    role: 'assistant',
    type: 'thinking',
    content: '',
    timestamp: Date.now(),
    streaming: true,
    thinkingData: { collapsed: false },
  }
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  isGenerating: false,
  streamingMessageId: '',
  streamingThinkingMessageId: '',
  permissionRequests: [],
  tokenUsage: null,
  phase: '',
  stopReason: '',
  isTransitioning: false,
  agentMode: 'build',
  permissionMode: 'default',

  addMessage: (msg) => set((s) => ({ messages: [...s.messages, msg] })),
  removeMessage: (id) => set((s) => ({ messages: s.messages.filter((m) => m.id !== id) })),

  truncateFromMessage: (messageId) =>
    set((s) => {
      const idx = s.messages.findIndex((m) => m.id === messageId)
      if (idx === -1) return s
      return {
        messages: s.messages.slice(0, idx),
        streamingMessageId: '',
        streamingThinkingMessageId: '',
        isGenerating: false,
        permissionRequests: [],
        phase: '',
        stopReason: '',
      }
    }),

  appendChunk: (text) =>
    set((s) => {
      if (!s.streamingMessageId) return s
      return {
        messages: s.messages.map((m) =>
          m.id === s.streamingMessageId ? { ...m, content: m.content + text } : m
        ),
      }
    }),

  /** 原子操作：创建消息 + 加入列表 + 设置 streamingMessageId，避免竞态 */
  startStreamingMessage: () => {
    const msg = createAssistantMessage()
    set((s) => ({
      messages: [...s.messages, msg],
      streamingMessageId: msg.id,
    }))
    return msg.id
  },

  /** 仅当 id 匹配当前 streamingMessageId 时才清空 */
  finalizeMessage: (id, content) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.id === id ? { ...m, content, streaming: false } : m
      ),
      streamingMessageId: s.streamingMessageId === id ? '' : s.streamingMessageId,
    })),

  startThinkingMessage: () => {
    const msg = createThinkingMessage()
    set((s) => ({
      messages: [...s.messages, msg],
      streamingThinkingMessageId: msg.id,
    }))
    return msg.id
  },

  appendThinkingChunk: (text) =>
    set((s) => {
      if (!s.streamingThinkingMessageId) return s
      return {
        messages: s.messages.map((m) =>
          m.id === s.streamingThinkingMessageId ? { ...m, content: m.content + text } : m
        ),
      }
    }),

  finalizeThinkingMessage: () =>
    set((s) => {
      if (!s.streamingThinkingMessageId) return s
      return {
        messages: s.messages.map((m) =>
          m.id === s.streamingThinkingMessageId
            ? { ...m, streaming: false, thinkingData: { collapsed: true } }
            : m
        ),
        streamingThinkingMessageId: '',
      }
    }),

  updateToolCall: (toolCallId, result, status) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.toolCallId === toolCallId ? { ...m, toolResult: result, toolStatus: status } : m
      ),
    })),

  appendToolOutput: (toolCallId, chunk) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.toolCallId === toolCallId
          ? { ...m, toolResult: (m.toolResult ?? '') + chunk }
          : m
      ),
    })),

  attachCheckpointToToolCall: (toolCallId, checkpointId) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.toolCallId === toolCallId && m.type === 'tool_call'
          ? { ...m, checkpointId, checkpointStatus: 'available' as const }
          : m
      ),
    })),

  setCheckpointStatus: (toolCallId, status) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.toolCallId === toolCallId && m.type === 'tool_call'
          ? { ...m, checkpointStatus: status }
          : m
      ),
    })),

  markAllCheckpointsRestored: () =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.checkpointStatus === 'available'
          ? { ...m, checkpointStatus: 'restored' as const }
          : m
      ),
    })),

  markAllCheckpointsAvailable: () =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.checkpointStatus === 'restored'
          ? { ...m, checkpointStatus: 'available' as const }
          : m
      ),
    })),

  updateVerificationMessage: (messageId, data) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.id === messageId && m.type === 'verification'
          ? { ...m, verificationData: data }
          : m
      ),
    })),

  setGenerating: (isGenerating) => set({ isGenerating }),
  setStreamingMessageId: (streamingMessageId) => set({ streamingMessageId }),

  /** 重置生成状态：终结当前流式消息 + 清除 isGenerating */
  resetGeneratingState: () =>
    set((s) => {
      let msgs = s.messages
      if (s.streamingMessageId) {
        msgs = msgs.map((m) =>
          m.id === s.streamingMessageId ? { ...m, streaming: false } : m
        )
      }
      if (s.streamingThinkingMessageId) {
        msgs = msgs.map((m) =>
          m.id === s.streamingThinkingMessageId
            ? { ...m, streaming: false, thinkingData: { collapsed: true } }
            : m
        )
      }
      return {
        messages: msgs,
        streamingMessageId: '',
        streamingThinkingMessageId: '',
        isGenerating: false,
      }
    }),

  setTransitioning: (isTransitioning) => set({ isTransitioning }),

  addPermissionRequest: (req) =>
    set((s) => ({ permissionRequests: [...s.permissionRequests, req] })),

  removePermissionRequest: (requestId) =>
    set((s) => ({
      permissionRequests: s.permissionRequests.filter((r) => r.request_id !== requestId),
    })),

  updateTokenUsage: (tokenUsage) => set({ tokenUsage }),
  setPhase: (phase) => set({ phase }),
  setStopReason: (stopReason) => set({ stopReason }),

  addSystemMessage: (content) =>
    set((s) => ({ messages: [...s.messages, createSystemMessage(content)] })),

  setAgentMode: (agentMode) => set({ agentMode }),
  setPermissionMode: (permissionMode) => set({ permissionMode }),

  /** 清理全部聊天状态，包括权限请求、token用量等。同时重置 eventBridge 模块级游标，避免跨会话泄漏。 */
  clearMessages: () => {
    resetEventBridgeCursors()
    set({
      messages: [],
      streamingMessageId: '',
      streamingThinkingMessageId: '',
      isGenerating: false,
      permissionRequests: [],
      tokenUsage: null,
      phase: '',
      stopReason: '',
      isTransitioning: false,
      agentMode: 'build',
      permissionMode: 'default',
    })
  },
}))
