import { useMemo, useState } from 'react'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useUIStore } from '@/stores/useUIStore'
import { deriveTodoView, type TodoViewRow } from '@/utils/deriveTodoView'
import {
  ChevronDown,
  ChevronUp,
  ChevronRight,
  CheckCircle2,
  XCircle,
  CircleDot,
  AlertTriangle,
  Loader2,
  ListChecks,
  Circle,
} from 'lucide-react'

type StatusKey = 'pending' | 'in_progress' | 'completed' | 'failed' | 'blocked' | 'canceled' | 'open'

const statusMeta: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  pending: { label: '待办', color: 'var(--text-tertiary)', icon: <Circle size={12} /> },
  in_progress: { label: '进行中', color: 'var(--accent)', icon: <Loader2 size={12} className="animate-spin" /> },
  completed: { label: '已完成', color: 'var(--success)', icon: <CheckCircle2 size={12} /> },
  failed: { label: '失败', color: 'var(--error)', icon: <XCircle size={12} /> },
  blocked: { label: '阻塞', color: 'var(--warning)', icon: <AlertTriangle size={12} /> },
  canceled: { label: '已取消', color: 'var(--text-tertiary)', icon: <CircleDot size={12} /> },
  open: { label: '待办', color: 'var(--text-tertiary)', icon: <Circle size={12} /> },
}

function getStatusMeta(status: string) {
  return statusMeta[status] || statusMeta.open
}

/** 输入框上方 Todo 折叠条 —— 实时显示当前 todo 进度,并在不超过上限时保留过时条目作为轨迹 */
export default function TodoStrip() {
  const snapshot = useRuntimeInsightStore((s) => s.todoSnapshot)
  const history = useRuntimeInsightStore((s) => s.todoHistory)
  const conflict = useRuntimeInsightStore((s) => s.todoConflict)
  const expanded = useUIStore((s) => s.todoStripExpanded)
  const setExpanded = useUIStore((s) => s.setTodoStripExpanded)

  const items = snapshot?.items ?? []
  const summary = snapshot?.summary
  const view = useMemo(() => deriveTodoView(snapshot, history), [snapshot, history])

  if (view.length === 0 && !conflict) {
    return null
  }

  const inProgress = items.find((i) => i.status === 'in_progress')
  const failedCount = items.filter((i) => i.status === 'failed').length
  const blockedCount = items.filter((i) => i.status === 'blocked').length
  const completedCount = items.filter((i) => i.status === 'completed').length
  const total = items.length
  const hasFailure = failedCount > 0
  const allDone = total > 0 && completedCount === total

  // 冲突态强制展开
  const effectiveExpanded = expanded || !!conflict

  // 头部状态图标与摘要文本
  let headIcon: React.ReactNode
  let headText: string
  let headColor: string
  if (allDone) {
    headIcon = <CheckCircle2 size={14} />
    headText = `全部完成 (${total})`
    headColor = 'var(--success)'
  } else if (conflict) {
    headIcon = <AlertTriangle size={14} />
    headText = `Todo 冲突: ${conflict.reason || '需要确认'}`
    headColor = 'var(--error)'
  } else if (hasFailure) {
    headIcon = <XCircle size={14} />
    headText = inProgress ? inProgress.content : `${failedCount} 项失败 · ${completedCount}/${total} 完成`
    headColor = 'var(--error)'
  } else if (inProgress) {
    headIcon = <Loader2 size={14} className="animate-spin" />
    headText = inProgress.content
    headColor = 'var(--accent)'
  } else if (allDone) {
    headIcon = <CheckCircle2 size={14} />
    headText = `全部完成 (${total})`
    headColor = 'var(--success)'
  } else {
    headIcon = <ListChecks size={14} />
    headText = `Todo · ${completedCount}/${total} 完成`
    headColor = 'var(--text-secondary)'
  }

  return (
    <div style={styles.outerWrap}>
      <div style={styles.innerWrap}>
        <button
          style={styles.head}
          onClick={() => setExpanded(!effectiveExpanded)}
          disabled={!!conflict}
          aria-expanded={effectiveExpanded}
        >
          <span style={{ display: 'flex', flexShrink: 0, color: headColor }}>{headIcon}</span>
          <span style={{ ...styles.headText, color: 'var(--text-primary)' }} title={headText}>
            {headText}
          </span>
          {(failedCount > 0 || blockedCount > 0) && !conflict && (
            <span style={styles.badgeRow}>
              {failedCount > 0 && (
                <span style={{ ...styles.badge, color: 'var(--error)', borderColor: 'rgba(220,38,38,0.3)' }}>
                  {failedCount} 失败
                </span>
              )}
              {blockedCount > 0 && (
                <span style={{ ...styles.badge, color: 'var(--warning)', borderColor: 'rgba(217,119,6,0.3)' }}>
                  {blockedCount} 阻塞
                </span>
              )}
            </span>
          )}
          <span style={styles.chevron} aria-hidden>
            {effectiveExpanded ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
          </span>
        </button>

        {effectiveExpanded && (
          <div style={styles.body}>
            {summary && (
              <div style={styles.summaryRow}>
                <SummaryBadge
                  icon={<CheckCircle2 size={12} />}
                  label={`${summary.required_completed}/${summary.required_total} 完成`}
                  color="var(--success)"
                />
                {summary.required_failed > 0 && (
                  <SummaryBadge
                    icon={<XCircle size={12} />}
                    label={`${summary.required_failed} 失败`}
                    color="var(--error)"
                  />
                )}
                {summary.required_open > 0 && (
                  <SummaryBadge
                    icon={<CircleDot size={12} />}
                    label={`${summary.required_open} 待办`}
                    color="var(--text-tertiary)"
                  />
                )}
              </div>
            )}
            {view.length > 0 ? (
              <div style={styles.list}>
                {view.map((row) => (
                  <TodoItem key={row.id} item={row} />
                ))}
              </div>
            ) : !conflict ? (
              <div style={styles.empty}>当前会话暂无 todo</div>
            ) : null}
          </div>
        )}
      </div>
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

function TodoItem({ item }: { item: TodoViewRow }) {
  const [open, setOpen] = useState(false)
  const meta = getStatusMeta(item.status)
  // 双保险:阻塞原因仅在 status=blocked 时纳入"展开详情"判断,与后端 invariant 对齐。
  const showBlockedReason = !!item.blocked_reason && item.status === 'blocked'
  const hasReason = !!item.failure_reason || showBlockedReason
  const isStale = item.isStale
  const iconColor = isStale ? 'var(--text-tertiary)' : meta.color
  const contentStyle: React.CSSProperties = isStale
    ? { ...styles.itemContent, textDecoration: 'line-through', color: 'var(--text-tertiary)' }
    : styles.itemContent

  return (
    <div style={styles.itemCard}>
      <button style={styles.itemHead} onClick={() => setOpen(!open)} disabled={!hasReason}>
        <span style={{ ...styles.itemChevron, transform: open ? 'rotate(90deg)' : 'rotate(0deg)', visibility: hasReason ? 'visible' : 'hidden' }}>
          <ChevronRight size={12} />
        </span>
        <span style={{ color: iconColor, display: 'flex', flexShrink: 0 }}>{meta.icon}</span>
        <span style={contentStyle}>
          {item.content}
          {!item.required && <span style={styles.optionalTag}>可选</span>}
        </span>
      </button>
      {open && hasReason && (
        <div style={styles.itemDetail}>
          {item.failure_reason && <div style={{ color: 'var(--error)', fontSize: 11 }}>失败原因: {item.failure_reason}</div>}
          {showBlockedReason && <div style={{ color: 'var(--warning)', fontSize: 11 }}>阻塞原因: {item.blocked_reason}</div>}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  outerWrap: {
    padding: '0 16px 8px',
    flexShrink: 0,
    background: 'var(--bg-primary)',
  },
  innerWrap: {
    maxWidth: 800,
    margin: '0 auto',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-secondary)',
    overflow: 'hidden',
  },
  head: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '8px 12px',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-primary)',
    cursor: 'pointer',
    textAlign: 'left',
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
  },
  headText: {
    flex: 1,
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontSize: 12,
    lineHeight: 1.5,
  },
  badgeRow: {
    display: 'flex',
    gap: 6,
    flexShrink: 0,
  },
  badge: {
    fontSize: 10,
    fontFamily: 'var(--font-ui)',
    padding: '1px 6px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid',
    background: 'transparent',
  },
  chevron: {
    display: 'flex',
    flexShrink: 0,
    color: 'var(--text-tertiary)',
  },
  body: {
    padding: '8px 12px 10px',
    borderTop: '1px solid var(--border-primary)',
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
    background: 'var(--bg-primary)',
  },
  summaryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '0 2px',
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
  },
  empty: {
    padding: '8px 0',
    textAlign: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 12,
  },
  itemCard: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-secondary)',
    overflow: 'hidden',
  },
  itemHead: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    width: '100%',
    padding: '6px 10px',
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

// Re-export status union type so consumers can refine if needed
export type { StatusKey }
