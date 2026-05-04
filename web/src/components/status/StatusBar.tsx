import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useRuntime } from '@/context/RuntimeProvider'
import { formatTokenCount } from '@/utils/format'
import { Sun, Moon, Wifi, WifiOff, Loader, FolderOpen } from 'lucide-react'
import BudgetIndicator from './BudgetIndicator'

/** 连接状态图标 */
function ConnectionIcon({ state }: { state: string }) {
  switch (state) {
    case 'connected':
      return <Wifi size={12} style={{ color: 'var(--success)' }} />
    case 'connecting':
      return <Loader size={12} style={{ color: 'var(--warning)' }} />
    case 'error':
      return <WifiOff size={12} style={{ color: 'var(--error)' }} />
    default:
      return <WifiOff size={12} />
  }
}

/** 底部状态栏 */
export default function StatusBar() {
  const tokenUsage = useChatStore((s) => s.tokenUsage)
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const connectionState = useGatewayStore((s) => s.connectionState)
  const authenticated = useGatewayStore((s) => s.authenticated)
  const { mode, workdir, selectWorkdir } = useRuntime()

  const totalTokens = tokenUsage ? tokenUsage.input_tokens + tokenUsage.output_tokens : 0

  async function handleChangeWorkdir() {
    if (mode !== 'electron') return
    await selectWorkdir()
  }

  return (
    <div style={styles.container}>
      <div style={styles.left}>
        <ConnectionIcon state={connectionState} />
        <span style={styles.connLabel}>
          {connectionState === 'connected' ? (authenticated ? '已连接' : '未认证') :
           connectionState === 'connecting' ? '连接中...' :
           connectionState === 'error' ? '连接失败' : '未连接'}
        </span>
        {mode === 'electron' && workdir && (
          <>
            <span style={styles.divider} />
            <button
              style={styles.workdirBtn}
              onClick={handleChangeWorkdir}
              title="点击切换工作区"
            >
              <FolderOpen size={12} />
              <span style={styles.workdirLabel}>{workdir}</span>
            </button>
          </>
        )}
      </div>
      <div style={styles.center}>
        <span style={styles.hint}>NeoCode 可能会生成不准确的信息，请验证重要代码。</span>
      </div>
      <div style={styles.right}>
        <BudgetIndicator />
        {tokenUsage && (
          <>
            <span style={styles.divider} />
            <span style={styles.tokenInfo}>
              Tokens: {formatTokenCount(totalTokens)}
            </span>
          </>
        )}
        <span style={styles.divider} />
        <button
          style={styles.themeBtn}
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          title={theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'}
        >
          {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
        </button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '4px 16px',
    borderTop: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    fontSize: 11,
    color: 'var(--text-tertiary)',
    flexShrink: 0,
    height: 28,
  },
  left: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  connLabel: {
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  center: {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  hint: {
    fontSize: 11,
    color: 'var(--text-tertiary)',
  },
  right: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  divider: {
    width: 1,
    height: 12,
    background: 'var(--border-primary)',
  },
  tokenInfo: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
  },
  themeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 22,
    height: 22,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  workdirBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    maxWidth: 280,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    padding: 0,
  },
  workdirLabel: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
}
