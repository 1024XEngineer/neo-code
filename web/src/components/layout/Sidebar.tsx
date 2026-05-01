import { useState, useEffect, useCallback } from 'react'
import { useSessionStore } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import {
  Plus,
  Search,
  PanelLeft,
  Filter,
  MessageSquare,
  Folder,
  ChevronRight,
  X,
  Server,
  Cpu,
  Blocks,
} from 'lucide-react'
import { type ProviderOption, type MCPServerParams, type AvailableSkillState, type SessionSkillState, type CreateProviderParams, type ProviderModelDescriptor } from '@/api/protocol'

interface SidebarProps {
  collapsed?: boolean
}

/** 左侧栏：会话列表 */
export default function Sidebar({ collapsed }: SidebarProps) {
  const gatewayAPI = useGatewayAPI()
  // All hooks must be called before any early return (React Rules of Hooks)
  const projects = useSessionStore((s) => s.projects)
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const switchSession = useSessionStore((s) => s.switchSession)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const searchQuery = useUIStore((s) => s.searchQuery)
  const setSearchQuery = useUIStore((s) => s.setSearchQuery)
  const setCurrentProjectId = useSessionStore((s) => s.setCurrentProjectId)

  const [expandedProjects, setExpandedProjects] = useState<Set<string>>(new Set())
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; sessionId: string } | null>(null)
  const [renamingSessionId, setRenamingSessionId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [mcpModalOpen, setMcpModalOpen] = useState(false)
  const [skillModalOpen, setSkillModalOpen] = useState(false)
  const [providerModalOpen, setProviderModalOpen] = useState(false)

  if (!gatewayAPI) return null

  const toggleProject = (projectId: string) => {
    setExpandedProjects((prev) => {
      const next = new Set(prev)
      if (next.has(projectId)) next.delete(projectId)
      else next.add(projectId)
      return next
    })
  }

  const filteredProjects = searchQuery.trim()
    ? projects.map((p) => {
        const projectMatches = p.name.toLowerCase().includes(searchQuery.trim().toLowerCase())
        return {
          ...p,
          sessions: projectMatches
            ? p.sessions
            : p.sessions.filter((s) => s.title.toLowerCase().includes(searchQuery.trim().toLowerCase())),
        }
      }).filter((p) => p.sessions.length > 0)
    : projects

  async function handleSelectSession(sessionId: string, projectId: string) {
    setCurrentProjectId(projectId)
    if (!gatewayAPI) return
    try {
      await switchSession(sessionId, gatewayAPI)
    } catch (err) {
      console.error('Switch session failed:', err)
    }
  }

  function handleNewSession() {
    const store = useSessionStore.getState()
    store.prepareNewChat()
  }

  // Collapsed sidebar strip
  if (collapsed) {
    return (
      <>
        <button style={styles.stripBtn} onClick={toggleSidebar} title="展开侧边栏">
          <PanelLeft size={16} />
        </button>
        <button style={styles.stripBtn} onClick={handleNewSession} title="新对话">
          <Plus size={16} />
        </button>
      </>
    )
  }

  return (
    <div style={styles.container}>
      {/* Collapse Toggle */}
      <button style={styles.collapseBtn} onClick={toggleSidebar} title="收起侧边栏">
        <PanelLeft size={14} />
      </button>

      {/* Top Actions */}
      <div style={styles.topActions}>
        <button style={styles.newChatBtn} onClick={handleNewSession}>
          <Plus size={16} />
          <span>新对话</span>
          <kbd style={styles.shortcut}>Ctrl+N</kbd>
        </button>
        <div style={styles.actionGrid}>
          <ActionButton icon={<Blocks size={16} />} label="MCP" onClick={() => setMcpModalOpen(true)} />
          <ActionButton icon={<Cpu size={16} />} label="Skill" onClick={() => setSkillModalOpen(true)} />
          <ActionButton icon={<Server size={16} />} label="供应商" onClick={() => setProviderModalOpen(true)} />
        </div>
      </div>

      {/* Session List Header */}
      <div style={styles.listHeader}>
        <span style={styles.listTitle}>项目</span>
        <div style={styles.searchBox}>
          <Search size={12} />
          <input
            style={styles.searchInput}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索项目或会话"
          />
        </div>
        <div style={styles.listActions}>
          <button style={styles.iconBtn} title="筛选">
            <Filter size={14} />
          </button>
        </div>
      </div>

      {/* Session List */}
      <div style={styles.scrollArea}>
        {filteredProjects.map((project) => (
          <div key={project.id} style={styles.projectGroup}>
            <button style={styles.projectHeader} onClick={() => toggleProject(project.id)}>
              <span style={{ ...styles.chevron, transform: expandedProjects.has(project.id) ? 'rotate(90deg)' : 'rotate(0deg)' }}>
                <ChevronRight size={14} />
              </span>
              <Folder size={14} />
              <span style={styles.projectName}>{project.name}</span>
            </button>
            {expandedProjects.has(project.id) && (
              <div style={styles.sessionsList}>
                {project.sessions.map((session) => (
                  <SessionItem
                    key={session.id}
                    session={session}
                    isActive={currentSessionId === session.id}
                    onClick={() => handleSelectSession(session.id, project.id)}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      setContextMenu({ x: e.clientX, y: e.clientY, sessionId: session.id })
                    }}
                  />
                ))}
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Context Menu */}
      {contextMenu && (
        <>
          <div style={styles.overlay} onClick={() => setContextMenu(null)} />
          <div style={{ ...styles.contextMenu, left: contextMenu.x, top: contextMenu.y }}>
            <button style={styles.contextItem} onClick={() => {
              setRenamingSessionId(contextMenu.sessionId)
              const proj = projects.find((p) => p.sessions.some((s) => s.id === contextMenu.sessionId))
              const sess = proj?.sessions.find((s) => s.id === contextMenu.sessionId)
              setRenameValue(sess?.title ?? '')
              setContextMenu(null)
            }}>重命名</button>
            <button style={{ ...styles.contextItem, color: 'var(--error)' }} onClick={async () => {
              try {
                await gatewayAPI.deleteSession(contextMenu.sessionId)
                await useSessionStore.getState().fetchSessions(gatewayAPI)
                useSessionStore.getState().prepareNewChat()
              } catch (err) {
                console.error('Delete session failed:', err)
              }
              setContextMenu(null)
            }}>删除</button>
          </div>
        </>
      )}

      {/* Rename Dialog */}
      {renamingSessionId && (
        <>
          <div style={styles.overlay} onClick={() => setRenamingSessionId(null)} />
          <div style={{ ...styles.contextMenu, left: '50%', top: '30%', transform: 'translateX(-50%)', minWidth: 240, padding: 12 }}>
            <input
              style={modalStyles.input}
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              onKeyDown={async (e) => {
                if (e.key === 'Enter' && renameValue.trim()) {
                  try {
                    await gatewayAPI.renameSession(renamingSessionId, renameValue.trim())
                    await useSessionStore.getState().fetchSessions(gatewayAPI)
                  } catch (err) {
                    console.error('Rename session failed:', err)
                  }
                  setRenamingSessionId(null)
                }
                if (e.key === 'Escape') setRenamingSessionId(null)
              }}
              autoFocus
            />
            <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
              <button style={modalStyles.actionBtn} onClick={async () => {
                if (renameValue.trim()) {
                  try {
                    await gatewayAPI.renameSession(renamingSessionId, renameValue.trim())
                    await useSessionStore.getState().fetchSessions(gatewayAPI)
                  } catch (err) {
                    console.error('Rename session failed:', err)
                  }
                }
                setRenamingSessionId(null)
              }}>确认</button>
              <button style={modalStyles.actionBtn} onClick={() => setRenamingSessionId(null)}>取消</button>
            </div>
          </div>
        </>
      )}

      {mcpModalOpen && <McpModal onClose={() => setMcpModalOpen(false)} />}
      {skillModalOpen && <SkillModal onClose={() => setSkillModalOpen(false)} />}
      {providerModalOpen && <ProviderModal onClose={() => setProviderModalOpen(false)} />}
    </div>
  )
}

function ActionButton({ icon, label, onClick }: { icon: React.ReactNode; label: string; onClick: () => void }) {
  return (
    <button style={styles.actionBtn} onClick={onClick}>
      {icon}
      <span style={styles.actionLabel}>{label}</span>
    </button>
  )
}

function SessionItem({
  session,
  isActive,
  onClick,
  onContextMenu,
}: {
  session: { id: string; title: string; time: string }
  isActive: boolean
  onClick: () => void
  onContextMenu: (e: React.MouseEvent) => void
}) {
  return (
    <button
      style={{ ...styles.sessionItem, background: isActive ? 'var(--bg-active)' : 'transparent' }}
      onClick={onClick}
      onContextMenu={onContextMenu}
    >
      <div style={styles.sessionRow}>
        <MessageSquare size={14} />
        <span style={styles.sessionTitle} title={session.title}>{session.title}</span>
      </div>
      <div style={styles.sessionMeta}>
        <span style={styles.sessionTime}>{session.time}</span>
      </div>
    </button>
  )
}

// ---- Modals ----

/** MCP Server 编辑表单初始值 */
function emptyServer(): MCPServerParams {
  return { id: '', enabled: true, stdio: { command: '', args: [], workdir: '' }, env: [] }
}

function McpModal({ onClose }: { onClose: () => void }) {
  const gatewayAPI = useGatewayAPI()
  const [servers, setServers] = useState<MCPServerParams[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<MCPServerParams | null>(null)
  const [isNew, setIsNew] = useState(false)

  const load = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true)
    setError('')
    try {
      const result = await gatewayAPI.listMCPServers()
      setServers(result?.payload?.servers ?? [])
    } catch (err) {
      const msg = err instanceof Error ? err.message : '加载 MCP 配置失败'
      setError(msg)
      console.error('listMCPServers failed:', err)
    } finally {
      setLoading(false)
    }
  }, [gatewayAPI])

  useEffect(() => {
    load()
  }, [load])

  if (!gatewayAPI) return (<div style={modalStyles.overlay} onClick={onClose}><div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}><div style={modalStyles.header}><h3 style={modalStyles.title}>MCP 配置</h3><button style={modalStyles.closeBtn} onClick={onClose}><X size={16} /></button></div><div style={modalStyles.body}><div style={modalStyles.emptyState}>Gateway 未连接，请检查连接状态</div></div></div></div>)

  async function handleToggle(server: MCPServerParams) {
    if (!gatewayAPI) return
    try {
      await gatewayAPI.setMCPServerEnabled(server.id, !server.enabled)
      await load()
    } catch (err) {
      console.error('setMCPServerEnabled failed:', err)
      setError(err instanceof Error ? err.message : '操作失败')
    }
  }

  function handleEdit(server: MCPServerParams) {
    setEditing({ ...server, stdio: server.stdio ?? { command: '', args: [], workdir: '' } })
    setIsNew(false)
  }

  function handleAdd() {
    setEditing(emptyServer())
    setIsNew(true)
  }

  async function handleDelete(serverId: string) {
    if (!gatewayAPI) return
    if (!window.confirm(`确定要删除 MCP Server "${serverId}" 吗？`)) return
    try {
      await gatewayAPI.deleteMCPServer(serverId)
      await load()
    } catch (err) {
      console.error('deleteMCPServer failed:', err)
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  async function handleSave() {
    if (!editing || !gatewayAPI) return
    if (!editing.id.trim()) {
      setError('Server ID 不能为空')
      return
    }
    if (editing.enabled && !editing.stdio?.command?.trim()) {
      setError('启用的 MCP Server 必须填写 Command')
      return
    }
    try {
      await gatewayAPI.upsertMCPServer({ server: editing })
      setEditing(null)
      await load()
    } catch (err) {
      console.error('upsertMCPServer failed:', err)
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  return (
    <div style={modalStyles.overlay} onClick={onClose}>
      <div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={modalStyles.header}>
          <h3 style={modalStyles.title}>MCP 配置</h3>
          <button style={modalStyles.closeBtn} onClick={onClose}>
            <X size={16} />
          </button>
        </div>
        <div style={modalStyles.body}>
          {loading && (
            <div style={modalStyles.emptyState}>加载中...</div>
          )}
          {!loading && error && !editing && (
            <div style={{ ...modalStyles.emptyState, color: 'var(--error)' }}>{error}</div>
          )}
          {!loading && !error && !editing && servers.length === 0 && (
            <div style={modalStyles.emptyState}>暂无已配置的 MCP Server</div>
          )}
          {!editing && servers.map((server) => (
            <div key={server.id} style={modalStyles.providerCard}>
              <div style={modalStyles.providerHeader}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                  <Blocks size={16} />
                  <span style={modalStyles.providerName}>{server.id}</span>
                </div>
                <span style={{ ...modalStyles.statusBadge, background: server.enabled ? 'rgba(22,163,74,0.15)' : 'var(--bg-active)', color: server.enabled ? 'var(--success)' : 'var(--text-tertiary)' }}>
                  {server.enabled ? '已启用' : '未启用'}
                </span>
              </div>
              <div style={modalStyles.description}>{server.source || server.version || ''}</div>
              <div style={modalStyles.providerActions}>
                <button style={{ ...modalStyles.actionBtn, background: server.enabled ? 'var(--error-bg, rgba(239,68,68,0.1))' : 'rgba(22,163,74,0.15)', color: server.enabled ? 'var(--error)' : 'var(--success)' }} onClick={() => handleToggle(server)}>
                  {server.enabled ? '停用' : '启用'}
                </button>
                <button style={modalStyles.actionBtn} onClick={() => handleEdit(server)}>编辑</button>
                <button style={{ ...modalStyles.actionBtn, color: 'var(--error)' }} onClick={() => handleDelete(server.id)}>删除</button>
              </div>
            </div>
          ))}
          {!editing && (
            <div style={{ marginTop: 8 }}>
              <button style={{ ...modalStyles.actionBtn, width: '100%' }} onClick={handleAdd}>+ 新增 MCP Server</button>
            </div>
          )}
          {editing && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>
              {error && (
                <div style={{ color: 'var(--error)', fontSize: 12 }}>{error}</div>
              )}
              <label style={formLabelStyle}>
                ID
                <input
                  style={modalStyles.input}
                  value={editing.id}
                  disabled={!isNew}
                  onChange={(e) => setEditing({ ...editing, id: e.target.value })}
                />
              </label>
              <label style={formLabelStyle}>
                Command
                <input
                  style={modalStyles.input}
                  value={editing.stdio?.command || ''}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, command: e.target.value } })}
                />
              </label>
              <label style={formLabelStyle}>
                Args（以空格分隔）
                <input
                  style={modalStyles.input}
                  value={(editing.stdio?.args || []).join(' ')}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, args: e.target.value.split(' ').filter(Boolean) } })}
                />
              </label>
              <label style={formLabelStyle}>
                Workdir
                <input
                  style={modalStyles.input}
                  value={editing.stdio?.workdir || ''}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, workdir: e.target.value } })}
                />
              </label>
              <div style={formLabelStyle}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <span>环境变量</span>
                  <button style={{ ...modalStyles.actionBtn, fontSize: 11, padding: '2px 6px' }} onClick={() => {
                    const env = [...(editing.env || []), { name: '', value: '' }]
                    setEditing({ ...editing, env })
                  }}>+ 添加</button>
                </div>
                {(editing.env || []).map((ev, idx) => (
                  <div key={idx} style={{ display: 'flex', gap: 4, marginTop: 2 }}>
                    <input
                      style={{ ...modalStyles.input, flex: 1 }}
                      placeholder="NAME"
                      value={ev.name}
                      onChange={(e) => {
                        const env = [...(editing.env || [])]
                        env[idx] = { ...env[idx], name: e.target.value }
                        setEditing({ ...editing, env })
                      }}
                    />
                    <input
                      style={{ ...modalStyles.input, flex: 1 }}
                      placeholder="VALUE"
                      value={ev.value || ''}
                      onChange={(e) => {
                        const env = [...(editing.env || [])]
                        env[idx] = { ...env[idx], value: e.target.value }
                        setEditing({ ...editing, env })
                      }}
                    />
                    <button style={{ ...modalStyles.actionBtn, color: 'var(--error)', padding: '2px 4px', fontSize: 11 }} onClick={() => {
                      const env = (editing.env || []).filter((_, i) => i !== idx)
                      setEditing({ ...editing, env })
                    }}>X</button>
                  </div>
                ))}
              </div>
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <button style={{ ...modalStyles.actionBtn, flex: 1 }} onClick={handleSave}>保存</button>
                <button style={{ ...modalStyles.actionBtn, flex: 1 }} onClick={() => { setEditing(null); setError('') }}>取消</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

const formLabelStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 4,
  fontSize: 12,
  color: 'var(--text-secondary)',
}

function SkillModal({ onClose }: { onClose: () => void }) {
  const gatewayAPI = useGatewayAPI()
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const [availableSkills, setAvailableSkills] = useState<AvailableSkillState[]>([])
  const [sessionSkills, setSessionSkills] = useState<SessionSkillState[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!gatewayAPI) return
    let cancelled = false
    setLoading(true)
    setError('')
    Promise.all([
      gatewayAPI.listAvailableSkills().catch(() => ({ payload: { skills: [] } })),
      currentSessionId ? gatewayAPI.listSessionSkills(currentSessionId).catch(() => ({ payload: { skills: [] } })) : Promise.resolve({ payload: { skills: [] } }),
    ])
      .then(([availResult, sessResult]) => {
        if (!cancelled) {
          setAvailableSkills((availResult.payload.skills as AvailableSkillState[]) || [])
          setSessionSkills((sessResult.payload.skills as SessionSkillState[]) || [])
        }
      })
      .catch((err) => {
        if (!cancelled) {
          const msg = err instanceof Error ? err.message : '加载 Skill 列表失败'
          setError(msg)
          console.error('listSkills failed:', err)
        }
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [gatewayAPI, currentSessionId])

  if (!gatewayAPI) return (<div style={modalStyles.overlay} onClick={onClose}><div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}><div style={modalStyles.header}><h3 style={modalStyles.title}>Skill 配置</h3><button style={modalStyles.closeBtn} onClick={onClose}><X size={16} /></button></div><div style={modalStyles.body}><div style={modalStyles.emptyState}>Gateway 未连接，请检查连接状态</div></div></div></div>)

  const sessionSkillIds = new Set(sessionSkills.map((s) => s.skill_id))

  async function handleToggleSkill(skillId: string, enabled: boolean) {
    if (!currentSessionId) {
      setError('请先选择一个会话再操作 Skill')
      return
    }
    if (!gatewayAPI) {
      setError('Gateway 未连接')
      return
    }
    try {
      if (enabled) {
        await gatewayAPI.deactivateSessionSkill(currentSessionId, skillId)
      } else {
        await gatewayAPI.activateSessionSkill(currentSessionId, skillId)
      }
      // 刷新列表
      const [availResult, sessResult] = await Promise.all([
        gatewayAPI.listAvailableSkills().catch(() => ({ payload: { skills: [] } })),
        gatewayAPI.listSessionSkills(currentSessionId).catch(() => ({ payload: { skills: [] } })),
      ])
      setAvailableSkills((availResult.payload.skills as AvailableSkillState[]) || [])
      setSessionSkills((sessResult.payload.skills as SessionSkillState[]) || [])
    } catch (err) {
      console.error('toggleSkill failed:', err)
      setError(err instanceof Error ? err.message : '操作失败')
    }
  }

  return (
    <div style={modalStyles.overlay} onClick={onClose}>
      <div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={modalStyles.header}>
          <h3 style={modalStyles.title}>Skill 配置</h3>
          <button style={modalStyles.closeBtn} onClick={onClose}>
            <X size={16} />
          </button>
        </div>
        <div style={modalStyles.body}>
          {loading && (
            <div style={modalStyles.emptyState}>加载中...</div>
          )}
          {!loading && error && (
            <div style={{ ...modalStyles.emptyState, color: 'var(--error)' }}>{error}</div>
          )}
          {!loading && !error && availableSkills.length === 0 && (
            <div style={modalStyles.emptyState}>暂无可用 Skill</div>
          )}
          {!loading && availableSkills.map((skill) => {
            const skillId = skill.descriptor.id
            const enabled = skill.active || sessionSkillIds.has(skillId)
            return (
              <div key={skillId} style={modalStyles.providerCard}>
                <div style={modalStyles.providerHeader}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                    <Cpu size={16} />
                    <span style={modalStyles.providerName}>{skill.descriptor.name || skillId}</span>
                  </div>
                  <span style={{ ...modalStyles.statusBadge, background: enabled ? 'rgba(22,163,74,0.15)' : 'var(--bg-active)', color: enabled ? 'var(--success)' : 'var(--text-tertiary)' }}>
                    {enabled ? '已启用' : '未启用'}
                  </span>
                </div>
                <div style={modalStyles.description}>{skill.descriptor.description || ''}</div>
                <div style={modalStyles.providerActions}>
                  <button
                    style={{ ...modalStyles.actionBtn, background: enabled ? 'var(--error-bg, rgba(239,68,68,0.1))' : 'rgba(22,163,74,0.15)', color: enabled ? 'var(--error)' : 'var(--success)' }}
                    onClick={() => handleToggleSkill(skillId, enabled)}
                    disabled={!currentSessionId}
                  >
                    {enabled ? '停用' : '启用'}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function emptyProviderForm(): CreateProviderParams & { modelsJSON?: string } {
  return {
    name: '',
    driver: 'openaicompat',
    model_source: 'discover',
    chat_api_mode: 'chat_completions',
    base_url: '',
    chat_endpoint_path: '/chat/completions',
    discovery_endpoint_path: '/models',
    api_key_env: '',
    api_key: '',
    modelsJSON: '',
  }
}

function ProviderModal({ onClose }: { onClose: () => void }) {
  const gatewayAPI = useGatewayAPI()
  const [providers, setProviders] = useState<ProviderOption[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [formData, setFormData] = useState<CreateProviderParams & { modelsJSON?: string }>(emptyProviderForm)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true)
    setError('')
    try {
      const result = await gatewayAPI.listProviders()
      setProviders(result?.payload?.providers ?? [])
    } catch (err) {
      const msg = err instanceof Error ? err.message : '加载供应商列表失败'
      setError(msg)
      console.error('listProviders failed:', err)
    } finally {
      setLoading(false)
    }
  }, [gatewayAPI])

  useEffect(() => {
    load()
  }, [load])

  async function handleSelect(providerId: string) {
    if (!gatewayAPI) return
    try {
      await gatewayAPI.selectProviderModel({ provider_id: providerId })
      useGatewayStore.getState().notifyProviderChanged()
      await load()
    } catch (err) {
      console.error('selectProviderModel failed:', err)
      setError(err instanceof Error ? err.message : '切换供应商失败')
    }
  }

  async function handleDelete(providerId: string, source: string) {
    if (!gatewayAPI) return
    if (source !== 'custom') return
    if (!window.confirm(`确定要删除供应商 "${providerId}" 吗？`)) return
    try {
      await gatewayAPI.deleteCustomProvider(providerId)
      await load()
    } catch (err) {
      console.error('deleteCustomProvider failed:', err)
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  function handleAdd() {
    setFormData(emptyProviderForm())
    setFormError('')
    setShowForm(true)
  }

  function handleDriverChange(driver: string) {
    setFormData(prev => {
      const next = { ...prev, driver }
      if (driver === 'openaicompat') {
        next.chat_endpoint_path = '/chat/completions'
        next.base_url = ''
        next.chat_api_mode = 'chat_completions'
      } else if (driver === 'gemini') {
        next.base_url = 'https://generativelanguage.googleapis.com/v1beta'
        next.chat_endpoint_path = '/models'
        next.chat_api_mode = ''
      } else if (driver === 'anthropic') {
        next.base_url = 'https://api.anthropic.com/v1'
        next.chat_endpoint_path = '/messages'
        next.chat_api_mode = ''
      }
      return next
    })
  }

  function handleChatModeChange(mode: string) {
    setFormData(prev => {
      const next = { ...prev, chat_api_mode: mode }
      if (mode === 'responses') {
        next.chat_endpoint_path = '/responses'
      } else {
        next.chat_endpoint_path = '/chat/completions'
      }
      return next
    })
  }

  function validateForm(): string {
    const name = formData.name.trim()
    const driver = formData.driver.trim()
    const modelSource = (formData.model_source || 'discover').trim()
    const apiKey = (formData.api_key || '').trim()
    const apiKeyEnv = formData.api_key_env.trim()

    if (!name) return '名称不能为空'
    if (!driver) return 'Driver 不能为空'
    if (!modelSource) return '模型来源不能为空'
    if (!apiKey) return 'API Key 不能为空'
    if (!apiKeyEnv) return 'API Key 环境变量不能为空'
    if (!/^[A-Z][A-Z0-9_]*$/.test(apiKeyEnv)) {
      return 'API Key 环境变量名不合法（需大写字母、数字、下划线，且以大写字母开头）'
    }

    if (modelSource === 'manual') {
      const json = (formData.modelsJSON || '').trim()
      if (!json) return '手动模式下模型 JSON 不能为空'
      try {
        const parsed = JSON.parse(json)
        if (!Array.isArray(parsed)) return '模型 JSON 必须是数组'
        for (const m of parsed) {
          if (!m.id || !m.name) return '每个模型必须包含 id 和 name 字段'
        }
      } catch {
        return '模型 JSON 格式错误'
      }
    }

    if (modelSource === 'discover' && driver === 'openaicompat') {
      const discoveryPath = (formData.discovery_endpoint_path || '').trim()
      if (!discoveryPath) return '自动发现模式下发现端点路径不能为空'
    }

    return ''
  }

  async function handleSave() {
    if (!gatewayAPI) return
    const err = validateForm()
    if (err) {
      setFormError(err)
      return
    }

    const { modelsJSON: _, ...payload }: CreateProviderParams & { modelsJSON?: string } = { ...formData }
    // 填充默认值
    if (!payload.base_url?.trim()) {
      if (payload.driver === 'openaicompat') payload.base_url = 'https://api.openai.com/v1'
      else if (payload.driver === 'gemini') payload.base_url = 'https://generativelanguage.googleapis.com/v1beta'
      else if (payload.driver === 'anthropic') payload.base_url = 'https://api.anthropic.com/v1'
    }
    if (!payload.chat_endpoint_path?.trim()) {
      payload.chat_endpoint_path = formData.driver === 'gemini' ? '/models' : formData.driver === 'anthropic' ? '/messages' : '/chat/completions'
    }
    if (!payload.discovery_endpoint_path?.trim() && payload.model_source !== 'manual') {
      payload.discovery_endpoint_path = '/models'
    }

    if (payload.model_source === 'manual' && formData.modelsJSON) {
      try {
        payload.models = JSON.parse(formData.modelsJSON) as ProviderModelDescriptor[]
      } catch {
        setFormError('模型 JSON 解析失败')
        return
      }
    }

    setSaving(true)
    setFormError('')
    try {
      await gatewayAPI.createCustomProvider(payload)
      setShowForm(false)
      await load()
    } catch (err) {
      console.error('createCustomProvider failed:', err)
      setFormError(err instanceof Error ? err.message : '创建失败')
    } finally {
      setSaving(false)
    }
  }

  if (!gatewayAPI) return (
    <div style={modalStyles.overlay} onClick={onClose}>
      <div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={modalStyles.header}>
          <h3 style={modalStyles.title}>供应商设置</h3>
          <button style={modalStyles.closeBtn} onClick={onClose}><X size={16} /></button>
        </div>
        <div style={modalStyles.body}>
          <div style={modalStyles.emptyState}>Gateway 未连接，请检查连接状态</div>
        </div>
      </div>
    </div>
  )

  return (
    <div style={modalStyles.overlay} onClick={onClose}>
      <div style={modalStyles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={modalStyles.header}>
          <h3 style={modalStyles.title}>供应商设置</h3>
          <button style={modalStyles.closeBtn} onClick={onClose}><X size={16} /></button>
        </div>
        <div style={modalStyles.body}>
          {loading && <div style={modalStyles.emptyState}>加载中...</div>}
          {!loading && error && !showForm && <div style={{ ...modalStyles.emptyState, color: 'var(--error)' }}>{error}</div>}
          {!loading && !showForm && providers.length === 0 && <div style={modalStyles.emptyState}>暂无已配置的供应商</div>}
          {!showForm && providers.map((p) => (
            <div key={p.id} style={{
              ...modalStyles.providerCard,
              ...(p.selected ? { border: '1px solid var(--success)' } : {}),
            }}>
              <div style={modalStyles.providerHeader}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Server size={16} />
                  <span style={modalStyles.providerName}>{p.name}</span>
                </div>
                <span style={{ ...modalStyles.statusBadge, background: p.selected ? 'rgba(22,163,74,0.15)' : 'var(--bg-active)', color: p.selected ? 'var(--success)' : 'var(--text-tertiary)' }}>
                  {p.selected ? '已启用' : '未启用'}
                </span>
              </div>
              <div style={modalStyles.providerModels}>
                {p.models?.map((m) => (
                  <span key={m.id} style={modalStyles.modelTag}>{m.name || m.id}</span>
                ))}
              </div>
              <div style={modalStyles.providerActions}>
                <button
                  style={{ ...modalStyles.actionBtn, background: p.selected ? 'var(--bg-active)' : 'rgba(22,163,74,0.15)', color: p.selected ? 'var(--text-tertiary)' : 'var(--success)' }}
                  onClick={() => handleSelect(p.id)}
                  disabled={p.selected}
                >
                  {p.selected ? '当前使用' : '选择'}
                </button>
                {p.source === 'custom' && (
                  <button style={{ ...modalStyles.actionBtn, color: 'var(--error)' }} onClick={() => handleDelete(p.id, p.source)}>删除</button>
                )}
              </div>
            </div>
          ))}
          {!showForm && (
            <div style={{ marginTop: 8 }}>
              <button style={{ ...modalStyles.actionBtn, width: '100%' }} onClick={handleAdd}>+ 新增 Provider</button>
            </div>
          )}
          {showForm && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>
              {formError && <div style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</div>}
              <label style={formLabelStyle}>
                名称 *
                <input style={modalStyles.input} value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} placeholder="例如：my-openai" />
              </label>
              <label style={formLabelStyle}>
                Driver *
                <select style={modalStyles.select} value={formData.driver} onChange={(e) => handleDriverChange(e.target.value)}>
                  <option value="openaicompat">OpenAI Compatible</option>
                  <option value="gemini">Gemini</option>
                  <option value="anthropic">Anthropic</option>
                </select>
              </label>
              <label style={formLabelStyle}>
                模型来源 *
                <select style={modalStyles.select} value={formData.model_source || 'discover'} onChange={(e) => setFormData({ ...formData, model_source: e.target.value })}>
                  <option value="discover">自动发现</option>
                  <option value="manual">手动配置</option>
                </select>
              </label>
              {formData.driver === 'openaicompat' && (
                <label style={formLabelStyle}>
                  Chat API 模式 *
                  <select style={modalStyles.select} value={formData.chat_api_mode || 'chat_completions'} onChange={(e) => handleChatModeChange(e.target.value)}>
                    <option value="chat_completions">Chat Completions</option>
                    <option value="responses">Responses</option>
                  </select>
                </label>
              )}
              <label style={formLabelStyle}>
                Base URL
                <input style={modalStyles.input} value={formData.base_url || ''} onChange={(e) => setFormData({ ...formData, base_url: e.target.value })} placeholder="例如：https://api.openai.com/v1" />
              </label>
              <label style={formLabelStyle}>
                Chat Endpoint Path
                <input style={modalStyles.input} value={formData.chat_endpoint_path || ''} onChange={(e) => setFormData({ ...formData, chat_endpoint_path: e.target.value })} placeholder="/chat/completions" />
              </label>
              {(formData.model_source || 'discover') !== 'manual' && (
                <label style={formLabelStyle}>
                  发现端点路径
                  <input style={modalStyles.input} value={formData.discovery_endpoint_path || ''} onChange={(e) => setFormData({ ...formData, discovery_endpoint_path: e.target.value })} placeholder="/models" />
                </label>
              )}
              <label style={formLabelStyle}>
                API Key 环境变量 *
                <input style={modalStyles.input} value={formData.api_key_env} onChange={(e) => setFormData({ ...formData, api_key_env: e.target.value })} placeholder="例如：OPENAI_API_KEY" />
              </label>
              <label style={formLabelStyle}>
                API Key *
                <input type="password" style={modalStyles.input} value={formData.api_key || ''} onChange={(e) => setFormData({ ...formData, api_key: e.target.value })} placeholder="sk-..." />
              </label>
              {(formData.model_source || 'discover') === 'manual' && (
                <label style={formLabelStyle}>
                  手动模型 JSON
                  <textarea
                    style={{ ...modalStyles.input, minHeight: 80, resize: 'vertical' }}
                    value={formData.modelsJSON || ''}
                    onChange={(e) => setFormData({ ...formData, modelsJSON: e.target.value })}
                    placeholder='[{"id":"gpt-4","name":"GPT-4"}]'
                  />
                </label>
              )}
              <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
                <button style={{ ...modalStyles.actionBtn, flex: 1 }} onClick={handleSave} disabled={saving}>
                  {saving ? '保存中...' : '保存'}
                </button>
                <button style={{ ...modalStyles.actionBtn, flex: 1 }} onClick={() => { setShowForm(false); setFormError('') }}>
                  取消
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    background: 'var(--bg-secondary)',
    position: 'relative',
  },
  collapseBtn: {
    position: 'absolute',
    top: 8,
    right: 8,
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
    zIndex: 2,
    opacity: 0.6,
    transition: 'opacity 0.15s',
  },
  stripBtn: {
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
    flexShrink: 0,
  },
  topActions: {
    padding: '12px 32px 12px 12px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
  },
  newChatBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    width: '100%',
    padding: '8px 12px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-primary)',
    fontSize: 13,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
    marginBottom: 8,
  },
  shortcut: {
    marginLeft: 'auto',
    fontSize: 10,
    padding: '2px 6px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-active)',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
  },
  actionGrid: {
    display: 'grid',
    gridTemplateColumns: '1fr 1fr 1fr',
    gap: 4,
  },
  actionBtn: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 4,
    padding: '8px 4px',
    borderRadius: 'var(--radius-md)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    fontSize: 11,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
  },
  actionLabel: {
    fontSize: 11,
  },
  listHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 6,
    padding: '10px 8px 6px 12px',
    flexShrink: 0,
  },
  listTitle: {
    flexShrink: 0,
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-tertiary)',
    textTransform: 'uppercase',
    letterSpacing: '0.5px',
  },
  searchBox: {
    flex: 1,
    minWidth: 0,
    display: 'flex',
    alignItems: 'center',
    gap: 5,
    height: 26,
    padding: '0 8px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
    color: 'var(--text-tertiary)',
  },
  searchInput: {
    flex: 1,
    minWidth: 0,
    border: 'none',
    outline: 'none',
    background: 'transparent',
    color: 'var(--text-primary)',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  listActions: {
    display: 'flex',
    gap: 2,
    flexShrink: 0,
  },
  iconBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  scrollArea: {
    flex: 1,
    overflowY: 'auto',
    padding: '0 6px',
  },
  projectGroup: {
    marginBottom: 2,
  },
  projectHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    width: '100%',
    padding: '6px 8px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
    transition: 'all 0.15s',
  },
  chevron: {
    display: 'flex',
    transition: 'transform 0.2s',
    color: 'var(--text-tertiary)',
  },
  projectName: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  sessionsList: {
    paddingLeft: 8,
  },
  sessionItem: {
    display: 'flex',
    flexDirection: 'column',
    width: '100%',
    padding: '6px 8px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    color: 'var(--text-secondary)',
    fontSize: 12,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
    gap: 2,
    transition: 'all 0.15s',
    marginBottom: 1,
  },
  sessionRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
  },
  sessionTitle: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    flex: 1,
  },
  sessionMeta: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingLeft: 20,
  },
  sessionTime: {
    fontSize: 11,
    color: 'var(--text-tertiary)',
  },
  msgCount: {
    fontSize: 10,
    color: 'var(--text-tertiary)',
    padding: '1px 5px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-active)',
  },
  overlay: {
    position: 'fixed',
    inset: 0,
    zIndex: 100,
  },
  contextMenu: {
    position: 'fixed',
    zIndex: 101,
    background: 'var(--bg-tertiary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    padding: '4px',
    minWidth: 140,
    boxShadow: 'var(--shadow-3)',
  },
  contextItem: {
    display: 'block',
    width: '100%',
    padding: '6px 10px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-primary)',
    fontSize: 12,
    textAlign: 'left',
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
  },
  contextDivider: {
    height: 1,
    background: 'var(--border-primary)',
    margin: '4px 0',
  },
}

const modalStyles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.5)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 200,
  },
  modal: {
    width: 480,
    maxHeight: '80vh',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-lg)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    boxShadow: 'var(--shadow-3)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '14px 16px',
    borderBottom: '1px solid var(--border-primary)',
  },
  title: {
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
    margin: 0,
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
  },
  body: {
    padding: '12px',
    overflowY: 'auto',
  },
  providerCard: {
    padding: '12px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
    marginBottom: 8,
  },
  providerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 8,
  },
  providerName: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  statusBadge: {
    fontSize: 10,
    padding: '2px 8px',
    borderRadius: 'var(--radius-sm)',
    fontWeight: 500,
  },
  providerModels: {
    display: 'flex',
    gap: 6,
    marginBottom: 10,
    flexWrap: 'wrap',
  },
  description: {
    color: 'var(--text-secondary)',
    fontSize: 12,
    marginBottom: 10,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  modelTag: {
    fontSize: 11,
    padding: '2px 8px',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-active)',
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
  },
  providerActions: {
    display: 'flex',
    gap: 8,
  },
  input: {
    flex: 1,
    padding: '6px 10px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    outline: 'none',
  },
  actionBtn: {
    padding: '6px 14px',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
  },
  select: {
    flex: 1,
    padding: '6px 10px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    outline: 'none',
    cursor: 'pointer',
  },
  emptyState: {
    padding: '20px 0',
    textAlign: 'center' as const,
    color: 'var(--text-tertiary)',
    fontSize: 13,
    fontFamily: 'var(--font-ui)',
  },
}
