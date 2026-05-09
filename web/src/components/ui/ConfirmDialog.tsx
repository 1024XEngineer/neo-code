import { AlertTriangle, X } from 'lucide-react'

interface ConfirmDialogProps {
  title: string
  description: string
  confirmLabel?: string
  cancelLabel?: string
  variant?: 'danger' | 'warning' | 'default'
  onConfirm: () => void
  onCancel: () => void
}

/** 通用二次确认弹窗 —— restore / undo 等不可逆操作使用 */
export default function ConfirmDialog({
  title,
  description,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  variant = 'default',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const confirmBg =
    variant === 'danger'
      ? 'var(--error)'
      : variant === 'warning'
      ? 'var(--warning)'
      : 'var(--accent)'

  const confirmColor =
    variant === 'default'
      ? '#ffffff'
      : '#111111'

  const toneColor =
    variant === 'danger'
      ? 'var(--error)'
      : variant === 'warning'
      ? 'var(--warning)'
      : 'var(--accent)'

  return (
    <div style={styles.overlay} onClick={onCancel}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <div style={styles.titleRow}>
            <span style={{ ...styles.toneIcon, color: toneColor }}>
              <AlertTriangle size={16} />
            </span>
            <h3 style={styles.title}>{title}</h3>
          </div>
          <button style={styles.closeBtn} onClick={onCancel}>
            <X size={16} />
          </button>
        </div>
        <div style={styles.body}>
          <p style={styles.description}>{description}</p>
          <div style={styles.actions}>
            <button style={styles.cancelBtn} onClick={onCancel}>
              {cancelLabel}
            </button>
            <button
              style={{ ...styles.confirmBtn, background: confirmBg, color: confirmColor }}
              onClick={onConfirm}
            >
              {confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.64)',
    backdropFilter: 'blur(2px)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 200,
  },
  modal: {
    width: 420,
    maxWidth: '90vw',
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-lg)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    boxShadow: 'var(--shadow-modal)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    borderBottom: '1px solid var(--border-primary)',
  },
  titleRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  toneIcon: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 'var(--radius-sm)',
    background: 'rgba(148,163,184,0.12)',
    flexShrink: 0,
  },
  title: {
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
    margin: 0,
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 28,
    height: 28,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
  },
  body: {
    padding: '16px',
    display: 'flex',
    flexDirection: 'column',
    gap: 16,
  },
  description: {
    margin: 0,
    fontSize: 14,
    lineHeight: 1.6,
    color: 'var(--text-primary)',
    whiteSpace: 'pre-wrap',
  },
  actions: {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 8,
  },
  cancelBtn: {
    padding: '8px 14px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-surface)',
    color: 'var(--text-primary)',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
  },
  confirmBtn: {
    padding: '8px 16px',
    borderRadius: 'var(--radius-md)',
    border: 'none',
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
  },
}
