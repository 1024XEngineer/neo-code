import { type ChatMessage } from '@/stores/useChatStore'

/**
 * 在消息列表中找到给定用户消息发送前最近一次 tool_call 的 checkpoint。
 *
 * 由于 checkpoint 在协议层挂载在 tool_call 消息上（由 `_latestDoneToolCallId` 时序关联），
 * "回退到 user message U_n 发送时的状态" 等价于"恢复 U_n 之前最近一条带 checkpointId 的 tool_call 的 cp"。
 *
 * 第一条 user message 之前没有 cp 时返回 null —— 调用方据此决定是否渲染 revert 按钮。
 */
export function findCheckpointBeforeMessage(
  messages: ChatMessage[],
  userMessageId: string,
): { checkpointId: string } | null {
  const idx = messages.findIndex((m) => m.id === userMessageId)
  if (idx <= 0) return null
  for (let i = idx - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.type === 'tool_call' && m.checkpointId) {
      return { checkpointId: m.checkpointId }
    }
  }
  return null
}
