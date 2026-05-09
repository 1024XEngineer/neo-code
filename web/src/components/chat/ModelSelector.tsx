import { useState, useEffect, useRef } from 'react'
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
  const [confirmedSelected, setConfirmedSelected] = useState<ModelEntry | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [pendingModelChange, setPendingModelChange] = useState<ModelEntry | null>(null)
  const latestRefreshRequestId = useRef(0)

  // 统一刷新模型列表，并以服务端返回的当前选择为准同步本地状态。
  async function refreshModels() {
    if (!gatewayAPI) return null
    const requestId = ++latestRefreshRequestId.current
    setLoading(true)
    setError('')
    try {
      const result = await gatewayAPI.listModels(currentSessionId || undefined)
      if (requestId !== latestRefreshRequestId.current) return null
      const fetched = result.payload.models
      setModels(fetched)
      const effective = fetched.find((entry) => (
        entry.id === result.payload.selected_model_id
        && entry.provider === result.payload.selected_provider_id
      )) ?? null
      setSelected(effective)
      setConfirmedSelected(effective)
      return effective
    } catch (err) {
      if (requestId !== latestRefreshRequestId.current) return null
      setError(err instanceof Error ? err.message : 'Failed to load model list')
      console.error('listModels failed:', err)
      return null
    } finally {
      if (requestId === latestRefreshRequestId.current) {
        setLoading(false)
      }
    }
  }

  async function applyModelSelection(model: ModelEntry) {
    if (!gatewayAPI) return
    await gatewayAPI.selectProviderModel({ provider_id: model.provider, model_id: model.id })
    useGatewayStore.getState().notifyProviderChanged()
  }

  useEffect(() => {
    if (!gatewayAPI) return
    void refreshModels()
    return () => {
      latestRefreshRequestId.current += 1
    }
  }, [gatewayAPI, currentSessionId, providerChangeTick])

  async function handleSelect(model: ModelEntry) {
    const previousConfirmed = confirmedSelected
    setSelected(model)
    setOpen(false)
    if (isGenerating) {
      setPendingModelChange(model)
      useUIStore.getState().showToast('Model change will apply on the next turn', 'info')
      return
    }
    try {
      await applyModelSelection(model)
      setConfirmedSelected(model)
    } catch (err) {
      setSelected(previousConfirmed)
      useUIStore.getState().showToast('Failed to apply model change', 'error')
      console.error('applyModelSelection failed:', err)
    }
  }

  useEffect(() => {
    if (!isGenerating && pendingModelChange && gatewayAPI) {
      const previousConfirmed = confirmedSelected
      applyModelSelection(pendingModelChange)
        .then(() => {
          setConfirmedSelected(pendingModelChange)
        })
        .catch(async (err) => {
          setSelected(previousConfirmed)
          useUIStore.getState().showToast('Failed to apply model change', 'error')
          console.error('Deferred applyModelSelection failed:', err)
          await refreshModels()
        })
        .finally(() => {
          setPendingModelChange(null)
        })
    }
  }, [isGenerating, pendingModelChange, confirmedSelected, currentSessionId, gatewayAPI])

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
