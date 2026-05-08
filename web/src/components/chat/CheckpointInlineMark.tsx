import { useState, useEffect, useRef } from 'react'
import { useSessionStore } from '@/stores/useSessionStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { parseUnifiedPatch } from '@/utils/patchParser'
import { Undo2, Redo2, Loader2, Eye } from 'lucide-react'

interface CheckpointInlineMarkProps {
  checkpointId: string
  status?: 'available' | 'restoring' | 'restored'
}

export default function CheckpointInlineMark({ checkpointId, status = 'available' }: CheckpointInlineMarkProps) {
  const gatewayAPI = useGatewayAPI()
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const [localStatus, setLocalStatus] = useState(status)
  const [showDiff, setShowDiff] = useState(false)

  useEffect(() => { setLocalStatus(status) }, [status])

  if (localStatus === 'restored') {
    return (
      <span style={styles.wrapper}>
        <button style={{ ...styles.badge, ...styles.restoredBadge }} onClick={handleUndoRestore} title="撤销恢复">
          <span>cp_{checkpointId.slice(0, 6)} · 已撤回</span>
          <Redo2 size={10} />
        </button>
      </span>
    )
  }

  async function handleRestore() {
    if (!gatewayAPI || !sessionId || localStatus !== 'available') return
    const ok = window.confirm(
      `Restore to checkpoint ${checkpointId.slice(0, 8)}?\nThis will roll back all later file changes.`,
    )
    if (!ok) return
    setLocalStatus('restoring')
    try {
      await gatewayAPI.restoreCheckpoint({ session_id: sessionId, checkpoint_id: checkpointId })
      setLocalStatus('restored')
    } catch {
      setLocalStatus('available')
    }
  }

  async function handleUndoRestore() {
    if (!gatewayAPI || !sessionId) return
    const ok = window.confirm('Undo the last restore? This will bring back the reverted state.')
    if (!ok) return
    try {
      await gatewayAPI.undoRestore(sessionId)
    } catch { /* event will sync state */ }
  }

  async function handleToggleDiff() {
    if (showDiff) { setShowDiff(false); return }
    if (!gatewayAPI || !sessionId) return
    const insightStore = useRuntimeInsightStore.getState()
    insightStore.setCheckpointDiff(null)
    setShowDiff(true)
    try {
      const result = await gatewayAPI.checkpointDiff({
        session_id: sessionId,
        checkpoint_id: checkpointId,
      })
      if (result?.payload) insightStore.setCheckpointDiff(result.payload)
    } catch {
      setShowDiff(false)
    }
  }

  const isRestoring = localStatus === 'restoring'

  return (
    <span style={styles.wrapper}>
      <button
        style={styles.badge}
        onClick={handleRestore}
        disabled={isRestoring}
        title="撤回到此 checkpoint"
      >
        <span>cp_{checkpointId.slice(0, 6)}</span>
        {isRestoring ? (
          <Loader2 size={10} className="animate-spin" style={{ opacity: 0.7 }} />
        ) : (
          <Undo2 size={10} />
        )}
      </button>
      <button style={styles.diffBtn} onClick={handleToggleDiff} title="查看差异">
        <Eye size={10} />
      </button>
      {showDiff && <CheckpointDiffPopover onClose={() => setShowDiff(false)} />}
    </span>
  )
}

function CheckpointDiffPopover({ onClose }: { onClose: () => void }) {
  const diff = useRuntimeInsightStore((s) => s.checkpointDiff)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [onClose])

  const fileDiffs = diff?.patch ? parseUnifiedPatch(diff.patch) : {}

  return (
    <div ref={ref} style={styles.popover}>
      <div style={styles.popoverHeader}>
        <span style={styles.popoverTitle}>
          {diff ? `Changes (${(diff.files?.added?.length ?? 0) + (diff.files?.modified?.length ?? 0) + (diff.files?.deleted?.length ?? 0)} files)` : 'Loading...'}
        </span>
        <button style={styles.popoverClose} onClick={onClose}>x</button>
      </div>
      {!diff ? (
        <div style={styles.popoverLoading}><Loader2 size={14} className="animate-spin" /></div>
      ) : Object.keys(fileDiffs).length === 0 ? (
        <div style={styles.popoverEmpty}>No file changes</div>
      ) : (
        <div style={styles.popoverBody}>
          {Object.entries(fileDiffs).map(([path, fd]) => (
            <div key={path}>
              <div style={styles.diffFileName}>{path}
                <span style={styles.diffStats}>
                  <span style={{ color: 'var(--success)' }}>+{fd.additions}</span>
                  <span style={{ color: 'var(--error)' }}>-{fd.deletions}</span>
                </span>
              </div>
              <div style={styles.diffBlock}>
                {fd.lines.map((line, i) => {
                  const ls = line.type === 'add'
                    ? { color: '#86efac', background: 'rgba(22,163,74,0.08)' }
                    : line.type === 'del'
                    ? { color: '#fca5a5', background: 'rgba(220,38,38,0.08)' }
                    : { color: 'var(--accent-hover)' }
                  const prefix = line.type === 'add' ? '+' : line.type === 'del' ? '-' : ''
                  return (
                    <div key={i} style={{ ...styles.diffLine, ...ls }}>
                      <span style={styles.diffPrefix}>{prefix}</span>
                      <span>{line.content}</span>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  wrapper: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 2,
    position: 'relative',
  },
  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 3,
    padding: '1px 5px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-secondary)',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    border: 'none',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  restoredBadge: {
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
  },
  diffBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 16,
    height: 16,
    padding: 0,
    borderRadius: 'var(--radius-sm)',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    border: 'none',
    cursor: 'pointer',
    transition: 'color 0.15s',
  },
  popover: {
    position: 'absolute',
    top: '100%',
    right: 0,
    marginTop: 4,
    width: 380,
    maxHeight: 400,
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    boxShadow: '0 8px 24px rgba(0,0,0,0.35)',
    zIndex: 100,
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column',
  },
  popoverHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 10px',
    borderBottom: '1px solid var(--border-primary)',
    fontSize: 11,
    color: 'var(--text-secondary)',
  },
  popoverTitle: { fontWeight: 600 },
  popoverClose: {
    background: 'none',
    border: 'none',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    fontSize: 12,
    padding: '0 2px',
  },
  popoverLoading: {
    padding: 20,
    display: 'flex',
    justifyContent: 'center',
    color: 'var(--text-tertiary)',
  },
  popoverEmpty: {
    padding: 16,
    textAlign: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 11,
  },
  popoverBody: {
    overflowY: 'auto',
    flex: 1,
  },
  diffFileName: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '4px 10px',
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    color: 'var(--text-primary)',
    background: 'var(--bg-tertiary)',
  },
  diffStats: {
    display: 'inline-flex',
    gap: 6,
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
  },
  diffBlock: {
    background: 'var(--code-bg)',
    padding: '4px 0',
  },
  diffLine: {
    display: 'flex',
    minWidth: 'max-content',
    padding: '1px 10px',
    fontFamily: 'var(--font-mono)',
    fontSize: 10,
    lineHeight: 1.5,
    whiteSpace: 'pre',
  },
  diffPrefix: {
    width: 14,
    flexShrink: 0,
    color: 'var(--text-tertiary)',
  },
}
