import { useState, useEffect, useCallback } from 'react'
import { type GatewayAPI } from '@/api/gateway'
import { type AvailableSkillState } from '@/api/protocol'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { X, Sparkles, Power, PowerOff, Loader2 } from 'lucide-react'

interface SkillPickerProps {
  gatewayAPI: GatewayAPI | null
  sessionId: string
  onClose: () => void
}

/** 技能管理弹层 */
export default function SkillPicker({ gatewayAPI, sessionId, onClose }: SkillPickerProps) {
  const isGenerating = useChatStore((s) => s.isGenerating)
  const [skills, setSkills] = useState<AvailableSkillState[]>([])
  const [loading, setLoading] = useState(true)
  const [togglingId, setTogglingId] = useState<string | null>(null)

  const fetchSkills = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true)
    try {
      const result = await gatewayAPI.listAvailableSkills(sessionId || undefined)
      setSkills(result.payload?.skills || [])
    } catch (err) {
      console.error('Failed to list skills:', err)
      useUIStore.getState().showToast('Failed to load skills', 'error')
    } finally {
      setLoading(false)
    }
  }, [gatewayAPI, sessionId])

  useEffect(() => {
    fetchSkills()
  }, [fetchSkills])

  const handleToggle = async (skillId: string, currentlyActive: boolean) => {
    if (!gatewayAPI || !sessionId) {
      useUIStore.getState().showToast('Send a message first to start a session', 'error')
      return
    }
    setTogglingId(skillId)
    try {
      if (currentlyActive) {
        await gatewayAPI.deactivateSessionSkill(sessionId, skillId)
      } else {
        await gatewayAPI.activateSessionSkill(sessionId, skillId)
      }
      // 刷新列表
      await fetchSkills()
    } catch (err) {
      console.error('Failed to toggle skill:', err)
      useUIStore.getState().showToast('Skill operation failed', 'error')
    } finally {
      setTogglingId(null)
    }
  }

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <div style={styles.headerTitle}>
            <Sparkles size={16} />
            <span>技能管理</span>
          </div>
          <button style={styles.closeBtn} onClick={onClose} title="关闭">
            <X size={16} />
          </button>
        </div>

        <div style={styles.content}>
          {loading && (
            <div style={styles.center}>
              <Loader2 size={20} style={{ animation: 'spin 1s linear infinite' } as React.CSSProperties} />
              <span style={styles.emptyText}>加载中...</span>
            </div>
          )}

          {!loading && skills.length === 0 && (
            <div style={styles.center}>
              <span style={styles.emptyText}>暂无可用技能</span>
            </div>
          )}

          {!loading && skills.map((skill) => {
            const desc = skill.descriptor
            const isToggling = togglingId === desc.id
            const anyToggling = !!togglingId
            return (
              <div key={desc.id} style={styles.skillRow}>
                <div style={styles.skillInfo}>
                  <div style={styles.skillName}>{desc.name || desc.id}</div>
                  {desc.description && <div style={styles.skillDesc}>{desc.description}</div>}
                  <div style={styles.skillMeta}>
                    <span style={styles.metaTag}>{desc.scope || 'explicit'}</span>
                    {desc.version && <span style={styles.metaTag}>v{desc.version}</span>}
                    {desc.source && <span style={styles.metaTag}>{desc.source.kind}{desc.source.layer ? `/${desc.source.layer}` : ''}</span>}
                  </div>
                </div>
                <button
                  style={{
                    ...styles.toggleBtn,
                    background: skill.active ? 'var(--accent-muted)' : 'var(--bg-tertiary)',
                    color: skill.active ? 'var(--accent)' : 'var(--text-tertiary)',
                    opacity: isGenerating || anyToggling ? 0.5 : 1,
                    cursor: isGenerating || anyToggling ? 'not-allowed' : 'pointer',
                  }}
                  onClick={() => !isGenerating && !anyToggling && handleToggle(desc.id, skill.active)}
                  disabled={isToggling || isGenerating || anyToggling}
                  title={isGenerating ? '生成中无法切换技能' : anyToggling ? '请等待当前操作完成' : skill.active ? '停用技能' : '激活技能'}
                >
                  {isToggling ? (
                    <Loader2 size={14} style={{ animation: 'spin 1s linear infinite' } as React.CSSProperties} />
                  ) : skill.active ? (
                    <Power size={14} />
                  ) : (
                    <PowerOff size={14} />
                  )}
                  <span>{skill.active ? '已激活' : '激活'}</span>
                </button>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.4)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 200,
    backdropFilter: 'blur(2px)',
  },
  modal: {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-xl)',
    width: '90%',
    maxWidth: 480,
    maxHeight: '70vh',
    display: 'flex',
    flexDirection: 'column',
    boxShadow: '0 8px 32px rgba(0,0,0,0.2)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    borderBottom: '1px solid var(--border-primary)',
  },
  headerTitle: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 28,
    height: 28,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  content: {
    overflowY: 'auto',
    padding: '8px 4px',
    flex: 1,
  },
  center: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    padding: '32px 16px',
    color: 'var(--text-tertiary)',
  },
  emptyText: {
    fontSize: 13,
    color: 'var(--text-tertiary)',
  },
  skillRow: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    padding: '10px 12px',
    margin: '0 4px',
    borderRadius: 'var(--radius-md)',
    transition: 'background 0.1s',
  },
  skillInfo: {
    display: 'flex',
    flexDirection: 'column',
    gap: 3,
    minWidth: 0,
    flex: 1,
  },
  skillName: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  skillDesc: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    lineHeight: 1.5,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    display: '-webkit-box',
    WebkitLineClamp: 2,
    WebkitBoxOrient: 'vertical',
  } as React.CSSProperties,
  skillMeta: {
    display: 'flex',
    gap: 6,
    marginTop: 2,
  },
  metaTag: {
    fontSize: 10,
    fontWeight: 500,
    color: 'var(--text-tertiary)',
    background: 'var(--bg-tertiary)',
    padding: '2px 6px',
    borderRadius: 'var(--radius-sm)',
    fontFamily: 'var(--font-mono)',
  },
  toggleBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '5px 10px',
    borderRadius: 'var(--radius-md)',
    border: 'none',
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    transition: 'all 0.15s',
    flexShrink: 0,
    whiteSpace: 'nowrap',
  },
}
