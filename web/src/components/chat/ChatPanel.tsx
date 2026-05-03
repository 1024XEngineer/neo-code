import { useState, useRef, useEffect } from 'react'
import { useUIStore } from '@/stores/useUIStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { PermissionDecision, PlanApprovalDecision } from '@/api/protocol'
import MessageList from './MessageList'
import ChatInput from './ChatInput'
import ModelSelector from './ModelSelector'
import {
  PanelRightOpen,
  FileDiff,
  FolderTree,
  MoreHorizontal,
  Edit3,
  Shield,
  X,
  Check,
} from 'lucide-react'

/** 聊天主区域 */
export default function ChatPanel() {
  const gatewayAPI = useGatewayAPI()
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const toggleSidebar = useUIStore((s) => s.toggleSidebar)
  const changesPanelOpen = useUIStore((s) => s.changesPanelOpen)
  const fileTreePanelOpen = useUIStore((s) => s.fileTreePanelOpen)
  const toggleChangesPanel = useUIStore((s) => s.toggleChangesPanel)
  const toggleFileTreePanel = useUIStore((s) => s.toggleFileTreePanel)

  const currentSessionId = useSessionStore((s) => s.currentSessionId)
  const projects = useSessionStore((s) => s.projects)

  const permissionRequests = useChatStore((s) => s.permissionRequests)
  const currentPermission = permissionRequests[0]
  const planApprovalRequest = useChatStore((s) => s.planApprovalRequest)

  const [editingTitle, setEditingTitle] = useState(false)
  const [moreMenuOpen, setMoreMenuOpen] = useState(false)
  const [isResolvingPermission, setIsResolvingPermission] = useState(false)
  const [isResolvingPlanApproval, setIsResolvingPlanApproval] = useState(false)
  const titleRef = useRef<HTMLDivElement>(null)
  const moreMenuRef = useRef<HTMLDivElement>(null)

  // Find current session title
  const currentSession = projects.flatMap((p) => p.sessions).find((s) => s.id === currentSessionId)
  const title = currentSession?.title || '新对话'

  async function handlePermissionDecision(decision: string) {
    if (!gatewayAPI || !currentPermission || isResolvingPermission) return
    setIsResolvingPermission(true)
    try {
      await gatewayAPI.resolvePermission({
        request_id: currentPermission.request_id,
        decision,
      })
      useUIStore.getState().showToast('已处理权限请求', 'success')
    } catch (err) {
      const message = err instanceof Error ? err.message : '处理权限请求失败'
      useUIStore.getState().showToast(message, 'error')
      console.error('Resolve permission failed:', err)
    } finally {
      setIsResolvingPermission(false)
    }
  }

  async function handlePlanApprovalDecision(decision: string) {
    if (!gatewayAPI || !planApprovalRequest || isResolvingPlanApproval) return
    setIsResolvingPlanApproval(true)
    try {
      await gatewayAPI.resolvePlanApproval({
        request_id: planApprovalRequest.request_id,
        decision,
      })
      if (decision === PlanApprovalDecision.Approve) {
        useChatStore.getState().setAgentMode('build')
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '处理计划审批失败'
      useUIStore.getState().showToast(message, 'error')
      console.error('Resolve plan approval failed:', err)
    } finally {
      setIsResolvingPlanApproval(false)
      useChatStore.getState().clearPlanApprovalRequest()
    }
  }

  useEffect(() => {
    function handleClick(event: MouseEvent) {
      if (moreMenuRef.current && !moreMenuRef.current.contains(event.target as Node)) {
        setMoreMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  useEffect(() => {
    if (editingTitle && titleRef.current) {
      titleRef.current.focus()
      const range = document.createRange()
      range.selectNodeContents(titleRef.current)
      const selection = window.getSelection()
      if (selection) {
        selection.removeAllRanges()
        selection.addRange(range)
      }
    }
  }, [editingTitle])

  const handleTitleSave = async () => {
    const newTitle = titleRef.current?.innerText.trim()
    if (newTitle && newTitle !== title && currentSessionId && gatewayAPI) {
      try {
        await gatewayAPI.renameSession(currentSessionId, newTitle)
        await useSessionStore.getState().fetchSessions(gatewayAPI)
      } catch (err) {
        console.error('Rename session failed:', err)
      }
    }
    setEditingTitle(false)
  }

  return (
    <div style={styles.container}>
      {/* Header */}
      <div style={styles.header}>
        <div style={styles.headerLeft}>
          {!sidebarOpen && (
            <button style={styles.expandSidebarBtn} onClick={toggleSidebar} title="展开侧边栏">
              <PanelRightOpen size={16} />
            </button>
          )}
          {editingTitle ? (
            <div
              ref={titleRef}
              contentEditable
              suppressContentEditableWarning
              style={styles.titleEditable}
              onBlur={handleTitleSave}
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleTitleSave() } }}
            >
              {title}
            </div>
          ) : (
            <div style={styles.titleRow} onClick={() => setEditingTitle(true)}>
              <h2 style={styles.title}>{title}</h2>
              <span style={styles.editHint}><Edit3 size={12} /></span>
            </div>
          )}
        </div>
        <div style={styles.headerRight}>
          <ModelSelector />
          <div ref={moreMenuRef} style={{ position: 'relative' }}>
            <button
              style={{
                ...styles.headerBtn,
                color: moreMenuOpen ? 'var(--text-primary)' : 'var(--text-tertiary)',
                background: moreMenuOpen ? 'var(--bg-hover)' : 'transparent',
              }}
              title="更多"
              onClick={() => setMoreMenuOpen(!moreMenuOpen)}
            >
              <MoreHorizontal size={16} />
            </button>
            {moreMenuOpen && (
              <div style={styles.moreMenu} className="animate-slide-up">
                <button style={styles.moreMenuItem} onClick={() => { setMoreMenuOpen(false); setEditingTitle(true) }}>
                  重命名会话
                </button>
                <button style={styles.moreMenuItem} onClick={async () => {
                  setMoreMenuOpen(false)
                  if (currentSessionId && gatewayAPI) {
                    try {
                      await gatewayAPI.deleteSession(currentSessionId)
                      await useSessionStore.getState().fetchSessions(gatewayAPI)
			      useSessionStore.getState().prepareNewChat()
                    } catch (err) {
                      console.error('Archive session failed:', err)
                    }
                  }
                }}>
                  归档会话
                </button>
              </div>
            )}
          </div>
          <button
            style={{ ...styles.headerBtn, color: changesPanelOpen ? 'var(--accent)' : 'var(--text-tertiary)' }}
            title="文件更改"
            onClick={toggleChangesPanel}
          >
            <FileDiff size={16} />
          </button>
          <button
            style={{ ...styles.headerBtn, color: fileTreePanelOpen ? 'var(--accent)' : 'var(--text-tertiary)' }}
            title="文件目录"
            onClick={toggleFileTreePanel}
          >
            <FolderTree size={16} />
          </button>
        </div>
      </div>

      {/* Messages */}
      <div style={styles.messagesArea} data-scroll-root="1">
        <MessageList />
      </div>

      {/* Input, Plan Approval, or Permission Request */}
      {planApprovalRequest ? (
        <div style={planApprovalStyles.container}>
          <div style={planApprovalStyles.card}>
            <div style={planApprovalStyles.header}>
              <Check size={16} style={{ color: 'var(--color-accent)' }} />
              <span style={planApprovalStyles.headerTitle}>计划已生成</span>
            </div>
            <div style={planApprovalStyles.summary}>
              {planApprovalRequest.summary || '（暂无摘要）'}
            </div>
            <div style={planApprovalStyles.prompt}>是否执行此计划？</div>
            <div style={planApprovalStyles.buttons}>
              <button
                onClick={() => handlePlanApprovalDecision(PlanApprovalDecision.Reject)}
                disabled={isResolvingPlanApproval}
                style={{ ...planApprovalStyles.btn, ...planApprovalStyles.btnReject, opacity: isResolvingPlanApproval ? 0.6 : 1, cursor: isResolvingPlanApproval ? 'not-allowed' : 'pointer' }}
              >
                <X size={13} /> 暂不执行
              </button>
              <button
                onClick={() => handlePlanApprovalDecision(PlanApprovalDecision.Approve)}
                disabled={isResolvingPlanApproval}
                style={{ ...planApprovalStyles.btn, ...planApprovalStyles.btnPrimary, opacity: isResolvingPlanApproval ? 0.6 : 1, cursor: isResolvingPlanApproval ? 'not-allowed' : 'pointer' }}
              >
                <Check size={13} /> 执行计划
              </button>
            </div>
          </div>
        </div>
      ) : currentPermission ? (
        <div style={permissionStyles.container}>
          <div style={permissionStyles.card}>
            <div style={permissionStyles.header}>
              <Shield size={16} style={{ color: 'var(--warning)' }} />
              <span style={permissionStyles.headerTitle}>权限请求</span>
            </div>
            <div style={permissionStyles.details}>
              <div style={permissionStyles.detailRow}>
                <span style={permissionStyles.detailLabel}>工具</span>
                <span style={permissionStyles.detailValue}>
                  {currentPermission.tool_name || currentPermission.tool_category || '-'}
                </span>
              </div>
              <div style={permissionStyles.detailRow}>
                <span style={permissionStyles.detailLabel}>操作</span>
                <span style={permissionStyles.detailValue}>
                  {currentPermission.operation || currentPermission.action_type || '-'}
                </span>
              </div>
              <div style={permissionStyles.detailRow}>
                <span style={permissionStyles.detailLabel}>目标</span>
                <span style={{ ...permissionStyles.detailValue, fontSize: 11 }}>
                  {currentPermission.target || currentPermission.target_type || '-'}
                </span>
              </div>
              {currentPermission.reason && (
                <div style={permissionStyles.detailRow}>
                  <span style={permissionStyles.detailLabel}>原因</span>
                  <span style={{ ...permissionStyles.detailValue, fontSize: 11 }}>
                    {currentPermission.reason}
                  </span>
                </div>
              )}
            </div>
            <div style={permissionStyles.buttons}>
              <button
                onClick={() => handlePermissionDecision(PermissionDecision.Reject)}
                disabled={isResolvingPermission}
                style={{ ...permissionStyles.btn, ...permissionStyles.btnReject, opacity: isResolvingPermission ? 0.6 : 1, cursor: isResolvingPermission ? 'not-allowed' : 'pointer' }}
              >
                <X size={13} /> 拒绝
              </button>
              <button
                onClick={() => handlePermissionDecision(PermissionDecision.AllowOnce)}
                disabled={isResolvingPermission}
                style={{ ...permissionStyles.btn, ...permissionStyles.btnPrimary, opacity: isResolvingPermission ? 0.6 : 1, cursor: isResolvingPermission ? 'not-allowed' : 'pointer' }}
              >
                <Check size={13} /> 允许一次
              </button>
              <button
                onClick={() => handlePermissionDecision(PermissionDecision.AllowSession)}
                disabled={isResolvingPermission}
                style={{ ...permissionStyles.btn, ...permissionStyles.btnSecondary, opacity: isResolvingPermission ? 0.6 : 1, cursor: isResolvingPermission ? 'not-allowed' : 'pointer' }}
              >
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

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 16px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
    gap: 12,
  },
  headerLeft: {
    minWidth: 0,
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  expandSidebarBtn: {
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
    flexShrink: 0,
  },
  titleRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    cursor: 'text',
    padding: '2px 6px',
    marginLeft: -6,
    borderRadius: 'var(--radius-sm)',
    transition: 'background 0.15s',
  },
  title: {
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
    margin: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontFamily: 'var(--font-ui)',
  },
  editHint: {
    color: 'var(--text-tertiary)',
    opacity: 0,
    transition: 'opacity 0.15s',
  },
  titleEditable: {
    fontSize: 15,
    fontWeight: 600,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-ui)',
    padding: '2px 6px',
    borderRadius: 'var(--radius-sm)',
    outline: 'none',
    border: '1px solid var(--accent)',
    background: 'var(--bg-primary)',
    minWidth: 200,
  },
  headerRight: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    flexShrink: 0,
  },
  headerBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 30,
    height: 30,
    borderRadius: 'var(--radius-md)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  moreMenu: {
    position: 'absolute',
    top: 'calc(100% + 6px)',
    right: 0,
    width: 142,
    padding: 4,
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
    boxShadow: 'var(--shadow-3)',
    zIndex: 60,
  },
  moreMenuItem: {
    display: 'block',
    width: '100%',
    padding: '7px 9px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-primary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  messagesArea: {
    flex: 1,
    overflowY: 'auto',
    overflowX: 'hidden',
    padding: '0 16px',
  },
}

const permissionStyles: Record<string, React.CSSProperties> = {
  container: {
    padding: '12px 16px 8px',
    flexShrink: 0,
    background: 'var(--bg-primary)',
  },
  card: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-lg)',
    background: 'var(--bg-secondary)',
    padding: '12px 14px',
    maxWidth: 800,
    margin: '0 auto',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginBottom: 10,
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  details: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    marginBottom: 12,
    fontSize: 12,
  },
  detailRow: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: 12,
  },
  detailLabel: {
    color: 'var(--text-tertiary)',
    flexShrink: 0,
  },
  detailValue: {
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontWeight: 500,
    textAlign: 'right',
    wordBreak: 'break-all',
  },
  buttons: {
    display: 'flex',
    gap: 8,
  },
  btn: {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: '7px 10px',
    borderRadius: 'var(--radius-md)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    transition: 'all 0.15s',
    border: '1px solid var(--border-primary)',
  },
  btnReject: {
    background: 'var(--bg-tertiary)',
    color: 'var(--text-primary)',
  },
  btnPrimary: {
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
  },
  btnSecondary: {
    background: 'var(--bg-active)',
    color: 'var(--text-primary)',
  },
}

const planApprovalStyles: Record<string, React.CSSProperties> = {
  container: {
    padding: '8px 12px 12px',
    borderTop: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
  },
  card: {
    border: '1px solid var(--color-accent)',
    borderRadius: 'var(--radius-md)',
    padding: 12,
    background: 'var(--bg-tertiary)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginBottom: 8,
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: 600,
    fontFamily: 'var(--font-ui)',
    color: 'var(--text-primary)',
  },
  summary: {
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    color: 'var(--text-secondary)',
    marginBottom: 8,
    lineHeight: 1.5,
  },
  prompt: {
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    color: 'var(--text-primary)',
    marginBottom: 10,
  },
  buttons: {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 8,
  },
  btn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: '7px 14px',
    borderRadius: 'var(--radius-md)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    transition: 'all 0.15s',
    border: '1px solid var(--border-primary)',
  },
  btnReject: {
    background: 'var(--bg-tertiary)',
    color: 'var(--text-primary)',
  },
  btnPrimary: {
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
  },
}
