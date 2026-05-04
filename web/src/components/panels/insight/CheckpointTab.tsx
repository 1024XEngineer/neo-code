import { useState, useMemo } from 'react'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import ConfirmDialog from '@/components/ui/ConfirmDialog'
import {
  GitCommitHorizontal,
  Diff,
  RotateCcw,
  Undo2,
  AlertTriangle,
  FilePlus,
  FileMinus,
  FileEdit,
} from 'lucide-react'
import type { CheckpointCreatedPayload, CheckpointRestoredPayload, CheckpointUndoRestorePayload } from '@/api/protocol'

function formatCpTime(ms: number): string {
  const d = new Date(ms)
  return d.toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/** Checkpoint 列表面板 —— diff / restore / undo */
export default function CheckpointTab() {
  const gatewayAPI = useGatewayAPI()
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const checkpoints = useRuntimeInsightStore((s) => s.checkpoints)
  const checkpointWarning = useRuntimeInsightStore((s) => s.checkpointWarning)
  const checkpointEvents = useRuntimeInsightStore((s) => s.checkpointEvents)

  const [expandedCpId, setExpandedCpId] = useState<string | null>(null)
  const [diffLoading, setDiffLoading] = useState<string | null>(null)
  const [diffResult, setDiffResult] = useState<Record<string, { added: string[]; deleted: string[]; modified: string[] } | null>>({})

  const [confirm, setConfirm] = useState<{
    title: string
    description: string
    variant: 'danger' | 'warning'
    onConfirm: () => void
  } | null>(null)

  // 计算当前可以 undo 的 checkpoint_id
  const undoableCpId = useMemo(() => {
    for (let i = checkpointEvents.length - 1; i >= 0; i--) {
      const e = checkpointEvents[i]
      // CheckpointUndoRestore: undo 后不能再 undo 之前的
      if ('guard_checkpoint_id' in e && !('checkpoint_id' in e)) {
        return null
      }
      // CheckpointRestored: 可以 undo
      if ('checkpoint_id' in e && 'guard_checkpoint_id' in e) {
        return (e as CheckpointRestoredPayload).checkpoint_id
      }
    }
    return null
  }, [checkpointEvents])

  async function handleDiff(cpId: string) {
    if (!gatewayAPI || !sessionId) return
    if (expandedCpId === cpId) {
      setExpandedCpId(null)
      return
    }
    setExpandedCpId(cpId)
    if (!diffResult[cpId]) {
      setDiffLoading(cpId)
      try {
        const result = await gatewayAPI.checkpointDiff({ session_id: sessionId, checkpoint_id: cpId })
        const payload = result.payload
        setDiffResult((prev) => ({
          ...prev,
          [cpId]: {
            added: payload.files?.added ?? [],
            deleted: payload.files?.deleted ?? [],
            modified: payload.files?.modified ?? [],
          },
        }))
      } catch {
        setDiffResult((prev) => ({ ...prev, [cpId]: null }))
      } finally {
        setDiffLoading(null)
      }
    }
  }

  function handleRestore(cpId: string) {
    setConfirm({
      title: '恢复 Checkpoint',
      description: `确认恢复到 checkpoint ${cpId.slice(0, 8)}?\n这会回滚此后所有文件改动。`,
      variant: 'danger',
      onConfirm: async () => {
        setConfirm(null)
        if (!gatewayAPI || !sessionId) return
        try {
          await gatewayAPI.restoreCheckpoint({ session_id: sessionId, checkpoint_id: cpId })
        } catch {
          // toast handled by event bridge
        }
      },
    })
  }

  function handleUndo() {
    setConfirm({
      title: '撤销恢复',
      description: '确认撤销最近一次 checkpoint 恢复?\n这会回到恢复之前的状态。',
      variant: 'warning',
      onConfirm: async () => {
        setConfirm(null)
        if (!gatewayAPI || !sessionId) return
        try {
          await gatewayAPI.undoRestore(sessionId)
        } catch {
          // toast handled by event bridge
        }
      },
    })
  }

  return (
    <div style={styles.container}>
      {checkpointWarning && (
        <div style={styles.warningBanner}>
          <AlertTriangle size={14} style={{ color: 'var(--warning)', flexShrink: 0 }} />
          <span>Checkpoint 告警: {checkpointWarning.phase || '未知阶段'}</span>
        </div>
      )}

      {checkpoints.length === 0 ? (
        <div style={styles.empty}>当前会话暂无 checkpoint</div>
      ) : (
        <div style={styles.list}>
          {checkpoints.map((cp) => (
            <CheckpointItem
              key={cp.checkpoint_id}
              cp={cp}
              isExpanded={expandedCpId === cp.checkpoint_id}
              isDiffLoading={diffLoading === cp.checkpoint_id}
              diffData={diffResult[cp.checkpoint_id]}
              canUndo={cp.checkpoint_id === undoableCpId}
              onDiff={() => handleDiff(cp.checkpoint_id)}
              onRestore={() => handleRestore(cp.checkpoint_id)}
              onUndo={handleUndo}
            />
          ))}
        </div>
      )}

      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          description={confirm.description}
          variant={confirm.variant}
          onConfirm={confirm.onConfirm}
          onCancel={() => setConfirm(null)}
        />
      )}
    </div>
  )
}

function CheckpointItem({
  cp,
  isExpanded,
  isDiffLoading,
  diffData,
  canUndo,
  onDiff,
  onRestore,
  onUndo,
}: {
  cp: { checkpoint_id: string; reason: string; status: string; restorable: boolean; created_at_ms: number }
  isExpanded: boolean
  isDiffLoading: boolean
  diffData: { added: string[]; deleted: string[]; modified: string[] } | null | undefined
  canUndo: boolean
  onDiff: () => void
  onRestore: () => void
  onUndo: () => void
}) {
  return (
    <div style={styles.card}>
      <div style={styles.cardHead}>
        <div style={styles.cardMeta}>
          <span style={{ color: 'var(--text-tertiary)', display: 'flex' }}>
            <GitCommitHorizontal size={13} />
          </span>
          <span style={styles.cpId}>{cp.checkpoint_id.slice(0, 8)}</span>
          <span style={styles.cpReason}>{cp.reason}</span>
          <span style={styles.cpTime}>{formatCpTime(cp.created_at_ms)}</span>
          {!cp.restorable && (
            <span style={styles.notRestorable}>不可恢复</span>
          )}
        </div>
        <div style={styles.cardActions}>
          <button style={styles.actionBtn} onClick={onDiff} title="查看 diff">
            <Diff size={12} />
            {isDiffLoading ? '加载中…' : 'Diff'}
          </button>
          {cp.restorable && (
            <button style={{ ...styles.actionBtn, color: 'var(--warning)' }} onClick={onRestore} title="恢复到此 checkpoint">
              <RotateCcw size={12} />
              恢复
            </button>
          )}
          {canUndo && (
            <button style={{ ...styles.actionBtn, color: 'var(--accent)' }} onClick={onUndo} title="撤销最近一次恢复">
              <Undo2 size={12} />
              撤销
            </button>
          )}
        </div>
      </div>

      {isExpanded && diffData !== undefined && (
        <div style={styles.diffArea}>
          {diffData === null ? (
            <div style={styles.diffEmpty}>加载 diff 失败</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {diffData.added.length > 0 && (
                <FileGroup icon={<FilePlus size={12} style={{ color: 'var(--success)' }} />} label="新增" files={diffData.added} />
              )}
              {diffData.deleted.length > 0 && (
                <FileGroup icon={<FileMinus size={12} style={{ color: 'var(--error)' }} />} label="删除" files={diffData.deleted} />
              )}
              {diffData.modified.length > 0 && (
                <FileGroup icon={<FileEdit size={12} style={{ color: 'var(--warning)' }} />} label="修改" files={diffData.modified} />
              )}
              {diffData.added.length === 0 && diffData.deleted.length === 0 && diffData.modified.length === 0 && (
                <div style={styles.diffEmpty}>无文件变更</div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function FileGroup({ icon, label, files }: { icon: React.ReactNode; label: string; files: string[] }) {
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 4, fontSize: 11, fontWeight: 500, color: 'var(--text-secondary)' }}>
        {icon}
        <span>{label} ({files.length})</span>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        {files.map((f) => (
          <div key={f} style={{ fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', paddingLeft: 16 }}>
            {f}
          </div>
        ))}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  warningBanner: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 10px',
    borderRadius: 'var(--radius-md)',
    background: 'rgba(217,119,6,0.08)',
    color: 'var(--warning)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  empty: {
    padding: '24px 0',
    textAlign: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 12,
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  card: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-primary)',
    overflow: 'hidden',
  },
  cardHead: {
    padding: '8px 10px',
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  cardMeta: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexWrap: 'wrap',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  cpId: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  cpReason: {
    color: 'var(--text-secondary)',
  },
  cpTime: {
    color: 'var(--text-tertiary)',
    fontSize: 11,
  },
  notRestorable: {
    fontSize: 10,
    padding: '1px 5px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-tertiary)',
  },
  cardActions: {
    display: 'flex',
    gap: 6,
  },
  actionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    height: 24,
    padding: '0 8px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  diffArea: {
    padding: '8px 10px',
    borderTop: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
  },
  diffEmpty: {
    color: 'var(--text-tertiary)',
    fontSize: 12,
    fontStyle: 'italic',
  },
}
