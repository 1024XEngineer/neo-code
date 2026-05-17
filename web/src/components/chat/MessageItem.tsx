import { memo, useState } from 'react'
import { type ChatMessage } from '@/stores/useChatStore'
import ToolCallCard from './ToolCallCard'
import VerificationMessage from './VerificationMessage'
import AcceptanceMessage from './AcceptanceMessage'
import CodeBlock from './CodeBlock'
import MarkdownContent from './MarkdownContent'
import { Bot, ChevronRight, Info, Loader2 } from 'lucide-react'

interface MessageItemProps {
  message: ChatMessage
  isLast?: boolean
  /** 是否与上一条 AI/工具消息属于同一回合（同回合压缩 avatar 与上下间距） */
  groupedWithPrev?: boolean
}

/** 单条消息渲染 */
const MessageItem = memo(function MessageItem({ message, isLast = false, groupedWithPrev = false }: MessageItemProps) {
  if (message.type === 'system') {
    return <SystemMessage message={message} />
  }

  if (message.type === 'welcome') {
    return <WelcomeMessage message={message} />
  }

  if (message.type === 'thinking') {
    return <ThinkingMessage message={message} groupedWithPrev={groupedWithPrev} />
  }

  if (message.type === 'tool_call') {
    return <ToolCallCard message={message} groupedWithPrev={groupedWithPrev} />
  }

  if (message.type === 'verification') {
    return <VerificationMessage message={message} groupedWithPrev={groupedWithPrev} />
  }

  if (message.type === 'acceptance') {
    return <AcceptanceMessage message={message} groupedWithPrev={groupedWithPrev} />
  }

  if (message.type === 'code') {
    return (
      <AIMessage message={message} isLast={isLast} groupedWithPrev={groupedWithPrev}>
        <CodeBlock code={message.content} language={message.language || 'text'} filename={message.filename} />
      </AIMessage>
    )
  }

  if (message.role === 'user') {
    return <UserMessage message={message} />
  }

  return <AIMessage message={message} isLast={isLast} groupedWithPrev={groupedWithPrev} />
})

function UserMessage({ message }: { message: ChatMessage }) {
  return (
    <div style={styles.userRow} className="animate-slide-up user-row-hoverable">
      <div style={styles.userContent}>
        <div style={styles.userBubble}>{message.content}</div>
      </div>
    </div>
  )
}

function AIMessage({ message, isLast, children, groupedWithPrev = false }: { message: ChatMessage; isLast: boolean; children?: React.ReactNode; groupedWithPrev?: boolean }) {
  return (
    <div style={groupedWithPrev ? styles.aiRowGrouped : styles.aiRow} className="animate-slide-up">
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div style={styles.aiAvatar}>
          <Bot size={16} />
        </div>
      )}
      <div style={styles.aiContent}>
        {children || (
          <div style={styles.aiText}>
            <MarkdownContent content={message.content} streaming={message.streaming} />
            {isLast && !message.content && message.streaming && (
              <span style={styles.typing}>
                <span className="thinking-dot">.</span>
                <span className="thinking-dot">.</span>
                <span className="thinking-dot">.</span>
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function ThinkingMessage({ message, groupedWithPrev = false }: { message: ChatMessage; groupedWithPrev?: boolean }) {
  const collapsed = message.thinkingData?.collapsed ?? false
  const isStreaming = message.streaming === true
  const [manualExpanded, setManualExpanded] = useState<boolean | null>(null)

  // streaming 时展开，collapsed 且无手动覆盖时折叠
  const expanded = manualExpanded !== null ? manualExpanded : (isStreaming || !collapsed)

  return (
    <div style={groupedWithPrev ? styles.aiRowGrouped : styles.aiRow} className="animate-fade-in">
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div style={{ ...styles.aiAvatar, background: 'var(--warning)', color: '#fff' }}>
          <Bot size={16} />
        </div>
      )}
      <div style={styles.aiContent}>
        <button style={styles.thinkingToggle} onClick={() => setManualExpanded(!expanded)}>
          <span style={{ transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform 0.2s', display: 'flex' }}>
            <ChevronRight size={14} />
          </span>
          <span style={styles.thinkingLabel}>
            {isStreaming ? 'AI 正在思考...' : 'AI 思考过程'}
          </span>
          {isStreaming && <Loader2 size={12} className="animate-spin" style={{ marginLeft: 4 }} />}
        </button>
        {expanded && (
          <div style={styles.thinkingContent}>
            <pre style={{ margin: 0, fontFamily: 'var(--font-mono)', fontSize: 12, lineHeight: 1.7, whiteSpace: 'pre-wrap' }}>
              {message.content}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}

function SystemMessage({ message }: { message: ChatMessage }) {
  return (
    <div style={styles.systemRow} className="animate-fade-in">
      <div style={styles.systemBadge}>
        <Info size={12} />
        <span style={styles.systemLabel}>系统</span>
      </div>
      <pre style={styles.systemPre}>{message.content}</pre>
    </div>
  )
}

function WelcomeMessage({ message }: { message: ChatMessage }) {
  return (
    <div style={{ ...styles.aiRow, justifyContent: 'center' }} className="animate-slide-up">
      <div style={styles.welcomeCard}>
        <div style={styles.welcomeIcon}>NeoCode</div>
        <p style={styles.welcomeText}>{message.content}</p>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  userRow: {
    display: 'flex',
    justifyContent: 'flex-end',
    alignItems: 'flex-start',
    padding: '12px 0 10px',
    position: 'relative',
    gap: 6,
  },
  userContent: {
    maxWidth: '85%',
    minWidth: 0,
  },
  userBubble: {
    background: 'var(--user-bubble)',
    color: 'var(--user-bubble-text)',
    padding: '10px 14px',
    borderRadius: 'var(--radius-lg)',
    fontSize: 14,
    lineHeight: 1.6,
    border: '1px solid var(--border-primary)',
    maxWidth: '100%',
    whiteSpace: 'pre-wrap',
    overflowWrap: 'anywhere',
    wordBreak: 'break-word',
    textWrap: 'pretty' as any,
  },
  aiRow: {
    display: 'flex',
    gap: 10,
    padding: '8px 0 10px',
  },
  aiRowGrouped: {
    display: 'flex',
    gap: 10,
    padding: '2px 0',
  },
  avatarSpacer: {
    width: 28,
    flexShrink: 0,
  },
  aiAvatar: {
    width: 28,
    height: 28,
    borderRadius: 'var(--radius-md)',
    background: 'var(--accent-muted)',
    color: 'var(--accent)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    marginTop: 2,
  },
  aiContent: {
    flex: 1,
    minWidth: 0,
  },
  aiText: {
    fontSize: 14,
    lineHeight: 1.7,
    color: 'var(--text-primary)',
    textWrap: 'pretty' as any,
  },
  typing: {
    marginLeft: 4,
    color: 'var(--text-tertiary)',
  },
  thinkingToggle: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '4px 8px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-secondary)',
    fontSize: 12,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    marginBottom: 8,
  },
  thinkingLabel: {
    fontWeight: 500,
  },
  thinkingContent: {
    padding: '10px 12px',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-secondary)',
    marginBottom: 8,
  },
  welcomeCard: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 12,
    padding: '24px 32px',
    textAlign: 'center',
    maxWidth: 480,
  },
  welcomeIcon: {
    width: 48,
    height: 48,
    borderRadius: 'var(--radius-xl)',
    background: 'var(--accent-muted)',
    color: 'var(--accent)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 11,
    fontWeight: 700,
    fontFamily: 'var(--font-mono)',
  },
  welcomeText: {
    fontSize: 14,
    lineHeight: 1.7,
    color: 'var(--text-secondary)',
    margin: 0,
  },
  systemRow: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 6,
    padding: '10px 16px',
    margin: '4px 0',
  },
  systemBadge: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '3px 10px',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-tertiary)',
    fontSize: 11,
    fontWeight: 600,
  },
  systemLabel: {
    fontSize: 11,
    fontWeight: 600,
    letterSpacing: '0.5px',
  },
  systemPre: {
    fontSize: 13,
    lineHeight: 1.6,
    color: 'var(--text-secondary)',
    textAlign: 'left',
    maxWidth: '85%',
    whiteSpace: 'pre-wrap',
    fontFamily: 'var(--font-mono)',
    margin: 0,
  },
}

export default MessageItem
