import { useChatStore } from '@/stores/useChatStore'
import { useRuntime } from '@/context/RuntimeProvider'
import { formatTokenCount } from '@/utils/format'
import { FolderOpen } from 'lucide-react'
import BudgetIndicator from './BudgetIndicator'

export default function StatusBar() {
  const tokenUsage = useChatStore((s) => s.tokenUsage)
  const { mode, workdir, selectWorkdir } = useRuntime()

  const totalTokens = tokenUsage ? tokenUsage.input_tokens + tokenUsage.output_tokens : 0

  async function handleChangeWorkdir() {
    if (mode !== 'electron') return
    await selectWorkdir()
  }

  return (
    <div className="status-bar">
      <div className="status-bar-left">
        {mode === 'electron' && workdir && (
          <button
            onClick={handleChangeWorkdir}
            title="点击切换工作区"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              border: 'none',
              background: 'transparent',
              color: 'var(--text-tertiary)',
              cursor: 'pointer',
              fontSize: 11,
              fontFamily: 'var(--font-mono)',
              maxWidth: 320,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              padding: 0,
            }}
          >
            <FolderOpen size={12} />
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{workdir}</span>
          </button>
        )}
      </div>
      <div className="status-bar-right">
        <BudgetIndicator />
        {tokenUsage && (
          <>
            <span style={{ width: 1, height: 12, background: 'var(--bg-active)' }} />
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>
              Tokens: {formatTokenCount(totalTokens)}
            </span>
          </>
        )}
      </div>
    </div>
  )
}
