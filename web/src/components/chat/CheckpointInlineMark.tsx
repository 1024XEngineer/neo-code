import { useState } from 'react'
import { useSessionStore } from '@/stores/useSessionStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { Undo2, Loader2 } from 'lucide-react'

interface CheckpointInlineMarkProps {
  checkpointId: string
  status?: 'available' | 'restoring' | 'restored'
}

/** ToolCallCard 内的 checkpoint 微标识 —— 点击撤回 */
export default function CheckpointInlineMark({ checkpointId, status = 'available' }: CheckpointInlineMarkProps) {
  const gatewayAPI = useGatewayAPI()
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const [localStatus, setLocalStatus] = useState(status)

  if (localStatus === 'restored') {
    return (
      <span style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 3,
        padding: '1px 5px',
        borderRadius: 'var(--radius-sm)',
        background: 'var(--bg-tertiary)',
        color: 'var(--text-tertiary)',
        fontSize: 10,
        fontFamily: 'var(--font-mono)',
        cursor: 'default',
      }}>
        cp_{checkpointId.slice(0, 6)} · 已撤回
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
      await gatewayAPI.restoreCheckpoint({
        session_id: sessionId,
        checkpoint_id: checkpointId,
      })
      setLocalStatus('restored')
    } catch {
      setLocalStatus('available')
    }
  }

  const isRestoring = localStatus === 'restoring'

  return (
    <button
      style={{
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
        cursor: isRestoring ? 'default' : 'pointer',
        transition: 'all 0.15s',
      }}
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
  )
}
