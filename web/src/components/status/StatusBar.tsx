import { useRuntime } from '@/context/RuntimeProvider'
import { FolderOpen } from 'lucide-react'

export default function StatusBar() {
  const { mode, workdir, selectWorkdir } = useRuntime()

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
      <div className="status-bar-right" />
    </div>
  )
}
