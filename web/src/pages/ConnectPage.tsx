import { useState } from 'react'
import { useRuntime } from '@/context/RuntimeProvider'
import { Zap, AlertCircle, Loader, Server } from 'lucide-react'

/** 浏览器端 Gateway 连接配置页 */
export default function ConnectPage() {
  const { connectBrowser, startLocalGateway, retry, error, status, vitePluginAvailable, defaultBrowserGatewayBaseURL: defaultURL } = useRuntime()
  const [gatewayBaseURL, setGatewayBaseURL] = useState(defaultURL)
  const [token, setToken] = useState('')
  const [localPort, setLocalPort] = useState('8080')
  const [localError, setLocalError] = useState('')
  const [localStarting, setLocalStarting] = useState(false)

  const isConnecting = status === 'connecting'
  const isError = status === 'error'

  async function handleConnect(e: React.FormEvent) {
    e.preventDefault()
    await connectBrowser({ gatewayBaseURL, token })
  }

  async function handleStartLocal(e: React.FormEvent) {
    e.preventDefault()
    setLocalError('')
    const port = parseInt(localPort, 10)
    if (isNaN(port) || port < 1 || port > 65535) {
      setLocalError('Please enter a valid port (1-65535)')
      return
    }
    setLocalStarting(true)
    await startLocalGateway(port)
    // startLocalGateway 不改全局 status，连接成功后 App.tsx 会自动切到 ChatPage
    // 连接失败时 error 会被设置，组件仍然显示
    setLocalStarting(false)
  }

  return (
    <div style={styles.container}>
      <div style={styles.card}>
        <div style={styles.header}>
          <Zap size={28} style={{ color: 'var(--accent)' }} />
          <h1 style={styles.title}>NeoCode</h1>
        </div>

        {vitePluginAvailable && (
          <>
            <p style={styles.subtitle}>启动本地 Gateway 服务</p>
            <form onSubmit={handleStartLocal} style={styles.form}>
              <div style={styles.localRow}>
                <label style={{ ...styles.label, flex: '0 0 auto' }}>
                  端口
                  <input
                    type="number"
                    value={localPort}
                    onChange={(e) => setLocalPort(e.target.value)}
                    min={1}
                    max={65535}
                    disabled={localStarting}
                    style={{ ...styles.input, width: 90, fontFamily: 'var(--font-mono)' }}
                  />
                </label>
                <button
                  type="submit"
                  disabled={localStarting}
                  style={{
                    ...styles.startBtn,
                    opacity: localStarting ? 0.5 : 1,
                    cursor: localStarting ? 'wait' : 'pointer',
                  }}
                >
                  {localStarting ? (
                    <>
                      <Loader size={14} style={{ animation: 'spin 1s linear infinite' }} />
                      Starting...
                    </>
                  ) : (
                    <>
                      <Server size={14} />
                      启动并连接
                    </>
                  )}
                </button>
              </div>
              {localError && (
                <div style={styles.errorBox}>
                  <AlertCircle size={14} />
                  <span>{localError}</span>
                </div>
              )}
            </form>

            <div style={styles.divider}>
              <span style={styles.dividerText}>或手动连接远端 Gateway</span>
            </div>
          </>
        )}

        {!vitePluginAvailable && (
          <p style={styles.subtitle}>连接到 Gateway 服务</p>
        )}

        <form onSubmit={handleConnect} style={styles.form}>
          <label style={styles.label}>
            Gateway 地址
            <input
              type="text"
              value={gatewayBaseURL}
              onChange={(e) => setGatewayBaseURL(e.target.value)}
              placeholder={defaultURL}
              disabled={isConnecting}
              style={styles.input}
            />
          </label>

          <label style={styles.label}>
            Token（本地模式可留空）
            <input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="可选"
              disabled={isConnecting}
              style={styles.input}
            />
          </label>

          {(isError || localError) && error && (
            <div style={styles.errorBox}>
              <AlertCircle size={14} />
              <span>{error}</span>
            </div>
          )}

          <button
            type="submit"
            disabled={isConnecting || !gatewayBaseURL.trim()}
            style={{
              ...styles.connectBtn,
              opacity: isConnecting || !gatewayBaseURL.trim() ? 0.5 : 1,
              cursor: isConnecting ? 'wait' : 'pointer',
            }}
          >
            {isConnecting ? (
              <>
                <Loader size={16} style={{ animation: 'spin 1s linear infinite' }} />
                Connecting...
              </>
            ) : (
              '连接'
            )}
          </button>

          {isError && (
            <button type="button" onClick={retry} style={styles.retryBtn}>
              重试
            </button>
          )}
        </form>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: '100dvh',
    height: '100dvh',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
  },
  card: {
    width: 400,
    padding: 32,
    borderRadius: 'var(--radius-lg)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    boxShadow: 'var(--shadow-3)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    marginBottom: 4,
  },
  title: {
    fontSize: 22,
    fontWeight: 700,
    margin: 0,
  },
  subtitle: {
    fontSize: 13,
    color: 'var(--text-tertiary)',
    margin: '0 0 24px',
  },
  form: {
    display: 'flex',
    flexDirection: 'column',
    gap: 16,
  },
  localRow: {
    display: 'flex',
    alignItems: 'flex-end',
    gap: 12,
  },
  label: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    fontSize: 13,
    fontWeight: 500,
  },
  input: {
    padding: '8px 12px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    fontSize: 14,
    fontFamily: 'var(--font-mono)',
    outline: 'none',
  },
  startBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: '8px 16px',
    borderRadius: 'var(--radius-md)',
    border: 'none',
    background: 'var(--accent)',
    color: '#fff',
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    whiteSpace: 'nowrap',
    height: 37,
  },
  divider: {
    display: 'flex',
    alignItems: 'center',
    margin: '20px 0 4px',
    gap: 12,
  },
  dividerText: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    whiteSpace: 'nowrap',
  },
  errorBox: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '8px 12px',
    borderRadius: 'var(--radius-md)',
    background: 'var(--error-bg, rgba(239,68,68,0.1))',
    color: 'var(--error)',
    fontSize: 12,
  },
  connectBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: '10px 16px',
    borderRadius: 'var(--radius-md)',
    border: 'none',
    background: 'var(--accent)',
    color: '#fff',
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  retryBtn: {
    padding: '8px 16px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'transparent',
    color: 'var(--text-primary)',
    fontSize: 13,
    cursor: 'pointer',
  },
}
