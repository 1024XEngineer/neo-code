import { useState, useRef, useEffect } from 'react'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { formatTokenCount } from '@/utils/format'
import { AlertTriangle } from 'lucide-react'

/** Budget 常驻指示器 —— 底部状态栏中的全局警觉信号 */
export default function BudgetIndicator() {
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)

  const budgetChecked = useRuntimeInsightStore((s) => s.budgetChecked)
  const budgetUsageRatio = useRuntimeInsightStore((s) => s.budgetUsageRatio)
  const budgetEstimateFailed = useRuntimeInsightStore((s) => s.budgetEstimateFailed)
  const ledgerReconciled = useRuntimeInsightStore((s) => s.ledgerReconciled)

  // 点击外部关闭 popover
  useEffect(() => {
    if (!open) return
    function onClick(e: MouseEvent) {
      const target = e.target as Node
      if (
        popoverRef.current?.contains(target) ||
        buttonRef.current?.contains(target)
      ) {
        return
      }
      setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [open])

  // 无数据时整体不渲染
  if (!budgetChecked && !budgetEstimateFailed) return null

  const ratio = budgetUsageRatio ?? 0
  const barWidth = `${Math.min(Math.round(ratio * 100), 100)}%`

  // 配色策略
  let statusColor = 'var(--text-tertiary)'
  if (budgetEstimateFailed) {
    statusColor = 'var(--error)'
  } else if (ratio > 0.8) {
    statusColor = 'var(--error)'
  } else if (ratio > 0.6) {
    statusColor = 'var(--warning)'
  }

  const hasEstimate = !!budgetChecked

  return (
    <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
      <button
        ref={buttonRef}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
          padding: '2px 6px',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--text-tertiary)',
          fontFamily: 'var(--font-ui)',
        }}
        onClick={() => setOpen((v) => !v)}
        title="点击查看预算明细"
      >
        {budgetEstimateFailed && (
          <AlertTriangle size={12} style={{ color: 'var(--error)', flexShrink: 0 }} />
        )}

        {/* 窄进度条 */}
        <div style={{
          width: 60,
          height: 4,
          borderRadius: 2,
          background: 'var(--border-primary)',
          overflow: 'hidden',
          flexShrink: 0,
        }}>
          <div style={{
            width: barWidth,
            height: '100%',
            background: statusColor,
            borderRadius: 2,
            transition: 'width 0.3s, background 0.3s',
          }} />
        </div>

        {/* 比例文本 */}
        <span style={{
          fontSize: 11,
          fontFamily: 'var(--font-mono)',
          color: statusColor,
          transition: 'color 0.3s',
          whiteSpace: 'nowrap',
        }}>
          {hasEstimate
            ? `${formatTokenCount(budgetChecked!.estimated_input_tokens)} / ${formatTokenCount(budgetChecked!.prompt_budget)}`
            : '预算不可用'}
        </span>
      </button>

      {open && (
        <div
          ref={popoverRef}
          style={{
            position: 'absolute',
            bottom: 30,
            right: 0,
            zIndex: 10,
            width: 240,
            padding: 12,
            borderRadius: 'var(--radius-md)',
            border: '1px solid var(--border-primary)',
            background: 'var(--bg-secondary)',
            boxShadow: 'var(--shadow-3)',
            fontFamily: 'var(--font-ui)',
            fontSize: 12,
          }}
        >
          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--text-primary)', marginBottom: 10 }}>
            预算明细
          </div>

          {budgetEstimateFailed ? (
            <div style={{ color: 'var(--error)', marginBottom: 8 }}>
              {budgetEstimateFailed.message}
            </div>
          ) : budgetChecked ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <Row label="Budget" value={formatTokenCount(budgetChecked.prompt_budget)} />
              {budgetChecked.context_window ? (
                <Row label="Context Window" value={formatTokenCount(budgetChecked.context_window)} />
              ) : null}
              <Row label="Estimated" value={`${formatTokenCount(budgetChecked.estimated_input_tokens)} (${(ratio * 100).toFixed(1)}%)`} />
              <Row label="Action" value={budgetChecked.action} />
              {budgetChecked.reason && (
                <Row label="Reason" value={budgetChecked.reason} />
              )}
            </div>
          ) : (
            <div style={{ color: 'var(--text-tertiary)' }}>暂无预算数据</div>
          )}

          {ledgerReconciled && (
            <>
              <div style={{
                height: 1,
                background: 'var(--border-primary)',
                margin: '10px 0',
              }} />
              <div style={{ fontWeight: 600, fontSize: 12, color: 'var(--text-primary)', marginBottom: 8 }}>
                Ledger 对账
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                <Row label="Input" value={`${formatTokenCount(ledgerReconciled.input_tokens)} (${ledgerReconciled.input_source})`} />
                <Row label="Output" value={`${formatTokenCount(ledgerReconciled.output_tokens)} (${ledgerReconciled.output_source})`} />
                {ledgerReconciled.has_unknown_usage && (
                  <div style={{ color: 'var(--warning)', fontSize: 11 }}>
                    ⚠ 存在未知用量
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
      <span style={{ color: 'var(--text-tertiary)' }}>{label}</span>
      <span style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-mono)' }}>{value}</span>
    </div>
  )
}
