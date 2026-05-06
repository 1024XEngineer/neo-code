import { useState, useMemo } from 'react'
import { useUIStore, type FileChange } from '@/stores/useUIStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import {
  ChevronRight,
  FileDiff,
  PanelRightClose,
  Check,
  X,
} from 'lucide-react'

const statusMeta: Record<string, { label: string; color: string; bg: string }> = {
  added: { label: '新增', color: 'var(--success)', bg: 'rgba(22, 163, 74, 0.12)' },
  modified: { label: '修改', color: 'var(--warning)', bg: 'rgba(217, 119, 6, 0.14)' },
  deleted: { label: '删除', color: 'var(--error)', bg: 'rgba(220, 38, 38, 0.12)' },
  accepted: { label: '已接受', color: 'var(--success)', bg: 'rgba(22, 163, 74, 0.12)' },
  rejected: { label: '已拒绝', color: 'var(--text-tertiary)', bg: 'var(--bg-active)' },
}

function getStatusMeta(status: string) {
  return statusMeta[status] || statusMeta.modified
}

function getChangeType(change: { status: string; additions: number; deletions: number }) {
  if (['added', 'modified', 'deleted'].includes(change.status)) return change.status as 'added' | 'modified' | 'deleted'
  if (change.additions > 0 && change.deletions > 0) return 'modified'
  if (change.additions > 0) return 'added'
  if (change.deletions > 0) return 'deleted'
  return 'modified'
}

function getChangeCounts(fileChanges: { status: string; additions: number; deletions: number }[]) {
  return fileChanges.reduce(
    (counts, change) => {
      const ct = getChangeType(change)
      if (ct === 'added') counts.added += 1
      if (ct === 'modified') counts.modified += 1
      if (ct === 'deleted') counts.deleted += 1
      return counts
    },
    { added: 0, modified: 0, deleted: 0 }
  )
}

function DiffLine({ line }: { line: { type: 'add' | 'del' | 'header'; content: string } }) {
  const lineStyles: Record<string, React.CSSProperties> = {
    add: { color: '#86efac', background: 'rgba(22, 163, 74, 0.08)' },
    del: { color: '#fca5a5', background: 'rgba(220, 38, 38, 0.08)' },
    header: { color: 'var(--accent-hover)' },
  }
  const prefix = line.type === 'add' ? '+' : line.type === 'del' ? '-' : ''

  return (
    <div style={{ ...styles.diffLine, ...lineStyles[line.type] }}>
      <span style={styles.diffPrefix}>{prefix}</span>
      <span style={styles.diffText}>{line.content}</span>
    </div>
  )
}

function FileChangeItem({
  change,
  expanded,
  onToggle,
}: {
  change: FileChange
  expanded: boolean
  onToggle: () => void
}) {
  const acceptFileChange = useUIStore((s) => s.acceptFileChange)
  const rejectFileChange = useUIStore((s) => s.rejectFileChange)
  const gatewayAPI = useGatewayAPI()
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const meta = getStatusMeta(change.status)
  const reviewed = change.status === 'accepted' || change.status === 'rejected'

  const handleReject = async () => {
    if (!change.checkpoint_id || !gatewayAPI || !sessionId) {
      rejectFileChange(change.id)
      return
    }
    const confirmed = window.confirm(
      `This will restore files to their pre-change state and revert all file changes from this turn. Continue?\n\nFile: ${change.path}`
    )
    if (!confirmed) return

    try {
      const result = await gatewayAPI.restoreCheckpoint({
        session_id: sessionId,
        checkpoint_id: change.checkpoint_id,
      })
      if (result?.payload) {
        useUIStore.getState().clearFileChanges()
        useUIStore.getState().showToast('Restored to pre-change state', 'success')
      }
    } catch (e) {
      console.warn('[FileChangePanel] restoreCheckpoint failed:', e)
      useUIStore.getState().showToast('Restore failed', 'error')
    }
  }

  return (
    <div style={styles.changeCard}>
      <button style={styles.changeHeader} onClick={onToggle}>
        <span style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          <ChevronRight size={12} />
        </span>
        <span style={styles.fileIcon}>
          <FileDiff size={14} />
        </span>
        <span style={styles.changeMain}>
          <span style={styles.pathText}>{change.path}</span>
          <span style={styles.changeStats}>
            <span style={{ ...styles.statusPill, color: meta.color, background: meta.bg }}>{meta.label}</span>
            <span style={styles.additions}>+{change.additions}</span>
            <span style={styles.deletions}>-{change.deletions}</span>
          </span>
        </span>
      </button>

      {expanded && (
        <div style={styles.expandedArea}>
          <div style={styles.actionsRow}>
            <button
              style={{ ...styles.actionBtn, color: reviewed ? 'var(--text-tertiary)' : 'var(--success)' }}
              onClick={() => acceptFileChange(change.id)}
              disabled={reviewed}
              title="标记为已审阅"
            >
              <Check size={13} />
              接受
            </button>
            <button
              style={{ ...styles.actionBtn, color: reviewed ? 'var(--text-tertiary)' : 'var(--error)' }}
              onClick={handleReject}
              disabled={reviewed}
              title="拒绝并回退更改"
            >
              <X size={13} />
              拒绝
            </button>
          </div>
          <div style={styles.diffBlock}>
            {change.diff?.map((line, index) => (
              <DiffLine key={`${change.id}-${index}`} line={line} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default function FileChangePanel() {
  const fileChanges = useUIStore((s) => s.fileChanges)
  const toggleChangesPanel = useUIStore((s) => s.toggleChangesPanel)
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const counts = useMemo(() => getChangeCounts(fileChanges), [fileChanges])

  const toggleExpanded = (id: string) => {
    setExpandedIds((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <div style={styles.headerTop}>
          <span style={styles.headerTitle}>文件更改</span>
          <button style={styles.closeBtn} onClick={toggleChangesPanel} title="关闭文件更改">
            <PanelRightClose size={16} />
          </button>
        </div>
        <div style={styles.summaryRow}>
          <span>{fileChanges.length} 个文件</span>
          <span style={styles.summaryDivider} />
          <span style={{ color: 'var(--success)' }}>{counts.added} 新增</span>
          <span style={{ color: 'var(--warning)' }}>{counts.modified} 修改</span>
          <span style={{ color: 'var(--error)' }}>{counts.deleted} 删除</span>
        </div>
      </div>

      <div style={styles.scrollArea}>
        {fileChanges.length === 0 ? (
          <div style={styles.emptyState}>当前会话暂无文件更改</div>
        ) : (
          fileChanges.map((change) => (
            <FileChangeItem
              key={change.id}
              change={change}
              expanded={expandedIds.has(change.id)}
              onToggle={() => toggleExpanded(change.id)}
            />
          ))
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    background: 'var(--bg-secondary)',
  },
  header: {
    padding: '12px 14px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
  },
  headerTop: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 4,
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
  },
  summaryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    minWidth: 0,
    color: 'var(--text-tertiary)',
    fontSize: 11,
    whiteSpace: 'nowrap',
  },
  summaryDivider: {
    width: 1,
    height: 10,
    background: 'var(--border-primary)',
  },
  scrollArea: {
    flex: 1,
    overflowY: 'auto',
    padding: 8,
  },
  emptyState: {
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 12,
  },
  changeCard: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-primary)',
    overflow: 'hidden',
    marginBottom: 8,
  },
  changeHeader: {
    display: 'flex',
    alignItems: 'center',
    width: '100%',
    gap: 6,
    padding: '9px 10px',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    textAlign: 'left',
  },
  chevron: {
    display: 'flex',
    width: 14,
    flexShrink: 0,
    color: 'var(--text-tertiary)',
    transition: 'transform 0.2s',
  },
  fileIcon: {
    display: 'flex',
    flexShrink: 0,
    color: 'var(--text-tertiary)',
  },
  changeMain: {
    minWidth: 0,
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    gap: 5,
  },
  pathText: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
    color: 'var(--text-primary)',
  },
  changeStats: {
    display: 'flex',
    alignItems: 'center',
    gap: 7,
    minWidth: 0,
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
  },
  statusPill: {
    padding: '1px 6px',
    borderRadius: 'var(--radius-sm)',
    fontFamily: 'var(--font-ui)',
    fontSize: 11,
  },
  additions: {
    color: 'var(--success)',
  },
  deletions: {
    color: 'var(--error)',
  },
  expandedArea: {
    borderTop: '1px solid var(--border-primary)',
  },
  actionsRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '7px 8px',
    background: 'var(--bg-tertiary)',
  },
  actionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    height: 24,
    padding: '0 8px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  diffBlock: {
    maxHeight: 260,
    overflow: 'auto',
    background: 'var(--code-bg)',
    padding: '6px 0',
  },
  diffLine: {
    display: 'flex',
    minWidth: 'max-content',
    padding: '1px 10px',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    lineHeight: 1.55,
    color: 'var(--text-secondary)',
    whiteSpace: 'pre',
  },
  diffPrefix: {
    width: 16,
    flexShrink: 0,
    color: 'var(--text-tertiary)',
  },
  diffText: {
    whiteSpace: 'pre',
  },
}
