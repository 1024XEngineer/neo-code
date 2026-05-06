import { Component, type ReactNode } from 'react'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: (error: Error, retry: () => void) => ReactNode
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

/**
 * React ErrorBoundary 防止组件异常导致白屏。
 * 捕获渲染阶段错误并显示恢复界面。
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('[ErrorBoundary] Caught error:', error, errorInfo)
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null })
  }

  handleReload = () => {
    window.location.reload()
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback && this.state.error) {
        return this.props.fallback(this.state.error, this.handleRetry)
      }

      return (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '100dvh',
          background: 'var(--bg-primary)',
          color: 'var(--text-primary)',
          gap: 16,
          padding: 32,
        }}>
          <div style={{ fontSize: 18, fontWeight: 600 }}>The application encountered an error</div>
          <div style={{
            fontSize: 13,
            color: 'var(--text-tertiary)',
            maxWidth: 480,
            textAlign: 'center',
            wordBreak: 'break-word',
          }}>
            {this.state.error?.message || 'Unknown error'}
          </div>
          <div style={{ display: 'flex', gap: 12, marginTop: 8 }}>
            <button
              onClick={this.handleRetry}
              style={{
                padding: '8px 24px',
                borderRadius: 'var(--radius-md)',
                border: '1px solid var(--border-primary)',
                background: 'var(--bg-secondary)',
                color: 'var(--text-primary)',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Retry
            </button>
            <button
              onClick={this.handleReload}
              style={{
                padding: '8px 24px',
                borderRadius: 'var(--radius-md)',
                border: 'none',
                background: 'var(--accent)',
                color: '#fff',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Reload
            </button>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}
