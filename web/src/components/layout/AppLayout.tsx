import { useEffect, useCallback, useRef } from 'react'
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

/** 拖拽调整面板宽度 */
function useResize(onResize: (delta: number) => void) {
  const dragging = useRef(false)

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    dragging.current = true
    const startX = e.clientX

    function onMove(ev: MouseEvent) {
      if (!dragging.current) return
      onResize(ev.clientX - startX)
    }
    function onUp() {
      dragging.current = false
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }, [onResize])

  return { onMouseDown }
}

export default function AppLayout({ shellMode = 'electron' }: AppLayoutProps) {
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const sidebarWidth = useUIStore((s) => s.sidebarWidth)
  const setSidebarWidth = useUIStore((s) => s.setSidebarWidth)
  const changesPanelOpen = useUIStore((s) => s.changesPanelOpen)
  const changesPanelWidth = useUIStore((s) => s.changesPanelWidth)
  const setChangesPanelWidth = useUIStore((s) => s.setChangesPanelWidth)
  const fileTreePanelOpen = useUIStore((s) => s.fileTreePanelOpen)
  const fileTreePanelWidth = useUIStore((s) => s.fileTreePanelWidth)
  const setFileTreePanelWidth = useUIStore((s) => s.setFileTreePanelWidth)

  const sidebarResize = useResize(useCallback((delta) => {
    setSidebarWidth(Math.max(180, Math.min(500, sidebarWidth + delta)))
  }, [sidebarWidth, setSidebarWidth]))

  const changesResize = useResize(useCallback((delta) => {
    setChangesPanelWidth(Math.max(240, Math.min(600, changesPanelWidth - delta)))
  }, [changesPanelWidth, setChangesPanelWidth]))

  const fileTreeResize = useResize(useCallback((delta) => {
    setFileTreePanelWidth(Math.max(240, Math.min(600, fileTreePanelWidth - delta)))
  }, [fileTreePanelWidth, setFileTreePanelWidth]))

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
    <div className="app-shell" style={shellMode === 'browser' ? { minHeight: '100dvh', height: '100dvh' } : undefined}>
      <div className="app-workspace">
        {sidebarOpen ? (
          <div className="sidebar" style={{ width: sidebarWidth, position: 'relative' }}>
            <Sidebar />
            <div
              className="resize-handle"
              onMouseDown={sidebarResize.onMouseDown}
            />
          </div>
        ) : (
          <div className="sidebar-collapsed-wrapper">
            <Sidebar collapsed />
          </div>
        )}
        <div className="main-area">
          <ErrorBoundary fallback={(err, retry) => (
            <div className="error-screen">
              <div className="error-screen-title">Failed to load chat panel</div>
              <div className="error-screen-detail">{err.message}</div>
              <button onClick={retry} className="error-screen-btn">Retry</button>
            </div>
          )}>
            <ChatPanel />
          </ErrorBoundary>
        </div>
        {changesPanelOpen && (
          <div className="right-panel" style={{ width: changesPanelWidth, position: 'relative' }}>
            <div className="resize-handle resize-handle-left" onMouseDown={changesResize.onMouseDown} />
            <FileChangePanel />
          </div>
        )}
        {fileTreePanelOpen && (
          <div className="right-panel" style={{ width: fileTreePanelWidth, position: 'relative' }}>
            <div className="resize-handle resize-handle-left" onMouseDown={fileTreeResize.onMouseDown} />
            <FileTreePanel />
          </div>
        )}
      </div>
      <StatusBar />
      <ToastContainer />
    </div>
  )
}
