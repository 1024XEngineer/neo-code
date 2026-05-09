import { useMemo, type CSSProperties } from 'react'
import Editor from '@monaco-editor/react'

type PreviewTheme = 'light' | 'dark'

const extensionLanguageMap: Record<string, string> = {
  go: 'go',
  ts: 'typescript',
  tsx: 'typescript',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  json: 'json',
  md: 'markdown',
  yaml: 'yaml',
  yml: 'yaml',
  sh: 'shell',
  bash: 'shell',
  zsh: 'shell',
  py: 'python',
  html: 'html',
  css: 'css',
  scss: 'scss',
  xml: 'xml',
  sql: 'sql',
  diff: 'diff',
  patch: 'diff',
}

export interface CodePreviewEditorProps {
  path: string
  content: string
  theme: PreviewTheme
  language?: string
}

export function detectMonacoLanguage(path: string): string {
  const normalizedPath = path.trim().toLowerCase()
  if (!normalizedPath) {
    return 'plaintext'
  }

  if (normalizedPath.endsWith('.d.ts')) {
    return 'typescript'
  }

  const extension = normalizedPath.split('.').pop() || ''
  return extensionLanguageMap[extension] || 'plaintext'
}

function mapTheme(theme: PreviewTheme): 'vs' | 'vs-dark' {
  return theme === 'light' ? 'vs' : 'vs-dark'
}

export default function CodePreviewEditor({ path, content, theme, language }: CodePreviewEditorProps) {
  const resolvedLanguage = useMemo(() => language || detectMonacoLanguage(path), [language, path])
  const editorOptions = useMemo(
    () => ({
      readOnly: true,
      automaticLayout: true,
      minimap: { enabled: false },
      glyphMargin: false,
      folding: true,
      lineNumbers: 'on' as const,
      lineNumbersMinChars: 3,
      renderLineHighlight: 'line' as const,
      matchBrackets: 'always' as const,
      scrollBeyondLastLine: false,
      occurrencesHighlight: 'off' as const,
      selectionHighlight: false,
      stickyScroll: { enabled: false },
      overviewRulerBorder: false,
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      cursorStyle: 'line' as const,
      cursorBlinking: 'solid' as const,
      wordWrap: 'off' as const,
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
    }),
    [],
  )

  return (
    <div data-testid="code-preview-host" style={styles.host}>
      <Editor
        height="100%"
        width="100%"
        path={path}
        value={content}
        language={resolvedLanguage}
        theme={mapTheme(theme)}
        keepCurrentModel={false}
        saveViewState={false}
        loading={<div style={styles.loading}>正在加载代码编辑器...</div>}
        options={editorOptions}
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
