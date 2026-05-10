import { useEffect, useMemo, useRef } from 'react'
import { useChatStore, type ChatMessage } from '@/stores/useChatStore'
import MessageItem from './MessageItem'

/** 判断消息是否属于"AI 回合"（用于把连续的 assistant/tool 视作同一叙述单元） */
function inAITurn(msg: ChatMessage): boolean {
  if (msg.role === 'tool') return true
  if (msg.role === 'assistant') return msg.type !== 'system' && msg.type !== 'welcome'
  return false
}

/** 是 AI 回合内的"过程"消息（工具调用、思考），渲染时优先置于"结论文本"之前 */
function isProcessMsg(msg: ChatMessage): boolean {
  return msg.role === 'tool' || msg.type === 'thinking'
}

/**
 * 在每个 AI 回合内重排消息：过程类（工具/思考）→ 结论类（assistant 文本/代码）。
 * 让最终生成的文本始终位于该回合末尾，避免被工具调用堆叠遮挡。
 * user / system / welcome 作为段分隔符，保持原位。
 */
function reorderForAITurn(messages: ChatMessage[]): ChatMessage[] {
  const out: ChatMessage[] = []
  let procBuf: ChatMessage[] = []
  let textBuf: ChatMessage[] = []

  const flush = () => {
    if (procBuf.length || textBuf.length) {
      out.push(...procBuf, ...textBuf)
      procBuf = []
      textBuf = []
    }
  }

  for (const m of messages) {
    if (!inAITurn(m)) {
      flush()
      out.push(m)
      continue
    }
    if (isProcessMsg(m)) procBuf.push(m)
    else textBuf.push(m)
  }
  flush()
  return out
}

/** 消息列表，自动滚动到底部（仅在用户位于底部时） */
export default function MessageList() {
  const messages = useChatStore((s) => s.messages)
  const isGenerating = useChatStore((s) => s.isGenerating)
  const containerRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const prevMessagesLengthRef = useRef<number>(0)
  const prevScrollTopRef = useRef<number>(0)
  const userScrolledUpRef = useRef<boolean>(false)

  const ordered = useMemo(() => reorderForAITurn(messages), [messages])

  // 获取滚动容器（通过 bottomRef 向上查找）
  const getScrollEl = (): HTMLElement | null =>
    (bottomRef.current?.closest('[data-scroll-root]') as HTMLElement | null)

  // 条件滚动到底部
  useEffect(() => {
    const prevMessagesLength = prevMessagesLengthRef.current
    const lastMsg = messages[messages.length - 1]
    const loadedHistoryFromEmpty = prevMessagesLength === 0 && messages.length > 0
    const userJustSent =
      prevMessagesLength > 0 && messages.length > prevMessagesLength && lastMsg?.role === 'user'
    prevMessagesLengthRef.current = messages.length

    const scrollEl = getScrollEl()

    // 通过 scrollTop 变化方向检测用户是否手动向上滚动：
    // scrollTop 减小 = 用户向上滚；scrollTop 增大/不变 = 自动跟滚或内容增长
    if (scrollEl) {
      const scrollTop = scrollEl.scrollTop
      if (scrollTop < prevScrollTopRef.current - 5) {
        userScrolledUpRef.current = true
      }
      // 用户滚回底部时恢复自动跟随
      const nearBottom = scrollEl.scrollHeight - scrollTop - scrollEl.clientHeight < 80
      if (nearBottom) {
        userScrolledUpRef.current = false
      }
      prevScrollTopRef.current = scrollTop
    }

    // 历史会话首次加载完成时直接定位到底部，避免进入会话后停留在顶部。
    if (loadedHistoryFromEmpty) {
      userScrolledUpRef.current = false
      bottomRef.current?.scrollIntoView({ behavior: 'instant' })
      return
    }

    // 用户发送新消息时重置暂停状态，强制滚到底部
    if (userJustSent) {
      userScrolledUpRef.current = false
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
      return
    }

    // 生成中且用户未手动上滚 → 自动跟随
    if (isGenerating && !userScrolledUpRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'instant' })
    }
  }, [ordered, isGenerating, messages.length])

  const styles: Record<string, React.CSSProperties> = {
    container: {
      display: 'flex',
      flexDirection: 'column',
      gap: 2,
      padding: '16px 0',
      maxWidth: 800,
      margin: '0 auto',
      width: '100%',
    },
    empty: {
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      height: '100%',
      gap: 12,
      padding: '40px 20px',
    },
    emptyIcon: {
      width: 56,
      height: 56,
      borderRadius: 'var(--radius-xl)',
      background: 'var(--accent-muted)',
      color: 'var(--accent)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      fontSize: 13,
      fontWeight: 700,
      fontFamily: 'var(--font-mono)',
    },
    emptyTitle: {
      fontSize: 18,
      fontWeight: 600,
      color: 'var(--text-primary)',
      margin: 0,
    },
    emptyDesc: {
      fontSize: 13,
      color: 'var(--text-secondary)',
      textAlign: 'center',
      maxWidth: 320,
      lineHeight: 1.6,
    },
    suggestions: {
      display: 'flex',
      flexWrap: 'wrap',
      gap: 8,
      justifyContent: 'center',
      marginTop: 8,
    },
    suggestionChip: {
      padding: '6px 14px',
      borderRadius: 'var(--radius-lg)',
      border: '1px solid var(--border-primary)',
      background: 'var(--bg-secondary)',
      color: 'var(--text-secondary)',
      fontSize: 12,
      cursor: 'pointer',
      fontFamily: 'var(--font-ui)',
      transition: 'all 0.15s',
    },
  }

  if (messages.length === 0) {
    return (
      <div style={styles.empty}>
        <div style={styles.emptyIcon}>NeoCode</div>
        <h3 style={styles.emptyTitle}>开始你的 AI 编程之旅</h3>
        <p style={styles.emptyDesc}>输入问题、描述需求或粘贴代码，我将帮你分析和处理。</p>
        <div style={styles.suggestions}>
          {['帮我重构这段代码', '解释一下这个报错', '生成一个 REST API', '优化数据库查询'].map((s) => (
            <button key={s} style={styles.suggestionChip} onClick={() => {}}>
              {s}
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div ref={containerRef} style={styles.container}>
      {ordered.map((msg, i) => {
        const prev = i > 0 ? ordered[i - 1] : null
        const groupedWithPrev = !!prev && inAITurn(prev) && inAITurn(msg)
        return (
          <MessageItem
            key={msg.id}
            message={msg}
            isLast={i === ordered.length - 1}
            groupedWithPrev={groupedWithPrev}
          />
        )
      })}
      <div ref={bottomRef} />
    </div>
  )
}
