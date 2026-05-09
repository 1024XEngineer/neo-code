import { useState, useRef, useEffect } from 'react'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { formatTokenCount } from '@/utils/format'
import { AlertTriangle } from 'lucide-react'

export default function BudgetIndicator() {
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)

  const budgetChecked = useRuntimeInsightStore((s) => s.budgetChecked)
  const budgetUsageRatio = useRuntimeInsightStore((s) => s.budgetUsageRatio)
  const budgetEstimateFailed = useRuntimeInsightStore((s) => s.budgetEstimateFailed)
  const ledgerReconciled = useRuntimeInsightStore((s) => s.ledgerReconciled)

  useEffect(() => {
    if (!open) return
    function onClick(e: MouseEvent) {
      const target = e.target as Node
      if (popoverRef.current?.contains(target) || buttonRef.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [open])

  if (!budgetChecked && !budgetEstimateFailed) return null

  const ratio = budgetUsageRatio ?? 0
  const barWidth = `${Math.min(Math.round(ratio * 100), 100)}%`

  let statusColor = 'var(--text-tertiary)'
  let barClass = ''
  if (budgetEstimateFailed) {
    statusColor = 'var(--error)'
    barClass = 'danger'
  } else if (ratio > 0.8) {
    statusColor = 'var(--error)'
    barClass = 'danger'
  } else if (ratio > 0.6) {
    statusColor = 'var(--warning)'
    barClass = 'warning'
  }

  const hasEstimate = !!budgetChecked

  return (
    <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
      <button
        ref={buttonRef}
        style={{
          display: 'flex', alignItems: 'center', gap: 6,
          background: 'transparent', border: 'none',
          cursor: 'pointer', padding: '2px 6px',
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
        <div className="budget-bar">
          <div className={`budget-bar-fill ${barClass}`} style={{ width: barWidth }} />
        </div>
        <span style={{
          fontSize: 11, fontFamily: 'var(--font-mono)',
          color: statusColor, transition: 'color 0.3s', whiteSpace: 'nowrap',
        }}>
          {hasEstimate
            ? `${formatTokenCount(budgetChecked!.estimated_input_tokens)} / ${formatTokenCount(budgetChecked!.prompt_budget)}`
            : '预算不可用'}
        </span>
      </button>

      {open && (
        <div ref={popoverRef} className="budget-popover">
          <div className="budget-popover-title">预算明细</div>
          {budgetEstimateFailed ? (
            <div style={{ color: 'var(--error)', marginBottom: 8 }}>{budgetEstimateFailed.message}</div>
          ) : budgetChecked ? (
            <div>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Budget</span>
                <span className="budget-popover-value">{formatTokenCount(budgetChecked.prompt_budget)}</span>
              </div>
              {budgetChecked.context_window && (
                <div className="budget-popover-row">
                  <span className="budget-popover-label">Context Window</span>
                  <span className="budget-popover-value">{formatTokenCount(budgetChecked.context_window)}</span>
                </div>
              )}
              <div className="budget-popover-row">
                <span className="budget-popover-label">Estimated</span>
                <span className="budget-popover-value">{formatTokenCount(budgetChecked.estimated_input_tokens)} ({(ratio * 100).toFixed(1)}%)</span>
              </div>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Action</span>
                <span className="budget-popover-value">{budgetChecked.action}</span>
              </div>
              {budgetChecked.reason && (
                <div className="budget-popover-row">
                  <span className="budget-popover-label">Reason</span>
                  <span className="budget-popover-value">{budgetChecked.reason}</span>
                </div>
              )}
            </div>
          ) : (
            <div style={{ color: 'var(--text-tertiary)' }}>暂无预算数据</div>
          )}
          {ledgerReconciled && (
            <>
              <div style={{ height: 1, background: 'var(--bg-active)', margin: '10px 0' }} />
              <div style={{ fontWeight: 600, fontSize: 12, color: 'var(--text-primary)', marginBottom: 8 }}>Ledger 对账</div>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Input</span>
                <span className="budget-popover-value">{formatTokenCount(ledgerReconciled.input_tokens)} ({ledgerReconciled.input_source})</span>
              </div>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Output</span>
                <span className="budget-popover-value">{formatTokenCount(ledgerReconciled.output_tokens)} ({ledgerReconciled.output_source})</span>
              </div>
              {ledgerReconciled.has_unknown_usage && (
                <div style={{ color: 'var(--warning)', fontSize: 11 }}>⚠ 存在未知用量</div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}
