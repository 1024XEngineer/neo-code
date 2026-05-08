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

  async function handleSelect(model: ModelEntry) {
    setSelected(model)
    setOpen(false)
    if (isGenerating) {
      setPendingModelChange(model)
      useUIStore.getState().showToast('Model change will apply on the next turn', 'info')
      return
    }
    try {
      await applyModelSelection(model)
    } catch (err) {
      console.error('applyModelSelection failed:', err)
    }
  }

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
      <button className="model-selector" onClick={() => setOpen(!open)} disabled={loading}>
        {loading ? (
          <Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} />
        ) : (
          <>
            <span className="model-dot" />
            <span>{selected ? `${selected.provider} / ${selected.name}` : (error || '无可用模型')}</span>
            <ChevronDown
              size={14}
              style={{
                color: 'var(--text-tertiary)',
                transition: 'transform 0.15s',
                transform: open ? 'rotate(180deg)' : 'none',
              }}
            />
          </>
        )}
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            bottom: 'calc(100% + 6px)',
            right: 0,
            width: 240,
            background: 'var(--bg-overlay)',
            borderRadius: 'var(--radius-md)',
            padding: 4,
            boxShadow: 'var(--shadow-elevated)',
            zIndex: 60,
            maxHeight: 320,
            overflowY: 'auto',
          }}
          onMouseLeave={() => setOpen(false)}
        >
          {models.length === 0 && !error && (
            <div style={{ padding: '6px 10px', fontSize: 12, color: 'var(--text-tertiary)' }}>无可用模型</div>
          )}
          {error && (
            <div style={{ padding: '6px 10px', fontSize: 12, color: 'var(--text-tertiary)' }}>加载失败</div>
          )}
          {models.map((model) => (
            <button
              key={model.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                width: '100%',
                padding: '7px 10px',
                borderRadius: 'var(--radius-sm)',
                border: 'none',
                background: selected?.id === model.id ? 'var(--bg-hover)' : 'transparent',
                color: 'var(--text-primary)',
                fontSize: 13,
                fontFamily: 'var(--font-ui)',
                cursor: 'pointer',
                textAlign: 'left',
                transition: 'all var(--duration-fast)',
              }}
              onClick={() => handleSelect(model)}
            >
              <span style={{ fontWeight: 500 }}>{model.name}</span>
              <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>{model.provider}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
