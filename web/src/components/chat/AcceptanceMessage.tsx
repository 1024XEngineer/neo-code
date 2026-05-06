import { useState, memo } from 'react'
import { type ChatMessage } from '@/stores/useChatStore'
import { CheckCircle2, XCircle, ChevronRight } from 'lucide-react'

interface AcceptanceMessageProps {
  message: ChatMessage
  groupedWithPrev?: boolean
}

/** 聊天流内的 Acceptance 决策卡 —— 不折叠,直接展示关键结果 */
const AcceptanceMessage = memo(function AcceptanceMessage({
  message,
  groupedWithPrev = false,
}: AcceptanceMessageProps) {
  const [expanded, setExpanded] = useState(false)
  const data = message.acceptanceData
  if (!data) return null

  const status = data.status
  const isAccepted = status === 'accepted'
  const isRejected = status === 'rejected'

  const accentColor = isAccepted ? 'var(--success)' : isRejected ? 'var(--error)' : 'var(--warning)'
  const bgColor = isAccepted
    ? 'rgba(22, 163, 74, 0.08)'
    : isRejected
    ? 'rgba(220, 38, 38, 0.08)'
    : 'rgba(217, 119, 6, 0.08)'

  return (
    <div style={groupedWithPrev ? styles.rowGrouped : styles.row} className="animate-fade-in">
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div style={{ ...styles.aiAvatar, background: bgColor, color: accentColor }}>
          {isAccepted ? <CheckCircle2 size={14} /> : <XCircle size={14} />}
        </div>
      )}
      <div style={styles.aiContent}>
        <div
          style={{
            ...styles.card,
            borderLeft: `3px solid ${accentColor}`,
            background: bgColor,
          }}
        >
          <div style={styles.cardTop}>
            <span style={{ ...styles.statusLabel, color: accentColor }}>
              {isAccepted ? '已接受' : isRejected ? '已拒绝' : status}
            </span>
            {data.user_visible_summary && (
              <span style={styles.summary}>{data.user_visible_summary}</span>
            )}
          </div>

          <div style={styles.metaRow}>
            {data.stop_reason && <span style={styles.meta}>停止原因: {data.stop_reason}</span>}
            {data.error_class && <span style={{ ...styles.meta, color: 'var(--error)' }}>错误: {data.error_class}</span>}
          </div>

          {(data.internal_summary || data.completion_blocked_reason) && (
            <button style={styles.expandBtn} onClick={() => setExpanded(!expanded)}>
              <span
                style={{
                  ...styles.expandChevron,
                  transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
                }}
              >
                <ChevronRight size={12} />
              </span>
              <span>{expanded ? '收起详情' : '展开详情'}</span>
            </button>
          )}

          {expanded && (
            <div style={styles.expandContent}>
              {data.internal_summary && <pre style={styles.pre}>{data.internal_summary}</pre>}
              {data.completion_blocked_reason && (
                <div style={{ color: 'var(--error)', fontSize: 12, marginTop: 4 }}>
                  阻塞原因: {data.completion_blocked_reason}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
})

const styles: Record<string, React.CSSProperties> = {
  row: {
    display: 'flex',
    gap: 10,
    padding: '6px 0',
  },
  rowGrouped: {
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
  card: {
    borderRadius: 'var(--radius-md)',
    padding: '10px 12px',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  cardTop: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexWrap: 'wrap',
  },
  statusLabel: {
    fontWeight: 600,
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    flexShrink: 0,
  },
  summary: {
    color: 'var(--text-primary)',
    fontSize: 13,
    lineHeight: 1.6,
    textWrap: 'pretty' as any,
  },
  metaRow: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: '4px 12px',
  },
  meta: {
    color: 'var(--text-tertiary)',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  expandBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '2px 0',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    fontSize: 11,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
  },
  expandChevron: {
    display: 'flex',
    transition: 'transform 0.15s',
  },
  expandContent: {
    padding: '6px 0',
    borderTop: '1px dashed var(--border-primary)',
    marginTop: 2,
  },
  pre: {
    margin: 0,
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    lineHeight: 1.6,
    color: 'var(--text-secondary)',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
  },
}

export default AcceptanceMessage