import { useState, useEffect, useCallback, useRef } from 'react'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { useSessionStore } from '@/stores/useSessionStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { type ModelEntry } from '@/api/protocol'
import { ChevronDown, Loader2 } from 'lucide-react'

export default function ModelSelector() {
  const gatewayAPI = useGatewayAPI()
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const providerChangeTick = useGatewayStore((s) => s.providerChangeTick)
  const [open, setOpen] = useState(false)
  const [models, setModels] = useState<ModelEntry[]>([])
  const [selected, setSelected] = useState<ModelEntry | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // 追踪 provider 切换，避免闭包陷阱
  const providerTickRef = useRef(providerChangeTick)

  const loadModels = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true)
    setError('')
    try {
      const result = await gatewayAPI.listModels(currentSessionId || undefined)
      const fetched = result.payload.models
      setModels(fetched)

      // 检测 provider 是否切换
      const isProviderChanged = providerChangeTick !== providerTickRef.current
      if (isProviderChanged) {
        providerTickRef.current = providerChangeTick
      }

      if (fetched.length > 0) {
        setSelected((prev) => {
          if (!prev) {
            return fetched[0]
          }
          if (isProviderChanged) {
            const retained = fetched.find((f) => f.id === prev.id)
            return retained ?? fetched[0]
          }
          return prev
        })
      } else {
        setSelected(null)
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '加载模型列表失败'
      setError(msg)
      console.error('listModels failed:', err)
    } finally {
      setLoading(false)
    }
  }, [gatewayAPI, currentSessionId, providerChangeTick])

  useEffect(() => {
    loadModels()
  }, [loadModels, providerChangeTick])

  async function handleSelect(m: ModelEntry) {
    setSelected(m)
    setOpen(false)
    if (currentSessionId && gatewayAPI) {
      try {
        await gatewayAPI.setSessionModel(currentSessionId, m.id, m.provider)
      } catch (err) {
        console.error('setSessionModel failed:', err)
      }
    }
  }

  if (!gatewayAPI) return null

  return (
    <div style={{ position: 'relative' }}>
      <button
        style={styles.selectorBtn}
        onClick={() => setOpen(!open)}
        disabled={loading}
      >
        {loading ? (
          <Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} />
        ) : (
          <>
            <span style={styles.modelName}>{selected ? `${selected.provider} / ${selected.name}` : (error || '无可用模型')}</span>
            <ChevronDown size={14} style={{ color: 'var(--text-tertiary)', transition: 'transform 0.15s', transform: open ? 'rotate(180deg)' : 'none' }} />
          </>
        )}
      </button>

      {open && (
        <div style={styles.dropdown} onMouseLeave={() => setOpen(false)}>
          {models.length === 0 && !error && (
            <div style={styles.emptyItem}>无可用模型</div>
          )}
          {error && (
            <div style={styles.emptyItem}>加载失败</div>
          )}
          {models.map((m) => (
            <button
              key={m.id}
              style={{
                ...styles.dropdownItem,
                background: selected?.id === m.id ? 'var(--bg-active)' : 'transparent',
              }}
              onClick={() => handleSelect(m)}
            >
              <span style={styles.dropdownName}>{m.name}</span>
              <span style={styles.dropdownProvider}>{m.provider}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  selectorBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 10px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  modelName: {
    fontWeight: 500,
  },
  dropdown: {
    position: 'absolute',
    top: 'calc(100% + 6px)',
    right: 0,
    width: 220,
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    padding: 4,
    boxShadow: 'var(--shadow-3)',
    zIndex: 60,
    maxHeight: 320,
    overflowY: 'auto',
  },
  dropdownItem: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    width: '100%',
    padding: '6px 10px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    color: 'var(--text-primary)',
    fontSize: 13,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    textAlign: 'left',
    transition: 'all 0.15s',
  },
  emptyItem: {
    padding: '6px 10px',
    fontSize: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-ui)',
  },
  dropdownName: {
    fontWeight: 500,
  },
  dropdownProvider: {
    fontSize: 11,
    color: 'var(--text-tertiary)',
  },
}
