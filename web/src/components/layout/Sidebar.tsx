import { useState, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useWorkspaceStore, type Workspace } from '@/stores/useWorkspaceStore'
import { useGatewayAPI, useRuntime } from '@/context/RuntimeProvider'
import {
  Plus,
  Search,
  PanelLeft,
  MessageSquare,
  Folder,
  FolderPlus,
  ChevronRight,
  Pencil,
  Trash2,
  X,
  Server,
  Cpu,
  Blocks,
  Sun,
  Moon,
} from 'lucide-react'
import ConfirmDialog from '@/components/ui/ConfirmDialog'
import { formatSessionTime, relativeTime } from '@/utils/format'
import { type ProviderOption, type MCPServerParams, type AvailableSkillState, type SessionSkillState, type CreateProviderParams, type ProviderModelDescriptor } from '@/api/protocol'

interface SidebarProps {
  collapsed?: boolean
}

export default function Sidebar({ collapsed }: SidebarProps) {
  const gatewayAPI = useGatewayAPI()
  const runtime = useRuntime()
  const projects = useSessionStore((s) => s.projects)
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const switchSession = useSessionStore((s) => s.switchSession)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const theme = useUIStore((s) => s.theme)
  const setTheme = useUIStore((s) => s.setTheme)
  const searchQuery = useUIStore((s) => s.searchQuery)
  const setSearchQuery = useUIStore((s) => s.setSearchQuery)
  const connectionState = useGatewayStore((s) => s.connectionState)
  const setCurrentProjectId = useSessionStore((s) => s.setCurrentProjectId)

  const workspaces = useWorkspaceStore((s) => s.workspaces)
  const currentWorkspaceHash = useWorkspaceStore((s) => s.currentWorkspaceHash)
  const switchWorkspace = useWorkspaceStore((s) => s.switchWorkspace)
  const renameWorkspace = useWorkspaceStore((s) => s.renameWorkspace)
  const deleteWorkspace = useWorkspaceStore((s) => s.deleteWorkspace)
  const createWorkspace = useWorkspaceStore((s) => s.createWorkspace)

  const [expandedWorkspaces, setExpandedWorkspaces] = useState<Set<string>>(new Set())
  const [renamingWorkspaceHash, setRenamingWorkspaceHash] = useState<string | null>(null)
  const [workspaceRenameValue, setWorkspaceRenameValue] = useState('')
  const [createWorkspaceOpen, setCreateWorkspaceOpen] = useState(false)
  const [mcpModalOpen, setMcpModalOpen] = useState(false)
  const [skillModalOpen, setSkillModalOpen] = useState(false)
  const [providerModalOpen, setProviderModalOpen] = useState(false)

  useEffect(() => {
    if (currentWorkspaceHash) {
      setExpandedWorkspaces((prev) => {
        if (prev.has(currentWorkspaceHash)) return prev
        const next = new Set(prev)
        next.add(currentWorkspaceHash)
        return next
      })
    }
  }, [currentWorkspaceHash])

  if (!gatewayAPI) return null

  const toggleWorkspace = (hash: string) => {
    setExpandedWorkspaces((prev) => {
      const next = new Set(prev)
      if (next.has(hash)) next.delete(hash)
      else next.add(hash)
      return next
    })
  }

  const currentSessions = projects.flatMap((p) => p.sessions)
  const trimmedQuery = searchQuery.trim().toLowerCase()
  const filteredWorkspaces = trimmedQuery
    ? workspaces.filter((w) => {
        const nameMatch = (w.name || w.path).toLowerCase().includes(trimmedQuery)
        if (nameMatch) return true
        if (w.hash === currentWorkspaceHash) {
          return currentSessions.some((s) => s.title.toLowerCase().includes(trimmedQuery))
        }
        return false
      })
    : workspaces
  const filteredCurrentSessions = trimmedQuery
    ? currentSessions.filter((s) => s.title.toLowerCase().includes(trimmedQuery))
    : currentSessions

  async function handleSelectSession(sessionId: string) {
    setCurrentProjectId('')
    if (!gatewayAPI) return
    try {
      await switchSession(sessionId, gatewayAPI)
    } catch (err) {
      console.error('Switch session failed:', err)
    }
  }

  async function handleSelectWorkspace(hash: string) {
    if (!gatewayAPI) return
    if (hash !== currentWorkspaceHash) {
      await switchWorkspace(hash, gatewayAPI)
    }
    setExpandedWorkspaces((prev) => {
      const next = new Set(prev)
      next.add(hash)
      return next
    })
  }

  async function handleCommitWorkspaceRename(hash: string) {
    const trimmed = workspaceRenameValue.trim()
    setRenamingWorkspaceHash(null)
    if (!gatewayAPI || !trimmed) return
    const target = workspaces.find((w) => w.hash === hash)
    if (target && trimmed === (target.name || target.path)) return
    await renameWorkspace(hash, trimmed, gatewayAPI)
  }

  async function handleDeleteWorkspace(hash: string) {
    const target = workspaces.find((w) => w.hash === hash)
    const label = target?.name || target?.path || hash
    if (!window.confirm(`Delete workspace "${label}"? Sessions in this workspace will become inaccessible.`)) return
    if (!gatewayAPI) return
    await deleteWorkspace(hash, gatewayAPI)
  }

  async function handleCreateWorkspace(path: string, name?: string) {
    if (!gatewayAPI || !path.trim()) return
    await createWorkspace(path.trim(), gatewayAPI, name?.trim() || undefined)
    setCreateWorkspaceOpen(false)
  }

  function handleNewSession() {
    const store = useSessionStore.getState()
    store.prepareNewChat()
  }

  const modalOverlays = typeof document === 'undefined'
    ? null
    : createPortal(
        <>
          {mcpModalOpen && <McpModal onClose={() => setMcpModalOpen(false)} />}
          {skillModalOpen && <SkillModal onClose={() => setSkillModalOpen(false)} />}
          {providerModalOpen && <ProviderModal onClose={() => setProviderModalOpen(false)} />}
        </>,
        document.body,
      )

  // Collapsed sidebar strip
  if (collapsed) {
    return (
      <>
        <button className="sidebar-strip-btn" onClick={toggleSidebar} title="展开侧边栏">
          <PanelLeft size={16} />
        </button>
        <button className="sidebar-strip-btn" onClick={handleNewSession} title="新对话">
          <Plus size={16} />
        </button>
        <div style={{ flex: 1 }} />
        <button className="sidebar-strip-btn" onClick={() => setMcpModalOpen(true)} title="MCP 配置">
          <Blocks size={16} />
        </button>
        <button className="sidebar-strip-btn" onClick={() => setSkillModalOpen(true)} title="Skill 管理">
          <Cpu size={16} />
        </button>
        <button className="sidebar-strip-btn" onClick={() => setProviderModalOpen(true)} title="供应商">
          <Server size={16} />
        </button>
        {modalOverlays}
      </>
    )
  }

  return (
    <>
      {/* Header */}
      <div className="sidebar-header">
        <span className="sidebar-brand">NeoCode</span>
        <span className={`status-dot ${connectionState === 'connected' ? 'connected' : connectionState === 'connecting' ? 'connecting' : 'error'}`}
          style={{ flexShrink: 0, marginRight: -4 }}
          title={connectionState === 'connected' ? '已连接' : connectionState === 'connecting' ? '连接中...' : '连接失败'}
        />
        <button className="sidebar-toggle-btn" onClick={toggleSidebar} title="收起侧边栏">
          <PanelLeft size={14} />
        </button>
      </div>

      {/* New Chat CTA */}
      <button className="sidebar-new-chat-btn" onClick={handleNewSession}>
        <Plus size={16} />
        <span>新对话</span>
      </button>

      <div className="sidebar-divider" />

      {/* Section header: label + add workspace */}
      <div className="sidebar-section-header">
        <span className="sidebar-section-label">工作区</span>
        <button className="btn btn-ghost" style={{ width: 24, height: 24, padding: 0 }} title="新建工作区" onClick={() => setCreateWorkspaceOpen(true)}>
          <FolderPlus size={14} />
        </button>
      </div>

      <div style={{ padding: '0 12px 8px' }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            padding: '0 10px',
            height: 32,
            borderRadius: 'var(--radius-md)',
            background: 'var(--bg-surface)',
            color: 'var(--text-tertiary)',
          }}
        >
          <Search size={13} />
          <input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索工作区或会话"
            style={{
              flex: 1,
              minWidth: 0,
              border: 'none',
              outline: 'none',
              background: 'transparent',
              color: 'var(--text-primary)',
              fontSize: 12,
              fontFamily: 'var(--font-ui)',
            }}
          />
        </div>
      </div>

      {/* Workspace List */}
      <div className="sidebar-section">
        {filteredWorkspaces.length === 0 && (
          <div className="empty-state" style={{ padding: '20px 12px', fontSize: 12 }}>
            暂无工作区，点击右上角 + 创建
          </div>
        )}
        {filteredWorkspaces.map((ws) => {
          const expanded = expandedWorkspaces.has(ws.hash)
          const isCurrent = ws.hash === currentWorkspaceHash
          const rowExpanded = isCurrent && expanded
          const sessionsForThisWorkspace = isCurrent ? filteredCurrentSessions : []
          const isRenaming = renamingWorkspaceHash === ws.hash
          return (
            <div key={ws.hash} style={{ marginBottom: 2 }}>
              <WorkspaceRow
                workspace={ws}
                expanded={rowExpanded}
                isCurrent={isCurrent}
                isRenaming={isRenaming}
                renameValue={workspaceRenameValue}
                onRenameValueChange={setWorkspaceRenameValue}
                onCommitRename={() => handleCommitWorkspaceRename(ws.hash)}
                onCancelRename={() => setRenamingWorkspaceHash(null)}
                onClick={() => {
                  if (isCurrent) {
                    toggleWorkspace(ws.hash)
                  } else {
                    handleSelectWorkspace(ws.hash)
                  }
                }}
                onStartRename={() => {
                  setRenamingWorkspaceHash(ws.hash)
                  setWorkspaceRenameValue(ws.name || ws.path)
                }}
                onDelete={() => handleDeleteWorkspace(ws.hash)}
              />
              {rowExpanded && (
                <div className="session-list">
                  {sessionsForThisWorkspace.length === 0 && (
                    <div style={{ padding: '6px 12px', fontSize: 11, color: 'var(--text-tertiary)', fontStyle: 'italic' }}>暂无会话</div>
                  )}
                  {sessionsForThisWorkspace.map((session) => (
                    <SessionItem
                      key={session.id}
                      session={session}
                      isActive={currentSessionId === session.id}
                      onClick={() => handleSelectSession(session.id)}
                      gatewayAPI={gatewayAPI}
                    />
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* Bottom Actions */}
      <div className="sidebar-divider" />
      <div className="sidebar-footer">
        <button className="sidebar-footer-btn" onClick={() => setMcpModalOpen(true)} title="MCP 配置"><Blocks size={15} /><span>MCP</span></button>
        <button className="sidebar-footer-btn" onClick={() => setSkillModalOpen(true)} title="Skill 管理"><Cpu size={15} /><span>Skill</span></button>
        <button className="sidebar-footer-btn" onClick={() => setProviderModalOpen(true)} title="供应商设置"><Server size={15} /><span>供应商</span></button>
        <button className="sidebar-footer-btn" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')} title={theme === 'dark' ? '浅色模式' : '深色模式'}>
          {theme === 'dark' ? <Sun size={15} /> : <Moon size={15} />}
        </button>
      </div>

      {mcpModalOpen && <McpModal onClose={() => setMcpModalOpen(false)} />}
      {skillModalOpen && <SkillModal onClose={() => setSkillModalOpen(false)} />}
      {providerModalOpen && <ProviderModal onClose={() => setProviderModalOpen(false)} />}
      {createWorkspaceOpen && (
        <CreateWorkspaceDialog
          electronMode={runtime.mode === 'electron'}
          onPickDirectory={runtime.mode === 'electron' ? runtime.selectWorkdir : undefined}
          onSubmit={handleCreateWorkspace}
          onClose={() => setCreateWorkspaceOpen(false)}
        />
      )}
    </>
  )
}

// ── Session Item ──

function SessionItem({
  session, isActive, onClick, gatewayAPI,
}: {
  session: { id: string; title: string; time: string; createdAt?: string; updatedAt?: string }
  isActive: boolean
  onClick: () => void
  gatewayAPI: ReturnType<typeof useGatewayAPI>
}) {
  const [hover, setHover] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [renameVal, setRenameVal] = useState(session.title)
  const [deleting, setDeleting] = useState(false)
  const updatedLabel = session.updatedAt ? formatSessionTime(session.updatedAt) : ''
  const createdLabel = session.createdAt ? formatSessionTime(session.createdAt) : ''
  const timeTitle = updatedLabel && createdLabel
    ? `更新: ${updatedLabel}\n创建: ${createdLabel}`
    : updatedLabel || createdLabel || ''

  async function commitRename() {
    const trimmed = renameVal.trim()
    setRenaming(false)
    if (!gatewayAPI || !trimmed || trimmed === session.title) return
    try {
      await gatewayAPI.renameSession(session.id, trimmed)
      await useSessionStore.getState().fetchSessions(gatewayAPI)
    } catch (err) { console.error('Rename session failed:', err) }
  }

  async function handleDelete() {
    setDeleting(false)
    if (!gatewayAPI) return
    try {
      useSessionStore.getState().removeSessionLocally(session.id)
      if (useSessionStore.getState().currentSessionId === session.id) {
        useSessionStore.getState().prepareNewChat()
      }
      await gatewayAPI.deleteSession(session.id)
      useSessionStore.getState().fetchSessions(gatewayAPI, true).catch(() => {})
    } catch (err) { console.error('Delete session failed:', err) }
  }

  return (
    <div
      className="session-item-wrapper"
      data-active={isActive ? '' : undefined}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      {renaming ? (
        <input
          className="workspace-rename-input"
          style={{ margin: '4px 8px', flex: 1, fontFamily: 'var(--font-ui)' }}
          value={renameVal}
          onChange={(e) => setRenameVal(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') commitRename(); else if (e.key === 'Escape') setRenaming(false) }}
          onBlur={commitRename}
          autoFocus
        />
      ) : (
        <button
          className={`session-item ${isActive ? 'active' : ''}`}
          onClick={onClick}
          style={{ flex: 1 }}
        >
          <MessageSquare size={14} />
          <span className="sess-title" title={session.title}>{session.title}</span>
          <span className="sess-time" title={timeTitle}>{relativeTime(session.time)}</span>
        </button>
      )}
      {!renaming && hover && (
        <div className="session-item-actions">
          <button
            className="workspace-action-btn"
            title="重命名"
            onClick={(e) => { e.stopPropagation(); setRenaming(true); setRenameVal(session.title) }}
          >
            <Pencil size={12} />
          </button>
          <button
            className="workspace-action-btn"
            style={{ color: 'var(--error)' }}
            title="删除"
            onClick={(e) => { e.stopPropagation(); setDeleting(true) }}
          >
            <Trash2 size={12} />
          </button>
        </div>
      )}
      {deleting && (
        <ConfirmDialog
          title="删除会话"
          description={`确定要删除「${session.title}」吗？此操作不可撤销。`}
          variant="danger"
          confirmLabel="删除"
          onConfirm={handleDelete}
          onCancel={() => setDeleting(false)}
        />
      )}
    </div>
  )
}

// ── Workspace Row ──

function WorkspaceRow({
  workspace, expanded, isCurrent, isRenaming, renameValue,
  onRenameValueChange, onCommitRename, onCancelRename,
  onClick, onStartRename, onDelete,
}: {
  workspace: Workspace
  expanded: boolean; isCurrent: boolean; isRenaming: boolean
  renameValue: string
  onRenameValueChange: (v: string) => void
  onCommitRename: () => void
  onCancelRename: () => void
  onClick: () => void
  onStartRename: () => void
  onDelete: () => void
}) {
  const [hover, setHover] = useState(false)
  const display = workspace.name || workspace.path
  return (
    <div
      className={`workspace-header ${isCurrent ? 'active' : ''}`}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <button className="workspace-header-main" onClick={onClick} title={workspace.path}>
        <span className={`chevron ${expanded ? 'expanded' : ''}`}>
          <ChevronRight size={14} />
        </span>
        <Folder size={14} />
        {isRenaming ? (
          <input
            className="workspace-rename-input"
            value={renameValue}
            onChange={(e) => onRenameValueChange(e.target.value)}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') { e.preventDefault(); onCommitRename() }
              else if (e.key === 'Escape') { e.preventDefault(); onCancelRename() }
            }}
            onBlur={onCommitRename}
            autoFocus
          />
        ) : (
          <span className="ws-name">{display}</span>
        )}
      </button>
      {!isRenaming && hover && (
        <div className="workspace-header-actions">
          <button className="workspace-action-btn" title="重命名工作区" onClick={(e) => { e.stopPropagation(); onStartRename() }}>
            <Pencil size={12} />
          </button>
          <button className="workspace-action-btn" style={{ color: 'var(--error)' }} title="删除工作区" onClick={(e) => { e.stopPropagation(); onDelete() }}>
            <Trash2 size={12} />
          </button>
        </div>
      )}
    </div>
  )
}

// ── Create Workspace Dialog ──

function CreateWorkspaceDialog({
  electronMode, onPickDirectory, onSubmit, onClose,
}: {
  electronMode: boolean
  onPickDirectory?: () => Promise<string>
  onSubmit: (path: string, name?: string) => Promise<void>
  onClose: () => void
}) {
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function handlePick() {
    if (!onPickDirectory) return
    try {
      const picked = await onPickDirectory()
      if (picked) setPath(picked)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to pick directory')
    }
  }

  async function handleSubmit() {
    if (!path.trim()) { setError('Workspace path is required'); return }
    setLoading(true)
    setError('')
    try {
      await onSubmit(path.trim(), name.trim() || undefined)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create workspace')
      setLoading(false)
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" style={{ width: 420 }} onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h3 className="modal-title">新建工作区</h3>
          <button className="modal-close-btn" onClick={onClose}><X size={16} /></button>
        </div>
        <div className="modal-body">
          {error && <div style={{ color: 'var(--error)', fontSize: 12, marginBottom: 8 }}>{error}</div>}
          <label className="form-label" style={{ marginBottom: 12 }}>
            工作目录路径
            <div style={{ display: 'flex', gap: 6 }}>
              <input className="form-input" style={{ flex: 1, fontFamily: 'var(--font-ui)' }}
                value={path} onChange={(e) => setPath(e.target.value)}
                placeholder="例如：/Users/me/projects/foo" autoFocus
              />
              {electronMode && onPickDirectory && (
                <button className="btn btn-secondary" onClick={handlePick}>浏览</button>
              )}
            </div>
          </label>
          <label className="form-label" style={{ marginBottom: 12 }}>
            显示名称（可选）
            <input className="form-input" style={{ fontFamily: 'var(--font-ui)' }}
              value={name} onChange={(e) => setName(e.target.value)}
              placeholder="留空则使用路径"
            />
          </label>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn btn-primary" style={{ flex: 1, opacity: loading ? 0.6 : 1 }} onClick={handleSubmit} disabled={loading}>
              {loading ? '创建中...' : '创建'}
            </button>
            <button className="btn btn-secondary" style={{ flex: 1 }} onClick={onClose} disabled={loading}>取消</button>
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Toggle Switch ──

function ToggleSwitch({ checked, onChange, disabled }: { checked: boolean; onChange: () => void; disabled?: boolean }) {
  return (
    <label className="toggle-switch" style={{ opacity: disabled ? 0.4 : 1, cursor: disabled ? 'not-allowed' : 'pointer' }}>
      <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled} />
      <span className="toggle-slider" />
    </label>
  )
}

// ── MCP Modal ──

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
      setError(err instanceof Error ? err.message : 'Failed to load MCP configuration')
      console.error('listMCPServers failed:', err)
    } finally { setLoading(false) }
  }, [gatewayAPI])

  useEffect(() => { load() }, [load])

  if (!gatewayAPI) return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">MCP 配置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body"><div className="empty-state">Gateway 未连接，请检查连接状态</div></div>
      </div>
    </div>
  )

  async function handleToggle(server: MCPServerParams) {
    if (!gatewayAPI) return
    try { await gatewayAPI.setMCPServerEnabled(server.id, !server.enabled); await load() }
    catch (err) { console.error('setMCPServerEnabled failed:', err); setError(err instanceof Error ? err.message : 'Operation failed') }
  }

  function handleEdit(server: MCPServerParams) {
    setEditing({ ...server, stdio: server.stdio ?? { command: '', args: [], workdir: '' } })
    setIsNew(false)
  }

  function handleAdd() { setEditing(emptyServer()); setIsNew(true) }

  async function handleDelete(serverId: string) {
    if (!gatewayAPI) return
    if (!window.confirm(`Delete MCP Server "${serverId}"?`)) return
    try { await gatewayAPI.deleteMCPServer(serverId); await load() }
    catch (err) { console.error('deleteMCPServer failed:', err); setError(err instanceof Error ? err.message : 'Delete failed') }
  }

  async function handleSave() {
    if (!editing || !gatewayAPI) return
    if (!editing.id.trim()) { setError('Server ID is required'); return }
    if (editing.enabled && !editing.stdio?.command?.trim()) { setError('An enabled MCP Server must specify a Command'); return }
    try { await gatewayAPI.upsertMCPServer({ server: editing }); setEditing(null); await load() }
    catch (err) { console.error('upsertMCPServer failed:', err); setError(err instanceof Error ? err.message : 'Save failed') }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">MCP 配置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body">
          {loading && <div className="empty-state">加载中...</div>}
          {!loading && error && !editing && <div className="empty-state" style={{ color: 'var(--error)' }}>{error}</div>}
          {!loading && !error && !editing && servers.length === 0 && <div className="empty-state">暂无已配置的 MCP Server</div>}
          {!editing && servers.map((server) => (
            <div key={server.id} className="config-card">
              <div className="config-card-icon"><Blocks size={16} /></div>
              <div className="config-card-info">
                <div className="config-card-name">{server.id}</div>
                {(server.source || server.version) && (
                  <div className="config-card-meta">{server.source || server.version}</div>
                )}
              </div>
              <ToggleSwitch checked={!!server.enabled} onChange={() => handleToggle(server)} />
              <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 11 }} onClick={() => handleEdit(server)}>编辑</button>
              <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 11, color: 'var(--error)' }} onClick={() => handleDelete(server.id)}>删除</button>
            </div>
          ))}
          {!editing && (
            <div style={{ marginTop: 8 }}>
              <button className="btn btn-secondary" style={{ width: '100%' }} onClick={handleAdd}>+ 新增 MCP Server</button>
            </div>
          )}
          {editing && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>
              {error && <div style={{ color: 'var(--error)', fontSize: 12 }}>{error}</div>}
              <label className="form-label">
                ID
                <input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={editing.id}
                  disabled={!isNew} onChange={(e) => setEditing({ ...editing, id: e.target.value })} />
              </label>
              <label className="form-label">
                Command
                <input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={editing.stdio?.command || ''}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, command: e.target.value } })} />
              </label>
              <label className="form-label">
                Args（以空格分隔）
                <input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={(editing.stdio?.args || []).join(' ')}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, args: e.target.value.split(' ').filter(Boolean) } })} />
              </label>
              <label className="form-label">
                Workdir
                <input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={editing.stdio?.workdir || ''}
                  onChange={(e) => setEditing({ ...editing, stdio: { ...editing.stdio, workdir: e.target.value } })} />
              </label>
              <div className="form-label">
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <span>环境变量</span>
                  <button className="btn btn-secondary" style={{ fontSize: 11, padding: '2px 6px' }} onClick={() => {
                    const env = [...(editing.env || []), { name: '', value: '' }]; setEditing({ ...editing, env }) }}>+ 添加</button>
                </div>
                {(editing.env || []).map((ev, idx) => (
                  <div key={idx} style={{ display: 'flex', gap: 4, marginTop: 2 }}>
                    <input className="form-input" style={{ flex: 1, fontFamily: 'var(--font-ui)' }} placeholder="NAME"
                      value={ev.name} onChange={(e) => {
                        const env = [...(editing.env || [])]; env[idx] = { ...env[idx], name: e.target.value }; setEditing({ ...editing, env }) }} />
                    <input className="form-input" style={{ flex: 1, fontFamily: 'var(--font-ui)' }} placeholder="VALUE"
                      value={ev.value || ''} onChange={(e) => {
                        const env = [...(editing.env || [])]; env[idx] = { ...env[idx], value: e.target.value }; setEditing({ ...editing, env }) }} />
                    <button className="btn btn-ghost" style={{ color: 'var(--error)', padding: '2px 4px', fontSize: 11 }} onClick={() => {
                      const env = (editing.env || []).filter((_, i) => i !== idx); setEditing({ ...editing, env }) }}>X</button>
                  </div>
                ))}
              </div>
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <button className="btn btn-primary" style={{ flex: 1 }} onClick={handleSave}>保存</button>
                <button className="btn btn-secondary" style={{ flex: 1 }} onClick={() => { setEditing(null); setError('') }}>取消</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Skill Modal ──

function SkillModal({ onClose }: { onClose: () => void }) {
  const gatewayAPI = useGatewayAPI()
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const isGenerating = useChatStore((s) => s.isGenerating)
  const [availableSkills, setAvailableSkills] = useState<AvailableSkillState[]>([])
  const [sessionSkills, setSessionSkills] = useState<SessionSkillState[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!gatewayAPI) return
    let cancelled = false
    setLoading(true); setError('')
    Promise.all([
      gatewayAPI.listAvailableSkills().catch(() => ({ payload: { skills: [] } })),
      currentSessionId ? gatewayAPI.listSessionSkills(currentSessionId).catch(() => ({ payload: { skills: [] } })) : Promise.resolve({ payload: { skills: [] } }),
    ]).then(([availResult, sessResult]) => {
      if (!cancelled) {
        setAvailableSkills((availResult.payload.skills as AvailableSkillState[]) || [])
        setSessionSkills((sessResult.payload.skills as SessionSkillState[]) || [])
      }
    }).catch((err) => {
      if (!cancelled) { setError(err instanceof Error ? err.message : 'Failed to load skills'); console.error('listSkills failed:', err) }
    }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [gatewayAPI, currentSessionId])

  if (!gatewayAPI) return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">Skill 配置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body"><div className="empty-state">Gateway 未连接，请检查连接状态</div></div>
      </div>
    </div>
  )

  const sessionSkillIds = new Set(sessionSkills.map((s) => s.skill_id))

  async function handleToggleSkill(skillId: string, enabled: boolean) {
    if (isGenerating) { setError('Cannot toggle skill while generating'); return }
    if (!currentSessionId) { setError('Select a session before managing skills'); return }
    if (!gatewayAPI) { setError('Gateway not connected'); return }
    try {
      if (enabled) await gatewayAPI.deactivateSessionSkill(currentSessionId, skillId)
      else await gatewayAPI.activateSessionSkill(currentSessionId, skillId)
      const [availResult, sessResult] = await Promise.all([
        gatewayAPI.listAvailableSkills().catch(() => ({ payload: { skills: [] } })),
        gatewayAPI.listSessionSkills(currentSessionId).catch(() => ({ payload: { skills: [] } })),
      ])
      setAvailableSkills((availResult.payload.skills as AvailableSkillState[]) || [])
      setSessionSkills((sessResult.payload.skills as SessionSkillState[]) || [])
    } catch (err) { console.error('toggleSkill failed:', err); setError(err instanceof Error ? err.message : 'Operation failed') }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">Skill 配置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body">
          {loading && <div className="empty-state">加载中...</div>}
          {!loading && error && <div className="empty-state" style={{ color: 'var(--error)' }}>{error}</div>}
          {!loading && !error && availableSkills.length === 0 && <div className="empty-state">暂无可用 Skill</div>}
          {!loading && availableSkills.map((skill) => {
            const skillId = skill.descriptor.id
            const enabled = skill.active || sessionSkillIds.has(skillId)
            return (
              <div key={skillId} className="config-card">
                <div className="config-card-icon"><Cpu size={16} /></div>
                <div className="config-card-info">
                  <div className="config-card-name">{skill.descriptor.name || skillId}</div>
                  {skill.descriptor.description && (
                    <div className="config-card-meta">{skill.descriptor.description}</div>
                  )}
                </div>
                <ToggleSwitch
                  checked={enabled}
                  onChange={() => handleToggleSkill(skillId, enabled)}
                  disabled={!currentSessionId || isGenerating}
                />
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// ── Provider Modal ──

function emptyProviderForm(): CreateProviderParams & { modelsJSON?: string } {
  return {
    name: '', driver: 'openaicompat', model_source: 'discover',
    chat_api_mode: 'chat_completions', base_url: '',
    chat_endpoint_path: '/chat/completions', discovery_endpoint_path: '/models',
    api_key_env: '', api_key: '', modelsJSON: '',
  }
}

function ProviderModal({ onClose }: { onClose: () => void }) {
  const gatewayAPI = useGatewayAPI()
  const isGenerating = useChatStore((s) => s.isGenerating)
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const providerChangeTick = useGatewayStore((s) => s.providerChangeTick)
  const [providers, setProviders] = useState<ProviderOption[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [formData, setFormData] = useState<CreateProviderParams & { modelsJSON?: string }>(emptyProviderForm)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true); setError('')
    try {
      const result = await gatewayAPI.listProviders()
      const listedProviders = result?.payload?.providers ?? []
      if (!currentSessionId) {
        setProviders(listedProviders)
        return
      }
      const sessionModel = await gatewayAPI.getSessionModel(currentSessionId)
      const effectiveProviderID = sessionModel?.payload?.provider || ''
      setProviders(listedProviders.map((provider) => ({
        ...provider,
        selected: provider.id === effectiveProviderID,
      })))
    }
    catch (err) { setError(err instanceof Error ? err.message : 'Failed to load providers'); console.error('listProviders failed:', err) }
    finally { setLoading(false) }
  }, [currentSessionId, gatewayAPI])

  useEffect(() => { load() }, [load, providerChangeTick])

  async function handleSelect(providerId: string) {
    if (!gatewayAPI) return
    if (isGenerating) { useUIStore.getState().showToast('Cannot switch provider while generating', 'info'); return }
    setError('')
    try {
      await gatewayAPI.selectProviderModel({ provider_id: providerId })
      useGatewayStore.getState().notifyProviderChanged()
      await load()
    }
    catch (err) { console.error('selectProviderModel failed:', err); setError(err instanceof Error ? err.message : 'Failed to switch provider') }
  }

  async function handleDelete(providerId: string, source: string) {
    if (!gatewayAPI) return
    if (source !== 'custom') return
    if (!window.confirm(`Delete provider "${providerId}"?`)) return
    try { await gatewayAPI.deleteCustomProvider(providerId); useGatewayStore.getState().notifyProviderChanged(); await load() }
    catch (err) { console.error('deleteCustomProvider failed:', err); setError(err instanceof Error ? err.message : 'Delete failed') }
  }

  function handleAdd() { setFormData(emptyProviderForm()); setFormError(''); setShowForm(true) }

  function handleDriverChange(driver: string) {
    setFormData(prev => {
      const next = { ...prev, driver }
      if (driver === 'openaicompat') { next.chat_endpoint_path = '/chat/completions'; next.base_url = ''; next.chat_api_mode = 'chat_completions' }
      else if (driver === 'gemini') { next.base_url = 'https://generativelanguage.googleapis.com/v1beta'; next.chat_endpoint_path = '/models'; next.chat_api_mode = '' }
      else if (driver === 'anthropic') { next.base_url = 'https://api.anthropic.com/v1'; next.chat_endpoint_path = '/messages'; next.chat_api_mode = '' }
      return next
    })
  }

  function handleChatModeChange(mode: string) {
    setFormData(prev => {
      const next = { ...prev, chat_api_mode: mode }
      next.chat_endpoint_path = mode === 'responses' ? '/responses' : '/chat/completions'
      return next
    })
  }

  function validateForm(): string {
    const name = formData.name.trim()
    const apiKey = (formData.api_key || '').trim()
    const apiKeyEnv = formData.api_key_env.trim()
    if (!name) return 'Name is required'
    if (!formData.driver.trim()) return 'Driver is required'
    if (!(formData.model_source || 'discover').trim()) return 'Model source is required'
    if (!apiKey) return 'API Key is required'
    if (!apiKeyEnv) return 'API Key environment variable is required'
    if (!/^[A-Z][A-Z0-9_]*$/.test(apiKeyEnv)) return 'Invalid API Key environment variable name'
    if ((formData.model_source || 'discover') === 'manual') {
      const json = (formData.modelsJSON || '').trim()
      if (!json) return 'Model JSON is required in manual mode'
      try {
        const parsed = JSON.parse(json)
        if (!Array.isArray(parsed)) return 'Model JSON must be an array'
        for (const m of parsed) { if (!m.id || !m.name) return 'Each model must include id and name fields' }
      } catch { return 'Invalid Model JSON format' }
    }
    return ''
  }

  async function handleSave() {
    if (!gatewayAPI) return
    const err = validateForm()
    if (err) { setFormError(err); return }

    const { modelsJSON: _, ...payload }: CreateProviderParams & { modelsJSON?: string } = { ...formData }
    if (!payload.base_url?.trim()) {
      if (payload.driver === 'openaicompat') payload.base_url = 'https://api.openai.com/v1'
      else if (payload.driver === 'gemini') payload.base_url = 'https://generativelanguage.googleapis.com/v1beta'
      else if (payload.driver === 'anthropic') payload.base_url = 'https://api.anthropic.com/v1'
    }
    if (!payload.chat_endpoint_path?.trim()) {
      payload.chat_endpoint_path = formData.driver === 'gemini' ? '/models' : formData.driver === 'anthropic' ? '/messages' : '/chat/completions'
    }
    if (!payload.discovery_endpoint_path?.trim() && payload.model_source !== 'manual') { payload.discovery_endpoint_path = '/models' }

    if (payload.model_source === 'manual' && formData.modelsJSON) {
      try { payload.models = JSON.parse(formData.modelsJSON) as ProviderModelDescriptor[] }
      catch { setFormError('Failed to parse Model JSON'); return }
    }

    setSaving(true); setFormError('')
    try { await gatewayAPI.createCustomProvider(payload); setShowForm(false); await load(); useGatewayStore.getState().notifyProviderChanged() }
    catch (err) { console.error('createCustomProvider failed:', err); setFormError(err instanceof Error ? err.message : 'Create failed') }
    finally { setSaving(false) }
  }

  if (!gatewayAPI) return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">供应商设置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body"><div className="empty-state">Gateway 未连接，请检查连接状态</div></div>
      </div>
    </div>
  )

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header"><h3 className="modal-title">供应商设置</h3><button className="modal-close-btn" onClick={onClose}><X size={16} /></button></div>
        <div className="modal-body">
          {loading && <div className="empty-state">加载中...</div>}
          {!loading && error && !showForm && <div className="empty-state" style={{ color: 'var(--error)' }}>{error}</div>}
          {!loading && !showForm && providers.length === 0 && <div className="empty-state">暂无已配置的供应商</div>}
          {!showForm && providers.map((p) => (
            <div key={p.id} className={`config-card ${p.selected ? 'selected' : ''}`}>
              <div className="config-card-icon"><Server size={16} /></div>
              <div className="config-card-info">
                <div className="config-card-name">{p.name}</div>
                <div className="config-card-models">
                  {p.models?.map((m) => (
                    <span key={m.id} className="config-card-model-tag">{m.name || m.id}</span>
                  ))}
                </div>
              </div>
              <button className="btn btn-primary" style={{ fontSize: 12, padding: '5px 12px' }}
                onClick={() => handleSelect(p.id)} disabled={p.selected || isGenerating}>
                {p.selected ? '当前使用' : '选择'}
              </button>
              {p.source === 'custom' && (
                <button className="btn btn-ghost" style={{ padding: '4px 8px', fontSize: 11, color: 'var(--error)' }}
                  onClick={() => handleDelete(p.id, p.source)}>删除</button>
              )}
            </div>
          ))}
          {!showForm && (
            <div style={{ marginTop: 8 }}>
              <button className="btn btn-secondary" style={{ width: '100%' }} onClick={handleAdd}>+ 新增 Provider</button>
            </div>
          )}
          {showForm && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>
              {formError && <div style={{ color: 'var(--error)', fontSize: 12 }}>{formError}</div>}
              <label className="form-label">名称 *<input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} placeholder="例如：my-openai" /></label>
              <label className="form-label">Driver *
                <select className="form-select" value={formData.driver} onChange={(e) => handleDriverChange(e.target.value)}>
                  <option value="openaicompat">OpenAI Compatible</option><option value="gemini">Gemini</option><option value="anthropic">Anthropic</option>
                </select>
              </label>
              <label className="form-label">模型来源 *
                <select className="form-select" value={formData.model_source || 'discover'} onChange={(e) => setFormData({ ...formData, model_source: e.target.value })}>
                  <option value="discover">自动发现</option><option value="manual">手动配置</option>
                </select>
              </label>
              {formData.driver === 'openaicompat' && (
                <label className="form-label">Chat API 模式 *
                  <select className="form-select" value={formData.chat_api_mode || 'chat_completions'} onChange={(e) => handleChatModeChange(e.target.value)}>
                    <option value="chat_completions">Chat Completions</option><option value="responses">Responses</option>
                  </select>
                </label>
              )}
              <label className="form-label">Base URL<input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.base_url || ''} onChange={(e) => setFormData({ ...formData, base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></label>
              <label className="form-label">Chat Endpoint Path<input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.chat_endpoint_path || ''} onChange={(e) => setFormData({ ...formData, chat_endpoint_path: e.target.value })} /></label>
              {(formData.model_source || 'discover') !== 'manual' && (
                <label className="form-label">发现端点路径<input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.discovery_endpoint_path || ''} onChange={(e) => setFormData({ ...formData, discovery_endpoint_path: e.target.value })} /></label>
              )}
              <label className="form-label">API Key 环境变量 *<input className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.api_key_env} onChange={(e) => setFormData({ ...formData, api_key_env: e.target.value })} placeholder="OPENAI_API_KEY" /></label>
              <label className="form-label">API Key *<input type="password" className="form-input" style={{ fontFamily: 'var(--font-ui)' }} value={formData.api_key || ''} onChange={(e) => setFormData({ ...formData, api_key: e.target.value })} placeholder="sk-..." /></label>
              {(formData.model_source || 'discover') === 'manual' && (
                <label className="form-label">手动模型 JSON<textarea className="form-textarea" value={formData.modelsJSON || ''} onChange={(e) => setFormData({ ...formData, modelsJSON: e.target.value })} placeholder='[{"id":"gpt-4","name":"GPT-4"}]' /></label>
              )}
              <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
                <button className="btn btn-primary" style={{ flex: 1 }} onClick={handleSave} disabled={saving}>{saving ? '保存中...' : '保存'}</button>
                <button className="btn btn-secondary" style={{ flex: 1 }} onClick={() => { setShowForm(false); setFormError('') }}>取消</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
