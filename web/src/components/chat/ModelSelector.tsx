import { useState, useEffect } from 'react'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useUIStore } from '@/stores/useUIStore'
import { type ModelEntry } from '@/api/protocol'
import { ChevronDown, Loader2 } from 'lucide-react'

export default function ModelSelector() {
  const gatewayAPI = useGatewayAPI()
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const isGenerating = useChatStore((s) => s.isGenerating)
  const providerChangeTick = useGatewayStore((s) => s.providerChangeTick)
  const [open, setOpen] = useState(false)
  const [models, setModels] = useState<ModelEntry[]>([])
  const [selected, setSelected] = useState<ModelEntry | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [pendingModelChange, setPendingModelChange] = useState<ModelEntry | null>(null)

  async function applyModelSelection(model: ModelEntry) {
    if (!gatewayAPI) return
    if (currentSessionId) {
      await gatewayAPI.setSessionModel(currentSessionId, model.id, model.provider)
      return
    }
    await gatewayAPI.selectProviderModel({ provider_id: model.provider, model_id: model.id })
    useGatewayStore.getState().notifyProviderChanged()
  }

  // 模型列表加载：直接在 effect 中 fetch，用 cancelled flag 防止陈旧更新
  useEffect(() => {
    if (!gatewayAPI) return
    let cancelled = false
    setLoading(true)
    setError('')
    gatewayAPI.listModels(currentSessionId || undefined)
      .then((result) => {
        if (cancelled) return
        const fetched = result.payload.models
        setModels(fetched)
        if (fetched.length > 0) {
          const effective = fetched.find((entry) => (
            entry.id === result.payload.selected_model_id
            && entry.provider === result.payload.selected_provider_id
          )) ?? null
          setSelected(effective)
        } else {
          setSelected(null)
        }
      })
      .catch((err) => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load model list')
        console.error('listModels failed:', err)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [gatewayAPI, currentSessionId, providerChangeTick])

  async function handleSelect(m: ModelEntry) {
    setSelected(m)
    setOpen(false)
    if (isGenerating) {
      setPendingModelChange(m)
      useUIStore.getState().showToast('Model change will apply on the next turn', 'info')
      return
    }
    try {
      await applyModelSelection(m)
    } catch (err) {
      console.error('applyModelSelection failed:', err)
    }
  }

  // 生成完成后补发延迟的模型切换
  useEffect(() => {
    if (!isGenerating && pendingModelChange && gatewayAPI) {
      applyModelSelection(pendingModelChange)
        .catch((err) => console.error('Deferred applyModelSelection failed:', err))
      setPendingModelChange(null)
    }
  }, [isGenerating, pendingModelChange, currentSessionId, gatewayAPI])

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
