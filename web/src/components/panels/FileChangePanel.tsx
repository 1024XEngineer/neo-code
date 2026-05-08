import { useMemo, useState, type CSSProperties } from 'react'
import { ChevronRight, Check, FileDiff, PanelRightClose } from 'lucide-react'
import { useUIStore, type FileChange } from '@/stores/useUIStore'
import type { DiffHunk, DiffLine } from '@/utils/patchParser'

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
    { added: 0, modified: 0, deleted: 0 },
  )
}

function DiffLineView({ line }: { line: DiffLine }) {
  const lineStyles: Record<DiffLine['type'], CSSProperties> = {
    add: { color: '#86efac', background: 'rgba(22, 163, 74, 0.08)' },
    del: { color: '#fca5a5', background: 'rgba(220, 38, 38, 0.08)' },
    header: { color: 'var(--accent-hover)', background: 'rgba(148, 163, 184, 0.08)' },
    context: { color: 'var(--text-secondary)' },
  }
  const prefix = line.type === 'add' ? '+' : line.type === 'del' ? '-' : line.type === 'context' ? ' ' : ''

  return (
    <div style={{ ...styles.diffLine, ...lineStyles[line.type] }}>
      <span style={styles.diffPrefix}>{prefix}</span>
      <span style={styles.diffText}>{line.content}</span>
    </div>
  )
}

function getDisplayHunks(change: FileChange): DiffHunk[] {
  if (change.hunks && change.hunks.length > 0) {
    return change.hunks
  }
  if (change.diff && change.diff.length > 0) {
    return [{
      header: '',
      lines: change.diff,
      additions: change.additions,
      deletions: change.deletions,
    }]
  }
  return []
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
  const meta = getStatusMeta(change.status)
  const reviewed = change.status === 'accepted' || change.status === 'rejected'
  const displayHunks = getDisplayHunks(change)

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
          </div>
          <div data-testid={`diff-scroller-${change.id}`} style={styles.diffScroller}>
            {displayHunks.length === 0 ? (
              <div style={styles.emptyDiff}>当前文件没有可展示的 diff</div>
            ) : (
              displayHunks.map((hunk, index) => (
                <div
                  key={`${change.id}-hunk-${index}`}
                  data-testid={`diff-hunk-${change.id}-${index}`}
                  style={styles.hunkBlock}
                >
                  {hunk.lines.map((line, lineIndex) => (
                    <DiffLineView key={`${change.id}-${index}-${lineIndex}`} line={line} />
                  ))}
                </div>
              ))
            )}
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
          <span style={styles.headerTitle}>文件变更</span>
          <button style={styles.closeBtn} onClick={toggleChangesPanel} title="关闭文件变更">
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
          <div style={styles.emptyState}>当前会话暂无文件变更</div>
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

const styles: Record<string, CSSProperties> = {
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
  diffScroller: {
    overflowX: 'auto',
    overflowY: 'visible',
    background: 'var(--code-bg)',
    padding: 8,
  },
  hunkBlock: {
    border: '1px solid rgba(148, 163, 184, 0.18)',
    borderRadius: 'var(--radius-sm)',
    overflow: 'hidden',
    background: 'rgba(15, 23, 42, 0.22)',
    marginBottom: 10,
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
  emptyDiff: {
    padding: '10px 12px',
    fontSize: 11,
    color: 'var(--text-tertiary)',
  },
}
