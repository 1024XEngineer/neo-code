import { useState } from 'react'
import { useRuntime } from '@/context/RuntimeProvider'
import { Zap, AlertCircle, Loader, Server } from 'lucide-react'

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
    if (isNaN(port) || port < 1 || port > 65535) { setLocalError('Please enter a valid port (1-65535)'); return }
    setLocalStarting(true)
    await startLocalGateway(port)
    setLocalStarting(false)
  }

  return (
    <div className="connect-page">
      <div className="connect-card">
        <div className="connect-header">
          <Zap size={28} style={{ color: 'var(--accent)' }} />
          <h1 className="connect-title">NeoCode</h1>
        </div>

        {vitePluginAvailable && (
          <>
            <p className="connect-subtitle">启动本地 Gateway 服务</p>
            <form onSubmit={handleStartLocal} className="connect-form">
              <div style={{ display: 'flex', alignItems: 'flex-end', gap: 12 }}>
                <label className="form-label" style={{ flex: '0 0 auto' }}>
                  端口
                  <input type="number" value={localPort}
                    onChange={(e) => setLocalPort(e.target.value)}
                    min={1} max={65535} disabled={localStarting}
                    className="form-input" style={{ width: 90, fontFamily: 'var(--font-mono)' }}
                  />
                </label>
                <button type="submit" disabled={localStarting}
                  className="btn btn-primary"
                  style={{ opacity: localStarting ? 0.5 : 1, height: 37, whiteSpace: 'nowrap' }}
                >
                  {localStarting ? <><Loader size={14} style={{ animation: 'spin 1s linear infinite' }} /> Starting...</> : <><Server size={14} /> 启动并连接</>}
                </button>
              </div>
              {localError && (
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 12px', borderRadius: 'var(--radius-md)', background: 'var(--error-muted)', color: 'var(--error)', fontSize: 12 }}>
                  <AlertCircle size={14} /><span>{localError}</span>
                </div>
              )}
            </form>
            <div className="connect-divider">
              <span style={{ flex: 1, height: 1, background: 'var(--bg-active)' }} />
              <span>或手动连接远端 Gateway</span>
              <span style={{ flex: 1, height: 1, background: 'var(--bg-active)' }} />
            </div>
          </>
        )}

        {!vitePluginAvailable && <p className="connect-subtitle">连接到 Gateway 服务</p>}

        <form onSubmit={handleConnect} className="connect-form">
          <label className="form-label">
            Gateway 地址
            <input type="text" value={gatewayBaseURL}
              onChange={(e) => setGatewayBaseURL(e.target.value)}
              placeholder={defaultURL} disabled={isConnecting}
              className="form-input" style={{ fontFamily: 'var(--font-mono)' }}
            />
          </label>
          <label className="form-label">
            Token（本地模式可留空）
            <input type="password" value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="可选" disabled={isConnecting}
              className="form-input"
            />
          </label>
          {(isError || localError) && error && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 12px', borderRadius: 'var(--radius-md)', background: 'var(--error-muted)', color: 'var(--error)', fontSize: 12 }}>
              <AlertCircle size={14} /><span>{error}</span>
            </div>
          )}
          <button type="submit" disabled={isConnecting || !gatewayBaseURL.trim()}
            className="btn btn-primary"
            style={{ opacity: isConnecting || !gatewayBaseURL.trim() ? 0.5 : 1, padding: '10px 16px', fontSize: 14 }}
          >
            {isConnecting ? <><Loader size={16} style={{ animation: 'spin 1s linear infinite' }} /> Connecting...</> : '连接'}
          </button>
          {isError && (
            <button type="button" onClick={retry} className="btn btn-secondary">重试</button>
          )}
        </form>
      </div>
    </div>
  )
}
