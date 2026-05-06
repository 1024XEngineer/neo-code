import { useState, useEffect, useCallback } from 'react'
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
} from 'lucide-react'
import { type ProviderOption, type MCPServerParams, type AvailableSkillState, type SessionSkillState, type CreateProviderParams, type ProviderModelDescriptor } from '@/api/protocol'

interface SidebarProps {
  collapsed?: boolean
}

/** 左侧栏：会话列表 */
export default function Sidebar({ collapsed }: SidebarProps) {
  const gatewayAPI = useGatewayAPI()
  const runtime = useRuntime()
  // All hooks must be called before any early return (React Rules of Hooks)
  const projects = useSessionStore((s) => s.projects)
  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const switchSession = useSessionStore((s) => s.switchSession)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const searchQuery = useUIStore((s) => s.searchQuery)
  const setSearchQuery = useUIStore((s) => s.setSearchQuery)
  const setCurrentProjectId = useSessionStore((s) => s.setCurrentProjectId)

  const workspaces = useWorkspaceStore((s) => s.workspaces)
  const currentWorkspaceHash = useWorkspaceStore((s) => s.currentWorkspaceHash)
  const switchWorkspace = useWorkspaceStore((s) => s.switchWorkspace)
  const renameWorkspace = useWorkspaceStore((s) => s.renameWorkspace)
  const deleteWorkspace = useWorkspaceStore((s) => s.deleteWorkspace)
  const createWorkspace = useWorkspaceStore((s) => s.createWorkspace)

  const [expandedWorkspaces, setExpandedWorkspaces] = useState<Set<string>>(new Set())
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; sessionId: string } | null>(null)
  const [renamingSessionId, setRenamingSessionId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [renamingWorkspaceHash, setRenamingWorkspaceHash] = useState<string | null>(null)
  const [workspaceRenameValue, setWorkspaceRenameValue] = useState('')
  const [workspaceContextMenu, setWorkspaceContextMenu] = useState<{ x: number; y: number; hash: string } | null>(null)
  const [createWorkspaceOpen, setCreateWorkspaceOpen] = useState(false)
  const [mcpModalOpen, setMcpModalOpen] = useState(false)
  const [skillModalOpen, setSkillModalOpen] = useState(false)
  const [providerModalOpen, setProviderModalOpen] = useState(false)

  // 当前工作区默认展开
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

  // 当前工作区下所有会话（来自时间分组的扁平合并）
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
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexShrink: 0 }}>
          <span style={styles.listTitle}>工作区</span>
          <button style={styles.iconBtn} title="新建工作区" onClick={() => setCreateWorkspaceOpen(true)}>
            <FolderPlus size={14} />
          </button>
        </div>
        <div style={styles.searchBox}>
          <Search size={12} />
          <input
            style={styles.searchInput}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索工作区或会话"
          />
        </div>
      </div>

      {/* Workspace List */}
      <div style={styles.scrollArea}>
        {filteredWorkspaces.length === 0 && (
          <div style={styles.emptyHint}>
            {trimmedQuery ? '无匹配的工作区' : '暂无工作区，点击右上角 + 创建'}
          </div>
        )}
        {filteredWorkspaces.map((ws) => {
          const expanded = expandedWorkspaces.has(ws.hash)
          const isCurrent = ws.hash === currentWorkspaceHash
          const sessionsForThisWorkspace = isCurrent ? filteredCurrentSessions : []
          const isRenaming = renamingWorkspaceHash === ws.hash
          return (
            <div key={ws.hash} style={styles.projectGroup}>
              <WorkspaceRow
                workspace={ws}
                expanded={expanded}
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
                onContextMenu={(e) => {
                  e.preventDefault()
                  setWorkspaceContextMenu({ x: e.clientX, y: e.clientY, hash: ws.hash })
                }}
                onStartRename={() => {
                  setRenamingWorkspaceHash(ws.hash)
                  setWorkspaceRenameValue(ws.name || ws.path)
                }}
                onDelete={() => handleDeleteWorkspace(ws.hash)}
              />
              {expanded && isCurrent && (
                <div style={styles.sessionsList}>
                  {sessionsForThisWorkspace.length === 0 && (
                    <div style={styles.emptySessions}>暂无会话</div>
                  )}
                  {sessionsForThisWorkspace.map((session) => (
                    <SessionItem
                      key={session.id}
                      session={session}
                      isActive={currentSessionId === session.id}
                      onClick={() => handleSelectSession(session.id)}
                      onContextMenu={(e) => {
                        e.preventDefault()
                        setContextMenu({ x: e.clientX, y: e.clientY, sessionId: session.id })
                      }}
                    />
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* Context Menu */}
      {contextMenu && (
        <>
          <div style={styles.overlay} onClick={() => setContextMenu(null)} />
          <div style={{ ...styles.contextMenu, left: contextMenu.x, top: contextMenu.y }}>
            <button style={styles.contextItem} onClick={() => {
              setRenamingSessionId(contextMenu.sessionId)
              const sess = currentSessions.find((s) => s.id === contextMenu.sessionId)
              setRenameValue(sess?.title ?? '')
              setContextMenu(null)
            }}>重命名</button>
            <button style={{ ...styles.contextItem, color: 'var(--error)' }} onClick={async () => {
              const deletedId = contextMenu.sessionId
              setContextMenu(null)
              try {
                useSessionStore.getState().removeSessionLocally(deletedId)
                if (useSessionStore.getState().currentSessionId === deletedId) {
                  useSessionStore.getState().prepareNewChat()
                }
                await gatewayAPI.deleteSession(deletedId)
                useSessionStore.getState().fetchSessions(gatewayAPI, true).catch(() => {})
              } catch (err) {
                console.error('Delete session failed:', err)
                useSessionStore.getState().fetchSessions(gatewayAPI, true).catch(() => {})
              }
            }}>删除</button>
          </div>
        </>
      )}

      {/* Workspace Context Menu */}
      {workspaceContextMenu && (
        <>
          <div style={styles.overlay} onClick={() => setWorkspaceContextMenu(null)} />
          <div style={{ ...styles.contextMenu, left: workspaceContextMenu.x, top: workspaceContextMenu.y }}>
            <button style={styles.contextItem} onClick={() => {
              const ws = workspaces.find((w) => w.hash === workspaceContextMenu.hash)
              if (ws) {
                setRenamingWorkspaceHash(ws.hash)
                setWorkspaceRenameValue(ws.name || ws.path)
              }
              setWorkspaceContextMenu(null)
            }}>重命名</button>
            <button style={{ ...styles.contextItem, color: 'var(--error)' }} onClick={() => {
              const hash = workspaceContextMenu.hash
              setWorkspaceContextMenu(null)
              handleDeleteWorkspace(hash)
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
      {createWorkspaceOpen && (
        <CreateWorkspaceDialog
          electronMode={runtime.mode === 'electron'}
          onPickDirectory={runtime.mode === 'electron' ? runtime.selectWorkdir : undefined}
          onSubmit={handleCreateWorkspace}
          onClose={() => setCreateWorkspaceOpen(false)}
        />
      )}
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

function WorkspaceRow({
  workspace,
  expanded,
  isCurrent,
  isRenaming,
  renameValue,
  onRenameValueChange,
  onCommitRename,
  onCancelRename,
  onClick,
  onContextMenu,
  onStartRename,
  onDelete,
}: {
  workspace: Workspace
  expanded: boolean
  isCurrent: boolean
  isRenaming: boolean
  renameValue: string
  onRenameValueChange: (v: string) => void
  onCommitRename: () => void
  onCancelRename: () => void
  onClick: () => void
  onContextMenu: (e: React.MouseEvent) => void
  onStartRename: () => void
  onDelete: () => void
}) {
  const [hover, setHover] = useState(false)
  const display = workspace.name || workspace.path
  return (
    <div
      style={{
        ...styles.workspaceHeader,
        background: isCurrent ? 'var(--bg-active)' : hover ? 'var(--bg-tertiary)' : 'transparent',
      }}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onContextMenu={onContextMenu}
    >
      <button style={styles.workspaceMain} onClick={onClick} title={workspace.path}>
        <span style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          <ChevronRight size={14} />
        </span>
        <Folder size={14} />
        {isRenaming ? (
          <input
            style={styles.workspaceRenameInput}
            value={renameValue}
            onChange={(e) => onRenameValueChange(e.target.value)}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                onCommitRename()
              } else if (e.key === 'Escape') {
                e.preventDefault()
                onCancelRename()
              }
            }}
            onBlur={onCommitRename}
            autoFocus
          />
        ) : (
          <span style={styles.projectName}>{display}</span>
        )}
      </button>
      {!isRenaming && hover && (
        <div style={styles.workspaceActions}>
          <button
            style={styles.workspaceActionBtn}
            title="重命名工作区"
            onClick={(e) => {
              e.stopPropagation()
              onStartRename()
            }}
          >
            <Pencil size={12} />
          </button>
          <button
            style={{ ...styles.workspaceActionBtn, color: 'var(--error)' }}
            title="删除工作区"
            onClick={(e) => {
              e.stopPropagation()
              onDelete()
            }}
          >
            <Trash2 size={12} />
          </button>
        </div>
      )}
    </div>
  )
}

function CreateWorkspaceDialog({
  electronMode,
  onPickDirectory,
  onSubmit,
  onClose,
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
    if (!path.trim()) {
      setError('Workspace path is required')
      return
    }
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
    <div style={modalStyles.overlay} onClick={onClose}>
      <div style={{ ...modalStyles.modal, width: 420 }} onClick={(e) => e.stopPropagation()}>
        <div style={modalStyles.header}>
          <h3 style={modalStyles.title}>新建工作区</h3>
          <button style={modalStyles.closeBtn} onClick={onClose}>
            <X size={16} />
          </button>
        </div>
        <div style={{ ...modalStyles.body, gap: 12, display: 'flex', flexDirection: 'column' }}>
          {error && <div style={{ color: 'var(--error)', fontSize: 12 }}>{error}</div>}
          <label style={formLabelStyle}>
            工作目录路径
            <div style={{ display: 'flex', gap: 6 }}>
              <input
                style={{ ...modalStyles.input, flex: 1 }}
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="例如：/Users/me/projects/foo"
                autoFocus
              />
              {electronMode && onPickDirectory && (
                <button style={{ ...modalStyles.actionBtn, padding: '0 10px' }} onClick={handlePick}>
                  浏览
                </button>
              )}
            </div>
          </label>
          <label style={formLabelStyle}>
            显示名称（可选）
            <input
              style={modalStyles.input}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="留空则使用路径"
            />
          </label>
          <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
            <button
              style={{ ...modalStyles.actionBtn, flex: 1, opacity: loading ? 0.6 : 1 }}
              onClick={handleSubmit}
              disabled={loading}
            >
              {loading ? '创建中...' : '创建'}
            </button>
            <button style={{ ...modalStyles.actionBtn, flex: 1 }} onClick={onClose} disabled={loading}>
              取消
            </button>
          </div>
        </div>
      </div>
    </div>
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
      const msg = err instanceof Error ? err.message : 'Failed to load MCP configuration'
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
      setError(err instanceof Error ? err.message : 'Operation failed')
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
    if (!window.confirm(`Delete MCP Server "${serverId}"?`)) return
    try {
      await gatewayAPI.deleteMCPServer(serverId)
      await load()
    } catch (err) {
      console.error('deleteMCPServer failed:', err)
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  async function handleSave() {
    if (!editing || !gatewayAPI) return
    if (!editing.id.trim()) {
      setError('Server ID is required')
      return
    }
    if (editing.enabled && !editing.stdio?.command?.trim()) {
      setError('An enabled MCP Server must specify a Command')
      return
    }
    try {
      await gatewayAPI.upsertMCPServer({ server: editing })
      setEditing(null)
      await load()
    } catch (err) {
      console.error('upsertMCPServer failed:', err)
      setError(err instanceof Error ? err.message : 'Save failed')
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
  const isGenerating = useChatStore((s) => s.isGenerating)
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
          const msg = err instanceof Error ? err.message : 'Failed to load skills'
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
    if (isGenerating) {
      setError('Cannot toggle skill while generating; wait for the current run to finish.')
      return
    }
    if (!currentSessionId) {
      setError('Select a session before managing skills')
      return
    }
    if (!gatewayAPI) {
      setError('Gateway not connected')
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
      setError(err instanceof Error ? err.message : 'Operation failed')
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
                    style={{ ...modalStyles.actionBtn, background: enabled ? 'var(--error-bg, rgba(239,68,68,0.1))' : 'rgba(22,163,74,0.15)', color: enabled ? 'var(--error)' : 'var(--success)', opacity: isGenerating ? 0.5 : 1, cursor: isGenerating ? 'not-allowed' : 'pointer' }}
                    onClick={() => !isGenerating && handleToggleSkill(skillId, enabled)}
                    disabled={!currentSessionId || isGenerating}
                    title={isGenerating ? '生成中无法切换技能' : undefined}
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
  const isGenerating = useChatStore((s) => s.isGenerating)
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
      const msg = err instanceof Error ? err.message : 'Failed to load providers'
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
    if (isGenerating) {
      useUIStore.getState().showToast('Cannot switch provider while generating; stop the current run first.', 'info')
      return
    }
    try {
      await gatewayAPI.selectProviderModel({ provider_id: providerId })
      useGatewayStore.getState().notifyProviderChanged()
      await load()
    } catch (err) {
      console.error('selectProviderModel failed:', err)
      setError(err instanceof Error ? err.message : 'Failed to switch provider')
    }
  }

  async function handleDelete(providerId: string, source: string) {
    if (!gatewayAPI) return
    if (source !== 'custom') return
    if (!window.confirm(`Delete provider "${providerId}"?`)) return
    try {
      await gatewayAPI.deleteCustomProvider(providerId)
      useGatewayStore.getState().notifyProviderChanged()
      await load()
    } catch (err) {
      console.error('deleteCustomProvider failed:', err)
      setError(err instanceof Error ? err.message : 'Delete failed')
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

    if (!name) return 'Name is required'
    if (!driver) return 'Driver is required'
    if (!modelSource) return 'Model source is required'
    if (!apiKey) return 'API Key is required'
    if (!apiKeyEnv) return 'API Key environment variable is required'
    if (!/^[A-Z][A-Z0-9_]*$/.test(apiKeyEnv)) {
      return 'Invalid API Key environment variable name (must use uppercase letters, digits, underscores, and start with an uppercase letter)'
    }

    if (modelSource === 'manual') {
      const json = (formData.modelsJSON || '').trim()
      if (!json) return 'Model JSON is required in manual mode'
      try {
        const parsed = JSON.parse(json)
        if (!Array.isArray(parsed)) return 'Model JSON must be an array'
        for (const m of parsed) {
          if (!m.id || !m.name) return 'Each model must include id and name fields'
        }
      } catch {
        return 'Invalid Model JSON format'
      }
    }

    if (modelSource === 'discover' && driver === 'openaicompat') {
      const discoveryPath = (formData.discovery_endpoint_path || '').trim()
      if (!discoveryPath) return 'Discovery endpoint path is required in discover mode'
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
        setFormError('Failed to parse Model JSON')
        return
      }
    }

    setSaving(true)
    setFormError('')
    try {
      await gatewayAPI.createCustomProvider(payload)
      setShowForm(false)
      await load()
      useGatewayStore.getState().notifyProviderChanged()
    } catch (err) {
      console.error('createCustomProvider failed:', err)
      setFormError(err instanceof Error ? err.message : 'Create failed')
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
                  style={{ ...modalStyles.actionBtn, background: p.selected ? 'var(--bg-active)' : 'rgba(22,163,74,0.15)', color: p.selected ? 'var(--text-tertiary)' : 'var(--success)', opacity: isGenerating ? 0.5 : 1, cursor: isGenerating ? 'not-allowed' : 'pointer' }}
                  onClick={() => handleSelect(p.id)}
                  disabled={p.selected || isGenerating}
                  title={isGenerating ? '生成中无法切换供应商' : undefined}
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
    flex: 1,
  },
  workspaceHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    width: '100%',
    padding: '0 4px 0 0',
    borderRadius: 'var(--radius-sm)',
    transition: 'all 0.15s',
    marginBottom: 1,
  },
  workspaceMain: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    flex: 1,
    minWidth: 0,
    padding: '6px 8px',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
  },
  workspaceActions: {
    display: 'flex',
    gap: 2,
    flexShrink: 0,
  },
  workspaceActionBtn: {
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
    transition: 'all 0.15s',
  },
  workspaceRenameInput: {
    flex: 1,
    minWidth: 0,
    padding: '2px 6px',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    outline: 'none',
  },
  emptyHint: {
    padding: '20px 12px',
    fontSize: 12,
    color: 'var(--text-tertiary)',
    textAlign: 'center',
    lineHeight: 1.5,
  },
  emptySessions: {
    padding: '6px 12px',
    fontSize: 11,
    color: 'var(--text-tertiary)',
    fontStyle: 'italic',
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
