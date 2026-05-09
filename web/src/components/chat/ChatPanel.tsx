import { useState, useRef, useEffect } from 'react'
import { useUIStore } from '@/stores/useUIStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { PermissionDecision } from '@/api/protocol'
import MessageList from './MessageList'
import ChatInput from './ChatInput'
import TodoStrip from './TodoStrip'
import {
  FileDiff,
  FolderTree,
  Edit3,
  Shield,
  X,
  Check,
} from 'lucide-react'

export default function ChatPanel() {
  const gatewayAPI = useGatewayAPI()
  const changesPanelOpen = useUIStore((s) => s.changesPanelOpen)
  const fileTreePanelOpen = useUIStore((s) => s.fileTreePanelOpen)
  const toggleChangesPanel = useUIStore((s) => s.toggleChangesPanel)
  const toggleFileTreePanel = useUIStore((s) => s.toggleFileTreePanel)

  const currentSessionId = useSessionStore((s) => s.currentSessionId)
	const projects = useSessionStore((s) => s.projects)

	const permissionRequests = useChatStore((s) => s.permissionRequests)
	const agentMode = useChatStore((s) => s.agentMode)
	const permissionMode = useChatStore((s) => s.permissionMode)
	const currentPermission = permissionRequests[0]

	const [editingTitle, setEditingTitle] = useState(false)
	const [isResolvingPermission, setIsResolvingPermission] = useState(false)
	const titleRef = useRef<HTMLDivElement>(null)
	const autoResolvingPermissionIdsRef = useRef<Set<string>>(new Set())

  const currentSession = projects.flatMap((p) => p.sessions).find((s) => s.id === currentSessionId)
  const title = currentSession?.title || '新对话'

  async function handlePermissionDecision(decision: string) {
    if (!gatewayAPI || !currentPermission || isResolvingPermission) return
    setIsResolvingPermission(true)
    try {
      await gatewayAPI.resolvePermission({ request_id: currentPermission.request_id, decision })
      useUIStore.getState().showToast('Permission request resolved', 'success')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to resolve permission request'
      useUIStore.getState().showToast(message, 'error')
      console.error('Resolve permission failed:', err)
    } finally {
      setIsResolvingPermission(false)
    }
  }

  useEffect(() => {
    const activeRequestIds = new Set(permissionRequests.map((request) => request.request_id))
    for (const requestId of Array.from(autoResolvingPermissionIdsRef.current)) {
      if (!activeRequestIds.has(requestId)) {
        autoResolvingPermissionIdsRef.current.delete(requestId)
      }
    }
  }, [permissionRequests])

  useEffect(() => {
    if (!gatewayAPI || !currentPermission) return
    if (agentMode !== 'build' || permissionMode !== 'bypass') return
    const requestId = currentPermission.request_id
    if (!requestId || autoResolvingPermissionIdsRef.current.has(requestId)) return

    autoResolvingPermissionIdsRef.current.add(requestId)
    setIsResolvingPermission(true)

    gatewayAPI.resolvePermission({
      request_id: requestId,
      decision: PermissionDecision.AllowOnce,
    }).catch((err) => {
      autoResolvingPermissionIdsRef.current.delete(requestId)
      const message = err instanceof Error ? err.message : 'Failed to resolve permission request'
      useUIStore.getState().showToast(message, 'error')
      console.error('Auto-resolve permission failed:', err)
    }).finally(() => {
      setIsResolvingPermission(false)
    })
  }, [agentMode, currentPermission, gatewayAPI, permissionMode])

  useEffect(() => {
    if (editingTitle && titleRef.current) {
      titleRef.current.focus()
      const range = document.createRange()
      range.selectNodeContents(titleRef.current)
      const selection = window.getSelection()
      if (selection) { selection.removeAllRanges(); selection.addRange(range) }
    }
  }, [editingTitle])

  const handleTitleSave = async () => {
    const newTitle = titleRef.current?.innerText.trim()
    if (newTitle && newTitle !== title && currentSessionId && gatewayAPI) {
      try {
        await gatewayAPI.renameSession(currentSessionId, newTitle)
        await useSessionStore.getState().fetchSessions(gatewayAPI)
      } catch (err) { console.error('Rename session failed:', err) }
    }
    setEditingTitle(false)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Header */}
      <div className="chat-header">
        <div className="chat-header-left">
          {editingTitle ? (
            <div
              ref={titleRef}
              contentEditable
              suppressContentEditableWarning
              className="chat-title-editable"
              onBlur={handleTitleSave}
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleTitleSave() } }}
            >
              {title}
            </div>
          ) : (
            <div className="chat-title-row" onClick={() => setEditingTitle(true)}>
              <span className="chat-header-title">{title}</span>
              <span className="edit-hint"><Edit3 size={12} /></span>
            </div>
          )}
        </div>
        <div className="chat-header-right">
          <button
            className={`header-icon-btn ${changesPanelOpen ? 'active' : ''}`}
            title="文件更改"
            onClick={toggleChangesPanel}
          >
            <FileDiff size={16} />
          </button>
          <button
            className={`header-icon-btn ${fileTreePanelOpen ? 'active' : ''}`}
            title="文件目录"
            onClick={toggleFileTreePanel}
          >
            <FolderTree size={16} />
          </button>
        </div>
      </div>

      {/* Messages */}
      <div className="messages-container" data-scroll-root="1">
        <MessageList />
      </div>

      {/* Todo strip */}
      <TodoStrip />

      {/* Input or Permission Request */}
      {currentPermission && !(agentMode === 'build' && permissionMode === 'bypass') ? (
        <div className="permission-area">
          <div className="permission-card">
            <div className="permission-card-header">
              <Shield size={16} style={{ color: 'var(--warning)' }} />
              <span>权限请求</span>
            </div>
            <div className="permission-details">
              <div>
                <div className="permission-detail-label">工具</div>
                <div className="permission-detail-value">{currentPermission.tool_name || currentPermission.tool_category || '-'}</div>
              </div>
              <div>
                <div className="permission-detail-label">操作</div>
                <div className="permission-detail-value">{currentPermission.operation || currentPermission.action_type || '-'}</div>
              </div>
              {currentPermission.target && (
                <div>
                  <div className="permission-detail-label">目标</div>
                  <div className="permission-detail-value" style={{ fontSize: 11 }}>{currentPermission.target}</div>
                </div>
              )}
              {currentPermission.reason && (
                <div>
                  <div className="permission-detail-label">原因</div>
                  <div className="permission-detail-value" style={{ fontSize: 11 }}>{currentPermission.reason}</div>
                </div>
              )}
            </div>
            <div className="permission-actions">
              <button onClick={() => handlePermissionDecision(PermissionDecision.Reject)} disabled={isResolvingPermission}
                className="permission-btn reject" style={{ opacity: isResolvingPermission ? 0.6 : 1 }}>
                <X size={13} /> 拒绝
              </button>
              <button onClick={() => handlePermissionDecision(PermissionDecision.AllowOnce)} disabled={isResolvingPermission}
                className="permission-btn once" style={{ opacity: isResolvingPermission ? 0.6 : 1 }}>
                <Check size={13} /> 允许一次
              </button>
              <button onClick={() => handlePermissionDecision(PermissionDecision.AllowSession)} disabled={isResolvingPermission}
                className="permission-btn session" style={{ opacity: isResolvingPermission ? 0.6 : 1 }}>
                <Check size={13} /> 本会话允许
              </button>
            </div>
          </div>
        </div>
      ) : (
        <ChatInput />
      )}
    </div>
  )
}
