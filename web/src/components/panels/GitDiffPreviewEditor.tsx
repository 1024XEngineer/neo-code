import { useMemo, type CSSProperties } from 'react'
import { DiffEditor } from '@monaco-editor/react'
import { detectMonacoLanguage } from './CodePreviewEditor'

type PreviewTheme = 'light' | 'dark'

export interface GitDiffPreviewEditorProps {
  path: string
  originalContent: string
  modifiedContent: string
  theme: PreviewTheme
  renderSideBySide: boolean
}

function mapTheme(theme: PreviewTheme): 'vs' | 'vs-dark' {
  return theme === 'light' ? 'vs' : 'vs-dark'
}

export default function GitDiffPreviewEditor({
  path,
  originalContent,
  modifiedContent,
  theme,
  renderSideBySide,
}: GitDiffPreviewEditorProps) {
  const language = useMemo(() => detectMonacoLanguage(path), [path])
  const options = useMemo(
    () => ({
      readOnly: true,
      automaticLayout: true,
      renderSideBySide,
      minimap: { enabled: false },
      glyphMargin: false,
      lineNumbers: 'on' as const,
      lineNumbersMinChars: 3,
      folding: true,
      matchBrackets: 'always' as const,
      scrollBeyondLastLine: false,
      renderOverviewRuler: false,
      hideUnchangedRegions: { enabled: true },
      stickyScroll: { enabled: false },
      originalEditable: false,
      scrollbar: {
        verticalScrollbarSize: 8,
        horizontalScrollbarSize: 8,
      },
      padding: {
        top: 12,
        bottom: 12,
      },
      fontFamily: 'var(--font-mono)',
      fontSize: 12,
      fontLigatures: true,
      tabSize: 2,
      wordWrap: 'off' as const,
    }),
    [renderSideBySide],
  )

  return (
    <div
      data-testid="git-diff-preview-host"
      className={renderSideBySide ? 'git-diff-preview-host' : 'git-diff-preview-host git-diff-preview-host-inline'}
      style={styles.host}
    >
      <DiffEditor
        height="100%"
        width="100%"
        theme={mapTheme(theme)}
        language={language}
        original={originalContent}
        modified={modifiedContent}
        keepCurrentOriginalModel={false}
        keepCurrentModifiedModel={false}
        loading={<div style={styles.loading}>正在加载 Diff 编辑器...</div>}
        options={options}
      />
    </div>
  )
}

const styles: Record<string, CSSProperties> = {
  host: {
    flex: 1,
    minHeight: 0,
    background: 'var(--code-bg)',
  },
  loading: {
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
  },
}
