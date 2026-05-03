import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { GatewayAPI } from '@/api/gateway'
import { createWSClient, type WSClient, type WSConnectionState } from '@/api/wsClient'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore, isValidSessionId } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'
import { handleGatewayEvent } from '@/utils/eventBridge'

const browserRuntimeStorageKey = 'neocode.browserRuntimeConfig'
export const defaultBrowserGatewayBaseURL = 'http://127.0.0.1:8080'

const PING_INTERVAL_MS = 5 * 60 * 1000 // 5 minutes (well within 15-min binding TTL)

export type RuntimeMode = 'electron' | 'browser'
export type RuntimeStatus = 'loading' | 'needs_config' | 'connecting' | 'connected' | 'error'

/** RuntimeConfig 描述当前前端连接 Gateway 所需的最小运行时配置。 */
export interface RuntimeConfig {
  mode: RuntimeMode
  gatewayBaseURL: string
  token: string
}

interface BrowserConnectInput {
  gatewayBaseURL: string
  token: string
}

interface RuntimeContextValue {
  mode: RuntimeMode
  status: RuntimeStatus
  config: RuntimeConfig | null
  gatewayAPI: GatewayAPI | null
  wsClient: WSClient | null
  connectionState: WSConnectionState
  error: string
  loadingMessage: string
  vitePluginAvailable: boolean
  defaultBrowserGatewayBaseURL: string
  workdir: string
  connectBrowser: (input: BrowserConnectInput) => Promise<void>
  startLocalGateway: (port: number) => Promise<void>
  selectWorkdir: () => Promise<string>
  retry: () => Promise<void>
  resetBrowserConfig: () => void
}

const RuntimeContext = createContext<RuntimeContextValue | null>(null)

/** RuntimeProvider 装配前端运行时，并为业务组件提供当前 Gateway 客户端。 */
export function RuntimeProvider({ children }: { children: ReactNode }) {
  const mode = useMemo(detectRuntimeMode, [])
  const theme = useUIStore((s) => s.theme)
  const [status, setStatusRaw] = useState<RuntimeStatus>('loading')
  const setStatus = useCallback((s: RuntimeStatus) => {
    statusRef.current = s
    setStatusRaw(s)
  }, [])
  const [config, setConfig] = useState<RuntimeConfig | null>(null)
  const [gatewayAPI, setGatewayAPI] = useState<GatewayAPI | null>(null)
  const [wsClient, setWsClient] = useState<WSClient | null>(null)
  const [connectionState, setLocalConnectionState] = useState<WSConnectionState>('disconnected')
  const [error, setError] = useState('')
  const [loadingMessage, setLoadingMessage] = useState('正在连接 Gateway...')
  const [vitePluginAvailable, setVitePluginAvailable] = useState(false)
  const [workdir, setWorkdir] = useState('')
  const cleanupRef = useRef<(() => void) | null>(null)
  const pingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const initializedRef = useRef(false)
  const statusRef = useRef<RuntimeStatus>('loading')

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  const clearRuntimeState = useCallback(() => {
    cleanupRef.current?.()
    cleanupRef.current = null
    if (pingIntervalRef.current) {
      clearInterval(pingIntervalRef.current)
      pingIntervalRef.current = null
    }
    setGatewayAPI(null)
    setWsClient(null)
    setLocalConnectionState('disconnected')
    useGatewayStore.getState().reset()
  }, [])

  const connectWithConfig = useCallback(async (nextConfig: RuntimeConfig, persistBrowserConfig: boolean) => {
    clearRuntimeState()
    setStatus('connecting')
    setError('')
    setConfig(nextConfig)

    const client = createWSClient({
      baseURL: nextConfig.gatewayBaseURL,
      token: nextConfig.token,
    })

    const api = new GatewayAPI(client)

    // Register state change handler
    const unsubState = client.onStateChange((wsState) => {
      setLocalConnectionState(wsState)
      useGatewayStore.getState().setConnectionState(wsState)

      if (wsState === 'error' || wsState === 'permanent_error') {
        // Reset stuck generating state on disconnect
        useChatStore.getState().resetGeneratingState()
        useGatewayStore.getState().setAuthenticated(false)
        setStatus('error')
        if (wsState === 'permanent_error') {
          setError('Gateway 连接失败，已超过最大重连次数')
        }
      } else if (wsState === 'connected' && statusRef.current !== 'connected') {
        // Reconnection recovery: if we were in error state, mark as connected
        setStatus('connected')
      }
    })

    // Register event handler
    const unsubEvent = client.onEvent((frame) => handleGatewayEvent(frame, api))

    // Register reconnect handler — re-establish bindStream and refresh data
    const unsubReconnect = client.onReconnect(async () => {
      try {
        // Re-authenticate on new connection
        await api.authenticate(nextConfig.token)
        useGatewayStore.getState().setAuthenticated(true)

        // Re-bind stream for current session (skip temporary IDs)
        const sessionId = useSessionStore.getState().currentSessionId
        if (isValidSessionId(sessionId)) {
          await api.bindStream({ session_id: sessionId, channel: 'all' })
        }

        // Refresh workspace list (best-effort) and session list
        try {
          await useWorkspaceStore.getState().fetchWorkspaces(api)
        } catch (workspaceErr) {
          console.warn('[RuntimeProvider] reconnect fetchWorkspaces failed:', workspaceErr)
        }
        await useSessionStore.getState().fetchSessions(api)

        // Restore connected status after successful reconnect
        setStatus('connected')
        setError('')
      } catch (reconnectErr) {
        console.error('[RuntimeProvider] Reconnect failed:', reconnectErr)
        setError(formatRuntimeError(reconnectErr))
      }
    })

    cleanupRef.current = () => {
      unsubState()
      unsubEvent()
      unsubReconnect()
      client.disconnect()
    }

    try {
      useGatewayStore.getState().setToken(nextConfig.token)

      // Open WebSocket connection
      client.connect()

      // Authenticate over WS
      await api.authenticate(nextConfig.token)
      useGatewayStore.getState().setAuthenticated(true)

      // Fetch workspaces (best-effort; gracefully degrades if backend not upgraded)
      try {
        await useWorkspaceStore.getState().fetchWorkspaces(api)
      } catch (workspaceErr) {
        console.warn('[RuntimeProvider] fetchWorkspaces failed, falling back to single workspace:', workspaceErr)
      }

      // Fetch sessions and initialize
      await useSessionStore.getState().fetchSessions(api)
      await useSessionStore.getState().initializeActiveSession(api)

      // Persist browser config if appropriate
      if (persistBrowserConfig && nextConfig.mode === 'browser') {
        saveBrowserRuntimeConfig(nextConfig)
      }

      // Start ping heartbeat to keep stream binding alive
      pingIntervalRef.current = setInterval(() => {
        api.ping().catch((err) => {
          console.warn('[RuntimeProvider] Ping failed:', err)
        })
      }, PING_INTERVAL_MS)

      setGatewayAPI(api)
      setWsClient(client)
      setStatus('connected')
    } catch (connectErr) {
      cleanupRef.current?.()
      cleanupRef.current = null
      useGatewayStore.getState().setAuthenticated(false)
      setGatewayAPI(null)
      setWsClient(null)
      setStatus('error')
      setError(formatRuntimeError(connectErr))
    }
  }, [clearRuntimeState])

  const connectBrowser = useCallback(async (input: BrowserConnectInput) => {
    await connectWithConfig({
      mode: 'browser',
      gatewayBaseURL: normalizeGatewayBaseURL(input.gatewayBaseURL),
      token: input.token.trim(),
    }, true)
  }, [connectWithConfig])

  const selectWorkdir = useCallback(async () => {
    if (!window.electronAPI || mode !== 'electron') return workdir
    try {
      const result = await window.electronAPI.selectWorkdir()
      if (!result.canceled) {
        setWorkdir(result.workdir)
      }
      return result.workdir
    } catch (err) {
      console.error('selectWorkdir failed:', err)
      return workdir
    }
  }, [mode, workdir])

  const startLocalGateway = useCallback(async (port: number) => {
    setError('')
    try {
      const res = await fetch('/__neocode_dev_config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ port }),
        signal: AbortSignal.timeout(30000),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({})) as { error?: string }
        throw new Error(data.error || `启动失败 (HTTP ${res.status})`)
      }
      const data = await res.json() as { gatewayBaseURL?: string; token?: string }
      if (!data.gatewayBaseURL) throw new Error('未获取到 Gateway 地址')
      await connectWithConfig({
        mode: 'browser',
        gatewayBaseURL: data.gatewayBaseURL,
        token: data.token || '',
      }, true)
    } catch (err) {
      setError(formatRuntimeError(err))
    }
  }, [connectWithConfig])

  const retry = useCallback(async () => {
    if (config) {
      await connectWithConfig(config, config.mode === 'browser')
      return
    }
    setStatus(mode === 'browser' ? 'needs_config' : 'loading')
  }, [config, connectWithConfig, mode])

  const resetBrowserConfig = useCallback(() => {
    sessionStorage.removeItem(browserRuntimeStorageKey)
    clearRuntimeState()
    useChatStore.getState().clearMessages()
    useSessionStore.getState().setProjects([])
    useSessionStore.getState().setCurrentSessionId('')
    useSessionStore.getState().setCurrentProjectId('')
    useWorkspaceStore.getState().setWorkspaces([])
    useWorkspaceStore.getState().setCurrentWorkspaceHash('')
    setConfig(null)
    setError('')
    setStatus('needs_config')
  }, [clearRuntimeState])

  useEffect(() => {
    if (initializedRef.current) return
    initializedRef.current = true

    if (mode === 'electron') {
      loadElectronRuntimeConfig()
        .then(async (electronConfig) => {
          const electronWorkdir = window.electronAPI ? await window.electronAPI.getWorkdir().catch(() => '') : ''
          setWorkdir(electronWorkdir)
          return electronConfig
        })
        .then((electronConfig) => connectWithConfig(electronConfig, false))
        .catch((loadErr) => {
          setStatus('error')
          setError(formatRuntimeError(loadErr))
        })
      return
    }

    const browserConfig = loadBrowserRuntimeConfig()
    if (browserConfig?.gatewayBaseURL) {
      connectWithConfig(browserConfig, false).catch(() => {})
    } else {
      setLoadingMessage('正在启动本地 Gateway...')
      tryAutoDetectLocalGateway()
        .then(({ config: autoConfig, pluginAvailable }) => {
          setVitePluginAvailable(pluginAvailable)
          if (autoConfig) {
            connectWithConfig(autoConfig, true).catch(() => setStatus('needs_config'))
          } else {
            setStatus('needs_config')
          }
        })
        .catch(() => setStatus('needs_config'))
    }
  }, [connectWithConfig, mode])

  useEffect(() => {
    return () => {
      cleanupRef.current?.()
      cleanupRef.current = null
      if (pingIntervalRef.current) {
        clearInterval(pingIntervalRef.current)
        pingIntervalRef.current = null
      }
    }
  }, [])

  const value = useMemo<RuntimeContextValue>(() => ({
    mode,
    status,
    config,
    gatewayAPI,
    wsClient,
    connectionState,
    error,
    loadingMessage,
    vitePluginAvailable,
    defaultBrowserGatewayBaseURL,
    workdir,
    connectBrowser,
    startLocalGateway,
    selectWorkdir,
    retry,
    resetBrowserConfig,
  }), [
    mode,
    status,
    config,
    gatewayAPI,
    wsClient,
    connectionState,
    error,
    loadingMessage,
    vitePluginAvailable,
    workdir,
    connectBrowser,
    startLocalGateway,
    selectWorkdir,
    retry,
    resetBrowserConfig,
  ])

  return (
    <RuntimeContext.Provider value={value}>
      {children}
    </RuntimeContext.Provider>
  )
}

/** useRuntime 读取当前前端运行时上下文。 */
export function useRuntime() {
  const runtime = useContext(RuntimeContext)
  if (!runtime) {
    throw new Error('useRuntime must be used within RuntimeProvider')
  }
  return runtime
}

/** useGatewayAPI 返回当前 Gateway 客户端，未连接时返回 null。 */
export function useGatewayAPI(): GatewayAPI | null {
  const runtime = useRuntime()
  return runtime.gatewayAPI
}

/** detectRuntimeMode 根据 preload 暴露能力判断当前运行环境。 */
function detectRuntimeMode(): RuntimeMode {
  return window.electronAPI ? 'electron' : 'browser'
}

/** loadElectronRuntimeConfig 从 Electron preload 读取 Gateway 地址与 token。 */
async function loadElectronRuntimeConfig(): Promise<RuntimeConfig> {
  if (!window.electronAPI) {
    throw new Error('Electron API is unavailable')
  }
  const [address, token] = await Promise.all([
    window.electronAPI.getAddress(),
    window.electronAPI.getToken(),
  ])
  return {
    mode: 'electron',
    gatewayBaseURL: normalizeGatewayBaseURL(address),
    token: token.trim(),
  }
}

/** loadBrowserRuntimeConfig 从 sessionStorage 读取浏览器端连接配置。 */
function loadBrowserRuntimeConfig(): RuntimeConfig | null {
  const raw = sessionStorage.getItem(browserRuntimeStorageKey)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as Partial<RuntimeConfig>
    if (parsed.mode !== 'browser' || !parsed.gatewayBaseURL) return null
    return {
      mode: 'browser',
      gatewayBaseURL: normalizeGatewayBaseURL(parsed.gatewayBaseURL),
      token: (parsed.token ?? '').trim(),
    }
  } catch {
    return null
  }
}

/** saveBrowserRuntimeConfig 将浏览器连接配置保存为会话级数据。 */
function saveBrowserRuntimeConfig(nextConfig: RuntimeConfig) {
  sessionStorage.setItem(browserRuntimeStorageKey, JSON.stringify(nextConfig))
}

/** normalizeGatewayBaseURL 将裸地址归一化为 HTTP Gateway 基础地址。 */
function normalizeGatewayBaseURL(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return defaultBrowserGatewayBaseURL
  const withProtocol = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`
  return withProtocol.replace(/\/+$/, '')
}

/** formatRuntimeError 将未知错误转换为可展示的连接错误文案。 */
function formatRuntimeError(err: unknown) {
  if (err instanceof Error && err.message) {
    return err.message
  }
  return 'Gateway 连接失败'
}

/** tryAutoDetectLocalGateway 尝试自动检测本地 Gateway 连接配置。 */
async function tryAutoDetectLocalGateway(): Promise<{ config: RuntimeConfig | null; pluginAvailable: boolean }> {
  const maxRetries = 60 // 最多等 60 秒（Go 编译在 Windows 上可能较慢）
  let sawPluginResponse = false

  for (let i = 0; i < maxRetries; i++) {
    try {
      const res = await fetch('/__neocode_dev_config', { signal: AbortSignal.timeout(3000) })
      sawPluginResponse = true
      if (res.ok) {
        const data = await res.json() as { gatewayBaseURL?: string; token?: string; available?: boolean }
        if (data.gatewayBaseURL) {
          return { config: { mode: 'browser', gatewayBaseURL: data.gatewayBaseURL, token: data.token || '' }, pluginAvailable: true }
        }
      }
      // 503 = 插件在位但 gateway 尚未就绪，继续等待
      if (res.status === 503) {
        await new Promise((r) => setTimeout(r, 1000))
        continue
      }
      // 其他非 ok 响应，插件可能不兼容，退出轮询
      if (!res.ok) break
    } catch {
      // fetch 失败（Vite 还没启动 / 网络错误），短暂等待后重试
      await new Promise((r) => setTimeout(r, 1000))
    }
  }

  try {
    const res = await fetch('http://127.0.0.1:8080/healthz', { signal: AbortSignal.timeout(3000) })
    if (res.ok) {
      return { config: { mode: 'browser', gatewayBaseURL: 'http://127.0.0.1:8080', token: '' }, pluginAvailable: false }
    }
  } catch { /* gateway 未运行，忽略 */ }

  return { config: null, pluginAvailable: sawPluginResponse }
}
