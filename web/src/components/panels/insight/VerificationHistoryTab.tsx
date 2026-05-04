import { useState } from 'react'
import { useRuntimeInsightStore, type VerificationRunRecord } from '@/stores/useRuntimeInsightStore'
import { ChevronRight, CheckCircle2, XCircle, Loader2, MinusCircle } from 'lucide-react'

/** 验证历史归档面板 */
export default function VerificationHistoryTab() {
  const history = useRuntimeInsightStore((s) => s.verificationHistory)
  const isRunning = useRuntimeInsightStore((s) => s.verificationRunning)

  if (history.length === 0 && !isRunning) {
    return <div style={styles.empty}>当前会话暂无验证记录</div>
  }

  return (
    <div style={styles.container}>
      {history.map((record, idx) => (
        <VerificationRunItem
          key={record.id}
          record={record}
          index={history.length - idx}
          isLatest={idx === history.length - 1}
        />
      ))}
    </div>
  )
}

function VerificationRunItem({
  record,
  index,
  isLatest,
}: {
  record: VerificationRunRecord
  index: number
  isLatest: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  const stages = Object.values(record.stages)
  const passedCount = stages.filter((s) => s.status === 'passed').length

  const duration =
    record.finishedAt && record.startedAt
      ? `${((record.finishedAt - record.startedAt) / 1000).toFixed(1)}s`
      : undefined

  let statusColor = 'var(--text-tertiary)'
  let StatusIcon: React.ReactNode = <MinusCircle size={12} />
  if (record.status === 'running') {
    statusColor = 'var(--warning)'
    StatusIcon = <Loader2 size={12} className="animate-spin" />
  } else if (record.status === 'failed') {
    statusColor = 'var(--error)'
    StatusIcon = <XCircle size={12} />
  } else if (record.status === 'completed') {
    statusColor = 'var(--success)'
    StatusIcon = <CheckCircle2 size={12} />
  }

  return (
    <div style={styles.card}>
      <button style={styles.head} onClick={() => setExpanded(!expanded)}>
        <span style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          <ChevronRight size={12} />
        </span>
        <span style={{ color: statusColor, display: 'flex' }}>{StatusIcon}</span>
        <span style={styles.runLabel}>Run #{index}</span>
        {isLatest && (
          <span style={styles.latestBadge}>最新</span>
        )}
        <span style={styles.stageCount}>
          {passedCount}/{stages.length} passed
        </span>
        {duration && <span style={styles.duration}>{duration}</span>}
      </button>

      {expanded && (
        <div style={styles.detail}>
          {stages.length === 0 ? (
            <div style={styles.emptyStage}>暂无 stage 数据</div>
          ) : (
            stages.map((stage) => (
              <div key={stage.name} style={styles.stageRow}>
                {stage.status === 'passed' ? (
                  <CheckCircle2 size={12} style={{ color: 'var(--success)', flexShrink: 0 }} />
                ) : stage.status === 'failed' ? (
                  <XCircle size={12} style={{ color: 'var(--error)', flexShrink: 0 }} />
                ) : (
                  <MinusCircle size={12} style={{ color: 'var(--text-tertiary)', flexShrink: 0 }} />
                )}
                <span style={styles.stageName}>{stage.name}</span>
                {stage.summary && <span style={styles.stageSummary}>{stage.summary}</span>}
                {stage.reason && <span style={styles.stageReason}>{stage.reason}</span>}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
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
  card: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    background: 'var(--bg-primary)',
    overflow: 'hidden',
  },
  head: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
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
  chevron: {
    display: 'flex',
    flexShrink: 0,
    color: 'var(--text-tertiary)',
    transition: 'transform 0.2s',
  },
  runLabel: {
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  latestBadge: {
    fontSize: 10,
    padding: '1px 5px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--accent-muted)',
    color: 'var(--accent)',
    fontWeight: 500,
  },
  stageCount: {
    marginLeft: 'auto',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
  },
  duration: {
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    minWidth: 40,
    textAlign: 'right',
  },
  detail: {
    padding: '8px 10px',
    borderTop: '1px solid var(--border-primary)',
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
  emptyStage: {
    color: 'var(--text-tertiary)',
    fontSize: 12,
    fontStyle: 'italic',
  },
}
