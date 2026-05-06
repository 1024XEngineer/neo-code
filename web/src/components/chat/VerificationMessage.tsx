import { useState, memo } from 'react'
import { type ChatMessage } from '@/stores/useChatStore'
import { ChevronRight, CheckCircle2, XCircle, Loader2, MinusCircle } from 'lucide-react'

interface VerificationMessageProps {
  message: ChatMessage
  /** 是否与上一条 AI/工具消息属于同一回合 */
  groupedWithPrev?: boolean
}

/** 聊天流内的 Verification 折叠摘要卡 —— 沿用 ThinkingMessage 折叠模式 */
const VerificationMessage = memo(function VerificationMessage({
  message,
  groupedWithPrev = false,
}: VerificationMessageProps) {
  const [expanded, setExpanded] = useState(false)
  const data = message.verificationData
  if (!data) return null

  const stages = Object.values(data.stages)
  const status = data.status
  const isRunning = status === 'running'
  const isFailed = status === 'failed'

  const passedCount = stages.filter((s) => s.status === 'pass').length
  const totalCount = stages.length

  let headText = ''
  if (isRunning) {
    headText = `Verify running… (${passedCount}/${totalCount})`
  } else if (isFailed) {
    const firstFailed = stages.find((s) => s.status === 'fail')
    headText = firstFailed
      ? `Verify failed at ${firstFailed.name}`
      : `Verify failed (${passedCount}/${totalCount} passed)`
  } else {
    headText = `Verify ${status === 'completed' ? 'completed' : 'finished'} (${passedCount}/${totalCount} passed)`
  }

  return (
    <div style={groupedWithPrev ? styles.rowGrouped : styles.row} className="animate-fade-in">
      {groupedWithPrev ? (
        <div style={styles.avatarSpacer} aria-hidden />
      ) : (
        <div style={{ ...styles.aiAvatar, background: 'var(--accent-muted)', color: 'var(--accent)' }}>
          <CheckCircle2 size={14} />
        </div>
      )}
      <div style={styles.aiContent}>
        <button style={styles.head} onClick={() => setExpanded(!expanded)}>
          <span
            style={{
              ...styles.chevron,
              transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
            }}
          >
            <ChevronRight size={12} />
          </span>
          {isRunning && <Loader2 size={12} className="animate-spin" style={{ color: 'var(--warning)' }} />}
          {isFailed && <XCircle size={12} style={{ color: 'var(--error)' }} />}
          {!isRunning && !isFailed && <CheckCircle2 size={12} style={{ color: 'var(--success)' }} />}
          <span style={styles.label}>{headText}</span>
        </button>

        {expanded && (
          <div style={styles.detail}>
            {stages.length === 0 ? (
              <div style={styles.empty}>暂无 stage 数据</div>
            ) : (
              stages.map((stage) => (
                <div key={stage.name} style={styles.stageRow}>
                  <StageIcon status={stage.status} />
                  <span style={styles.stageName}>{stage.name}</span>
                  {stage.summary && (
                    <span style={styles.stageSummary}>{stage.summary}</span>
                  )}
                  {stage.reason && <span style={styles.stageReason}>{stage.reason}</span>}
                </div>
              ))
            )}
          </div>
        )}
      </div>
    </div>
  )
})

function StageIcon({ status }: { status: string }) {
  if (status === 'pass') return <CheckCircle2 size={12} style={{ color: 'var(--success)', flexShrink: 0 }} />
  if (status === 'fail') return <XCircle size={12} style={{ color: 'var(--error)', flexShrink: 0 }} />
  if (status === 'soft_block') return <MinusCircle size={12} style={{ color: 'var(--warning)', flexShrink: 0 }} />
  if (status === 'hard_block') return <XCircle size={12} style={{ color: 'var(--error)', flexShrink: 0 }} />
  return <MinusCircle size={12} style={{ color: 'var(--text-tertiary)', flexShrink: 0 }} />
}

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
  head: {
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
    textAlign: 'left',
    width: '100%',
  },
  chevron: {
    display: 'flex',
    color: 'var(--text-tertiary)',
    transition: 'transform 0.15s',
    flexShrink: 0,
  },
  label: {
    fontWeight: 500,
  },
  detail: {
    padding: '8px 10px',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-tertiary)',
    marginTop: 4,
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  stageRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  stageName: {
    fontWeight: 500,
    color: 'var(--text-primary)',
    minWidth: 80,
  },
  stageSummary: {
    color: 'var(--text-secondary)',
    flex: 1,
  },
  stageReason: {
    color: 'var(--error)',
    fontSize: 11,
  },
  empty: {
    color: 'var(--text-tertiary)',
    fontSize: 12,
    fontStyle: 'italic',
  },
}

export default VerificationMessage