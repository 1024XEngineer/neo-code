import { useState } from 'react'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { ChevronRight, CheckCircle2, XCircle, CircleDot, AlertTriangle } from 'lucide-react'

const statusMeta: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  completed: { label: '已完成', color: 'var(--success)', icon: <CheckCircle2 size={12} /> },
  failed: { label: '失败', color: 'var(--error)', icon: <XCircle size={12} /> },
  blocked: { label: '阻塞', color: 'var(--warning)', icon: <AlertTriangle size={12} /> },
  open: { label: '待办', color: 'var(--text-tertiary)', icon: <CircleDot size={12} /> },
}

function getStatusMeta(status: string) {
  return statusMeta[status] || statusMeta.open
}

/** Todo 面板 —— InsightPanel 默认 tab */
export default function TodoTab() {
  const snapshot = useRuntimeInsightStore((s) => s.todoSnapshot)
  const conflict = useRuntimeInsightStore((s) => s.todoConflict)

  const items = snapshot?.items ?? []
  const summary = snapshot?.summary

  return (
    <div style={styles.container}>
      {/* 冲突横幅 */}
      {conflict && (
        <div style={styles.conflictBanner}>
          <AlertTriangle size={14} style={{ color: 'var(--error)', flexShrink: 0 }} />
          <span>Todo 冲突: {conflict.reason || '未知'}</span>
        </div>
      )}

      {/* 摘要行 */}
      {summary && (
        <div style={styles.summaryRow}>
          <SummaryBadge
            icon={<CheckCircle2 size={12} />}
            label={`${summary.required_completed}/${summary.required_total} 完成`}
            color="var(--success)"
          />
          <SummaryBadge
            icon={<XCircle size={12} />}
            label={`${summary.required_failed} 失败`}
            color="var(--error)"
          />
          <SummaryBadge
            icon={<CircleDot size={12} />}
            label={`${summary.required_open} 待办`}
            color="var(--text-tertiary)"
          />
        </div>
      )}

      {/* Todo 列表 */}
      {items.length === 0 ? (
        <div style={styles.empty}>当前会话暂无 todo</div>
      ) : (
        <div style={styles.list}>
          {items.map((item) => (
            <TodoItem key={item.id} item={item} />
          ))}
        </div>
      )}
    </div>
  )
}

function SummaryBadge({ icon, label, color }: { icon: React.ReactNode; label: string; color: string }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, color, fontSize: 11, fontFamily: 'var(--font-ui)' }}>
      {icon}
      <span>{label}</span>
    </div>
  )
}

function TodoItem({ item }: { item: { id: string; content: string; status: string; required: boolean; failure_reason?: string; blocked_reason?: string; revision: number } }) {
  const [expanded, setExpanded] = useState(false)
  const meta = getStatusMeta(item.status)
  const hasReason = !!item.failure_reason || !!item.blocked_reason

  return (
    <div style={styles.itemCard}>
      <button style={styles.itemHead} onClick={() => setExpanded(!expanded)}>
        <span style={{ ...styles.itemChevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          <ChevronRight size={12} />
        </span>
        <span style={{ color: meta.color, display: 'flex', flexShrink: 0 }}>{meta.icon}</span>
        <span style={styles.itemContent}>
          {item.content}
          {!item.required && <span style={styles.optionalTag}>可选</span>}
        </span>
      </button>
      {expanded && hasReason && (
        <div style={styles.itemDetail}>
          {item.failure_reason && <div style={{ color: 'var(--error)', fontSize: 11 }}>失败原因: {item.failure_reason}</div>}
          {item.blocked_reason && <div style={{ color: 'var(--warning)', fontSize: 11 }}>阻塞原因: {item.blocked_reason}</div>}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  conflictBanner: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 10px',
    borderRadius: 'var(--radius-md)',
    background: 'rgba(220,38,38,0.08)',
    color: 'var(--error)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  summaryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '4px 2px',
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  empty: {
    padding: '24px 0',
    textAlign: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 12,
  },
  itemCard: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-primary)',
    overflow: 'hidden',
  },
  itemHead: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    width: '100%',
    padding: '8px 10px',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    textAlign: 'left',
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
  },
  itemChevron: {
    display: 'flex',
    flexShrink: 0,
    color: 'var(--text-tertiary)',
    transition: 'transform 0.2s',
  },
  itemContent: {
    flex: 1,
    minWidth: 0,
    color: 'var(--text-primary)',
    lineHeight: 1.5,
  },
  optionalTag: {
    marginLeft: 6,
    fontSize: 10,
    padding: '0 4px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-tertiary)',
  },
  itemDetail: {
    padding: '6px 10px',
    borderTop: '1px solid var(--border-primary)',
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
    fontFamily: 'var(--font-ui)',
  },
}
