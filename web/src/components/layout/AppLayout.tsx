import { useEffect } from 'react'
import Sidebar from './Sidebar'
import ChatPanel from '@/components/chat/ChatPanel'
import FileChangePanel from '@/components/panels/FileChangePanel'
import FileTreePanel from '@/components/panels/FileTreePanel'
import StatusBar from '@/components/status/StatusBar'
import ToastContainer from '@/components/ui/ToastContainer'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { useUIStore } from '@/stores/useUIStore'
import { useSessionStore } from '@/stores/useSessionStore'

interface AppLayoutProps {
  shellMode?: 'electron' | 'browser'
}

/** 三栏布局外壳 */
export default function AppLayout({ shellMode = 'electron' }: AppLayoutProps) {
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const sidebarWidth = useUIStore((s) => s.sidebarWidth)
  const changesPanelOpen = useUIStore((s) => s.changesPanelOpen)
  const changesPanelWidth = useUIStore((s) => s.changesPanelWidth)
  const fileTreePanelOpen = useUIStore((s) => s.fileTreePanelOpen)
  const fileTreePanelWidth = useUIStore((s) => s.fileTreePanelWidth)

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'n' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        useSessionStore.getState().prepareNewChat()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  return (
    <div style={{
      ...layoutStyles.container,
      ...(shellMode === 'browser' ? layoutStyles.browserContainer : null),
    }}>
      <div style={layoutStyles.workspace}>
        {sidebarOpen ? (
          <div style={{ ...layoutStyles.sidebar, width: sidebarWidth }}>
            <ErrorBoundary fallback={(_err, retry) => <div style={{ padding: 12, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8 }}><div style={{ color: 'var(--text-tertiary)', fontSize: 12 }}>Failed to load sidebar</div><button onClick={retry} style={{ padding: '4px 12px', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-primary)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 11, cursor: 'pointer' }}>Retry</button></div>}>
              <Sidebar />
            </ErrorBoundary>
          </div>
        ) : (
          <div style={layoutStyles.sidebarCollapsed}>
            <ErrorBoundary fallback={(_err, retry) => <div style={{ padding: 8, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6 }}><div style={{ color: 'var(--text-tertiary)', fontSize: 11 }}>Sidebar error</div><button onClick={retry} style={{ padding: '2px 8px', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-primary)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 10, cursor: 'pointer' }}>Retry</button></div>}>
              <Sidebar collapsed />
            </ErrorBoundary>
          </div>
        )}
        <div style={layoutStyles.main}>
          <ErrorBoundary fallback={(err, retry) => (
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', gap: 12, padding: 32, color: 'var(--text-tertiary)' }}>
              <div>Failed to load chat panel</div>
              <div style={{ fontSize: 12 }}>{err.message}</div>
              <button onClick={retry} style={{ padding: '6px 16px', borderRadius: 'var(--radius-md)', border: '1px solid var(--border-primary)', background: 'var(--bg-secondary)', color: 'var(--text-primary)', fontSize: 12, cursor: 'pointer' }}>Retry</button>
            </div>
          )}>
            <ChatPanel />
          </ErrorBoundary>
        </div>
        {changesPanelOpen && (
          <div style={{ ...layoutStyles.rightPanel, width: `min(${changesPanelWidth}px, 24vw)` }}>
            <FileChangePanel />
          </div>
        )}
        {fileTreePanelOpen && (
          <div style={{ ...layoutStyles.rightPanel, width: `min(${fileTreePanelWidth}px, 22vw)` }}>
            <FileTreePanel />
          </div>
        )}
      </div>
      <StatusBar />
      <ToastContainer />
    </div>
  )
}

const layoutStyles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100vh',
    width: '100vw',
    overflow: 'hidden',
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
  },
  browserContainer: {
    minHeight: '100dvh',
    height: '100dvh',
  },
  workspace: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    overflow: 'hidden',
  },
  sidebar: {
    flexShrink: 0,
    borderRight: '1px solid var(--border-primary)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  sidebarCollapsed: {
    width: 44,
    flexShrink: 0,
    borderRight: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    paddingTop: 8,
    gap: 4,
  },
  main: {
    flex: 1,
    minWidth: 0,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  rightPanel: {
    flexShrink: 0,
    borderLeft: '1px solid var(--border-primary)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
}
