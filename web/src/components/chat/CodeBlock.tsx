import { memo, useState } from 'react'
import { Copy, Check, Download, FileText } from 'lucide-react'

const langColors: Record<string, string> = {
  javascript: '#f7df1e',
  typescript: '#3178c6',
  go: '#00add8',
  python: '#3776ab',
  rust: '#dea584',
  html: '#e34c26',
  css: '#264de4',
  bash: '#4eaa25',
  json: '#292929',
  yaml: '#cb171e',
  markdown: '#083fa1',
}

interface CodeBlockProps {
  code: string
  language?: string
  filename?: string
}

/**
 * 代码块组件:`filename` 决定形态。
 * - inline:无 filename,叙述性代码,极简样式;hover 出现复制按钮。
 * - file:显式文件代码,保留 header(图标 + 文件名 + 语言徽章 + 复制/下载)与行号。footer 已下线。
 */
function CodeBlock({ code, language = 'text', filename }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)
  const [hovered, setHovered] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (!filename) {
    return (
      <div
        style={inlineStyles.container}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <pre style={inlineStyles.pre}>
          <code style={inlineStyles.code}>{code}</code>
        </pre>
        {hovered && (
          <button style={inlineStyles.copyBtn} onClick={handleCopy} title="复制">
            {copied ? <Check size={12} /> : <Copy size={12} />}
          </button>
        )}
      </div>
    )
  }

  const lines = code.split('\n')

  return (
    <div style={fileStyles.container}>
      <div style={fileStyles.header}>
        <div style={fileStyles.headerLeft}>
          <FileText size={12} />
          <span style={fileStyles.filename}>{filename}</span>
          <span style={{ ...fileStyles.langBadge, background: langColors[language] || 'var(--border-primary)' }}>
            {language}
          </span>
        </div>
        <div style={fileStyles.headerRight}>
          <button style={fileStyles.headerBtn} onClick={handleCopy} title="复制">
            {copied ? <Check size={14} /> : <Copy size={14} />}
          </button>
          <button style={fileStyles.headerBtn} title="下载">
            <Download size={14} />
          </button>
        </div>
      </div>
      <div style={fileStyles.codeWrap}>
        <pre style={fileStyles.pre}>
          <code style={fileStyles.code}>
            {lines.map((line, i) => (
              <div key={i} style={fileStyles.line}>
                <span style={fileStyles.lineNum}>{i + 1}</span>
                <span style={fileStyles.lineContent}>{line || ' '}</span>
              </div>
            ))}
          </code>
        </pre>
      </div>
    </div>
  )
}

const inlineStyles: Record<string, React.CSSProperties> = {
  container: {
    position: 'relative',
    margin: '6px 0',
    borderRadius: 'var(--radius-md)',
    background: 'var(--code-bg)',
    overflow: 'hidden',
  },
  pre: {
    margin: 0,
    padding: '10px 14px',
    fontFamily: 'var(--font-mono)',
    fontSize: 12.5,
    lineHeight: 1.7,
    overflowX: 'auto',
    background: 'transparent',
  },
  code: {
    color: 'var(--text-primary)',
    whiteSpace: 'pre',
    tabSize: 2,
  },
  copyBtn: {
    position: 'absolute',
    top: 6,
    right: 6,
    width: 24,
    height: 24,
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-secondary)',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    transition: 'all 0.15s',
  },
}

const fileStyles: Record<string, React.CSSProperties> = {
  container: {
    margin: '8px 0',
    borderRadius: 'var(--radius-lg)',
    border: '1px solid var(--border-primary)',
    overflow: 'hidden',
    background: 'var(--code-bg)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 12px',
    borderBottom: '1px solid var(--border-primary)',
    background: 'var(--bg-tertiary)',
  },
  headerLeft: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    color: 'var(--text-secondary)',
    fontSize: 12,
  },
  filename: {
    fontWeight: 500,
    fontFamily: 'var(--font-mono)',
  },
  langBadge: {
    fontSize: 10,
    padding: '1px 6px',
    borderRadius: 'var(--radius-sm)',
    color: '#fff',
    fontWeight: 600,
    textTransform: 'uppercase',
  },
  headerRight: {
    display: 'flex',
    gap: 2,
  },
  headerBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 26,
    height: 26,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  codeWrap: {
    overflowX: 'auto',
    maxHeight: 400,
    overflowY: 'auto',
  },
  pre: {
    margin: 0,
    padding: '10px 0',
    fontFamily: 'var(--font-mono)',
    fontSize: 12.5,
    lineHeight: 1.7,
    background: 'var(--code-bg)',
  },
  code: {
    display: 'block',
  },
  line: {
    display: 'flex',
    padding: '0 12px',
  },
  lineNum: {
    width: 36,
    flexShrink: 0,
    textAlign: 'right',
    paddingRight: 12,
    color: 'var(--text-tertiary)',
    userSelect: 'none',
    fontSize: 11,
  },
  lineContent: {
    color: 'var(--text-primary)',
    whiteSpace: 'pre',
    tabSize: 2,
  },
}

export default memo(CodeBlock)
