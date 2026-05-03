import { create } from 'zustand'
import { type TokenUsage, type PermissionRequestPayload, type PlanApprovalRequestPayload } from '@/api/protocol'

/** 聊天消息 */
export interface ChatMessage {
  id: string
  /** 消息角色：user / assistant / tool */
  role: 'user' | 'assistant' | 'tool'
  /** 消息类型：text / thinking / tool_call / code / welcome / system */
  type: 'text' | 'thinking' | 'tool_call' | 'code' | 'welcome' | 'system'
  /** 文本内容 */
  content: string
  /** 工具调用信息 */
  toolName?: string
  toolCallId?: string
  toolArgs?: string
  toolResult?: string
  toolStatus?: 'running' | 'done' | 'error'
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
  /** 权限请求列表 */
  permissionRequests: PermissionRequestPayload[]
  /** 计划审批请求 */
  planApprovalRequest: PlanApprovalRequestPayload | null
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

  // Actions
  addMessage: (msg: ChatMessage) => void
  removeMessage: (id: string) => void
  appendChunk: (text: string) => void
  /** 原子操作：创建流式 assistant 消息 + 加入列表 + 设置 streamingMessageId */
  startStreamingMessage: () => string
  finalizeMessage: (id: string, content: string) => void
  updateToolCall: (toolCallId: string, result: string, status: ChatMessage['toolStatus']) => void
  appendToolOutput: (toolCallId: string, chunk: string) => void
  setGenerating: (v: boolean) => void
  setStreamingMessageId: (id: string) => void
  /** 重置生成状态：终结当前流式消息 + 清除 isGenerating */
  resetGeneratingState: () => void
  setTransitioning: (v: boolean) => void
  addPermissionRequest: (req: PermissionRequestPayload) => void
  removePermissionRequest: (requestId: string) => void
  setPlanApprovalRequest: (req: PlanApprovalRequestPayload) => void
  clearPlanApprovalRequest: () => void
  updateTokenUsage: (usage: TokenUsage) => void
  setPhase: (phase: string) => void
  setStopReason: (reason: string) => void
  clearMessages: () => void
  addSystemMessage: (content: string) => void
  setAgentMode: (mode: 'build' | 'plan') => void
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

export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  isGenerating: false,
  streamingMessageId: '',
  permissionRequests: [],
  planApprovalRequest: null,
  tokenUsage: null,
  phase: '',
  stopReason: '',
  isTransitioning: false,
  agentMode: 'build',

  addMessage: (msg) => set((s) => ({ messages: [...s.messages, msg] })),
  removeMessage: (id) => set((s) => ({ messages: s.messages.filter((m) => m.id !== id) })),

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

  setGenerating: (isGenerating) => set({ isGenerating }),
  setStreamingMessageId: (streamingMessageId) => set({ streamingMessageId }),

  /** 重置生成状态：终结当前流式消息 + 清除 isGenerating */
  resetGeneratingState: () =>
    set((s) => {
      if (s.streamingMessageId) {
        return {
          messages: s.messages.map((m) =>
            m.id === s.streamingMessageId ? { ...m, streaming: false } : m
          ),
          streamingMessageId: '',
          isGenerating: false,
        }
      }
      return { isGenerating: false }
    }),

  setTransitioning: (isTransitioning) => set({ isTransitioning }),

  addPermissionRequest: (req) =>
    set((s) => ({ permissionRequests: [...s.permissionRequests, req] })),

  removePermissionRequest: (requestId) =>
    set((s) => ({
      permissionRequests: s.permissionRequests.filter((r) => r.request_id !== requestId),
    })),

  setPlanApprovalRequest: (planApprovalRequest) => set({ planApprovalRequest }),
  clearPlanApprovalRequest: () => set({ planApprovalRequest: null }),

  updateTokenUsage: (tokenUsage) => set({ tokenUsage }),
  setPhase: (phase) => set({ phase }),
  setStopReason: (stopReason) => set({ stopReason }),

  addSystemMessage: (content) =>
    set((s) => ({ messages: [...s.messages, createSystemMessage(content)] })),

  setAgentMode: (agentMode) => set({ agentMode }),

  /** 清理全部聊天状态，包括权限请求、token用量等 */
  clearMessages: () => set({
    messages: [],
    streamingMessageId: '',
    isGenerating: false,
    permissionRequests: [],
    planApprovalRequest: null,
    tokenUsage: null,
    phase: '',
    stopReason: '',
    isTransitioning: false,
    agentMode: 'build',
  }),
}))
