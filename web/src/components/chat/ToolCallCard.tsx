import { useEffect, useMemo, useState } from 'react'
import { type ChatMessage } from '@/stores/useChatStore'
import { Loader2, Wrench, CheckCircle2, XCircle, ChevronRight } from 'lucide-react'

interface ToolCallCardProps {
  message: ChatMessage
  /** 是否与上一条 AI/工具消息属于同一回合（同回合则压缩上下间距） */
  groupedWithPrev?: boolean
}

/** 工具调用 — 内联到 AI 回合的折叠行 */
export default function ToolCallCard({ message, groupedWithPrev = false }: ToolCallCardProps) {
  const isRunning = message.toolStatus === 'running'
  const isDone = message.toolStatus === 'done'
  const isError = message.toolStatus === 'error'

  const [expanded, setExpanded] = useState(isRunning)
  const [userToggled, setUserToggled] = useState(false)

  useEffect(() => {
    if (!userToggled && !isRunning) {
      setExpanded(false)
    }
  }, [isRunning, userToggled])

  const argsSummary = useMemo(() => parseArgsSummary(message.toolArgs), [message.toolArgs])
  const resultStats = useMemo(() => formatResultStats(message.toolResult), [message.toolResult])

  function toggle() {
    setUserToggled(true)
    setExpanded((v) => !v)
  }

  return (
    <div style={groupedWithPrev ? styles.rowGrouped : styles.row} className="animate-fade-in">
      <div style={styles.avatarSpacer} aria-hidden />
      <div style={styles.body}>
        <button style={styles.head} onClick={toggle} aria-expanded={expanded}>
          <span style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
            <ChevronRight size={12} />
          </span>
          <Wrench size={12} style={styles.icon} />
          <span style={styles.toolName}>{message.toolName || 'tool'}</span>
          {isRunning && <Loader2 size={12} className="animate-spin" style={styles.iconMuted} />}
          {isDone && <CheckCircle2 size={12} style={styles.iconDone} />}
          {isError && <XCircle size={12} style={styles.iconError} />}
          {argsSummary && <span style={styles.summary}>{argsSummary}</span>}
          {resultStats && <span style={styles.stats}>· {resultStats}</span>}
        </button>
        {expanded && (
          <div style={styles.detail}>
            {renderToolDetail(message)}
          </div>
        )}
      </div>
    </div>
  )
}

/** 按工具名派发详情渲染 */
function renderToolDetail(message: ChatMessage): React.ReactNode {
  const name = message.toolName || ''
  const args = tryParseJson(message.toolArgs)
  const result = message.toolResult

  switch (name) {
    case 'filesystem_edit':
      return <FileEditDiff args={args} result={result} />

    case 'filesystem_write_file':
      return <FileWriteDetail args={args} result={result} />

    case 'bash':
      return <BashDetail args={args} result={result} />

    case 'filesystem_read_file':
      return <ReadFileDetail args={args} result={result} />

    default:
      return (
        <div style={defaultStyles.wrap}>
          {message.toolArgs && (
            <div>
              <div style={styles.sectionLabel}>参数</div>
              <pre style={defaultStyles.pre}>{prettyJson(message.toolArgs)}</pre>
            </div>
          )}
          {message.toolResult && (
            <div>
              <div style={styles.sectionLabel}>结果</div>
              <pre style={defaultStyles.pre}>{prettyJson(message.toolResult)}</pre>
            </div>
          )}
        </div>
      )
  }
}

/** 统一 Diff 渲染(按行切分,红 `-` 绿 `+`) */
function FileEditDiff({ args, result }: { args: Record<string, unknown>; result?: string }) {
  const path = (args.path as string) || '未知文件'
  const search = String(args.search_string || '')
  const replace = String(args.replace_string || '')
  const searchLines = search.split('\n').filter((l) => l !== '')
  const replaceLines = replace.split('\n').filter((l) => l !== '')

  return (
    <div>
      <div style={diffStyles.path}>{path}</div>
      <div style={diffStyles.box}>
        {searchLines.map((line, i) => (
          <div key={`d-${i}`} style={diffStyles.del}>
            <span style={diffStyles.mark}>-</span>
            <span>{line}</span>
          </div>
        ))}
        {replaceLines.map((line, i) => (
          <div key={`a-${i}`} style={diffStyles.add}>
            <span style={diffStyles.mark}>+</span>
            <span>{line}</span>
          </div>
        ))}
      </div>
      {result && (
        <div style={diffStyles.result}>
          <CheckCircle2 size={12} style={{ color: 'var(--success)' }} />
          <span style={{ color: 'var(--text-tertiary)', fontSize: 11 }}>{formatResultStats(result)}</span>
        </div>
      )}
    </div>
  )
}

/** 写文件详情 */
function FileWriteDetail({ args, result }: { args: Record<string, unknown>; result?: string }) {
  const path = (args.path as string) || '未知文件'
  const content = String(args.content || '')
  return (
    <div>
      <div style={diffStyles.path}>{path}</div>
      <pre style={defaultStyles.pre}>{content}</pre>
      {result && (
        <div style={diffStyles.result}>
          <CheckCircle2 size={12} style={{ color: 'var(--success)' }} />
          <span style={{ color: 'var(--text-tertiary)', fontSize: 11 }}>{formatResultStats(result)}</span>
        </div>
      )}
    </div>
  )
}

/** Bash 详情 */
function BashDetail({ args, result }: { args: Record<string, unknown>; result?: string }) {
  const command = String(args.command || '')
  return (
    <div>
      <div style={terminalStyles.wrap}>
        <span style={terminalStyles.prompt}>$</span>
        <span style={terminalStyles.cmd}>{command}</span>
      </div>
      {result && (
        <pre style={terminalStyles.output}>{result}</pre>
      )}
    </div>
  )
}

/** 读文件详情 — 只展路径 + 结果统计,不重复全文 */
function ReadFileDetail({ args, result }: { args: Record<string, unknown>; result?: string }) {
  const path = (args.path as string) || '未知文件'
  return (
    <div>
      <div style={diffStyles.path}>{path}</div>
      {result && (
        <div style={diffStyles.result}>
          <span style={{ color: 'var(--text-tertiary)', fontSize: 11 }}>{formatResultStats(result)}</span>
        </div>
      )}
    </div>
  )
}

/** 辅助 */
function tryParseJson(str?: string): Record<string, unknown> {
  if (!str) return {}
  try {
    return JSON.parse(str) as Record<string, unknown>
  } catch {
    return {}
  }
}

function prettyJson(str?: string): string {
  if (!str) return ''
  try {
    return JSON.stringify(JSON.parse(str), null, 2)
  } catch {
    return str
  }
}

function parseArgsSummary(args?: string): string {
  if (!args) return ''
  const trimmed = args.trim()
  if (!trimmed) return ''
  try {
    const obj = JSON.parse(trimmed)
    if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
      for (const key of Object.keys(obj)) {
        const v = (obj as Record<string, unknown>)[key]
        if (typeof v === 'string' && v.length > 0) return truncate(v, 80)
        if (typeof v === 'number' || typeof v === 'boolean') return String(v)
      }
      return ''
    }
    return truncate(trimmed, 80)
  } catch {
    return truncate(trimmed, 80)
  }
}

function formatResultStats(result?: string): string {
  if (!result) return ''
  const lines = result.split(/\r?\n/).length
  if (lines >= 3) return `${lines} lines`
  return `${result.length} chars`
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s
  return s.slice(0, n) + '…'
}

const styles: Record<string, React.CSSProperties> = {
  row: {
    display: 'flex',
    gap: 10,
    padding: '2px 0',
  },
  rowGrouped: {
    display: 'flex',
    gap: 10,
    padding: '0',
  },
  avatarSpacer: {
    width: 28,
    flexShrink: 0,
  },
  body: {
    flex: 1,
    minWidth: 0,
    borderLeft: '2px solid var(--border-primary)',
    paddingLeft: 10,
  },
  head: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    width: '100%',
    padding: '4px 0',
    background: 'transparent',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
  },
  chevron: {
    display: 'flex',
    color: 'var(--text-tertiary)',
    transition: 'transform 0.15s',
    flexShrink: 0,
  },
  icon: {
    color: 'var(--text-tertiary)',
    flexShrink: 0,
  },
  iconMuted: {
    color: 'var(--text-tertiary)',
    flexShrink: 0,
  },
  iconDone: {
    color: 'var(--success)',
    flexShrink: 0,
  },
  iconError: {
    color: 'var(--error)',
    flexShrink: 0,
  },
  toolName: {
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
  },
  summary: {
    fontSize: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    minWidth: 0,
    marginLeft: 2,
  },
  stats: {
    fontSize: 11,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    flexShrink: 0,
  },
  detail: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
    padding: '4px 0 8px',
  },
  sectionLabel: {
    fontSize: 10,
    color: 'var(--text-tertiary)',
    textTransform: 'uppercase',
    letterSpacing: '0.5px',
    marginBottom: 4,
    fontFamily: 'var(--font-ui)',
  },
}

const defaultStyles: Record<string, React.CSSProperties> = {
  wrap: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  pre: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    background: 'var(--bg-tertiary)',
    padding: '8px 10px',
    borderRadius: 'var(--radius-sm)',
    margin: 0,
    overflow: 'auto',
    maxHeight: 280,
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-all',
    lineHeight: 1.5,
  },
}

const diffStyles: Record<string, React.CSSProperties> = {
  path: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    marginBottom: 4,
    letterSpacing: '0.3px',
  },
  box: {
    borderRadius: 'var(--radius-sm)',
    overflow: 'hidden',
    border: '1px solid var(--border-primary)',
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    lineHeight: 1.6,
  },
  del: {
    background: 'rgba(220,53,69,0.08)',
    color: 'var(--error)',
    padding: '2px 8px',
    display: 'flex',
    gap: 4,
  },
  add: {
    background: 'rgba(40,167,69,0.08)',
    color: 'var(--success)',
    padding: '2px 8px',
    display: 'flex',
    gap: 4,
  },
  mark: {
    width: 14,
    flexShrink: 0,
    opacity: 0.7,
    userSelect: 'none',
  },
  result: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    paddingTop: 4,
  },
}

const terminalStyles: Record<string, React.CSSProperties> = {
  wrap: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    background: 'var(--bg-tertiary)',
    padding: '6px 10px',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-primary)',
  },
  prompt: {
    color: 'var(--accent)',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    fontWeight: 600,
    userSelect: 'none',
  },
  cmd: {
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
  },
  output: {
    margin: '6px 0 0',
    padding: '8px 10px',
    background: 'var(--bg-tertiary)',
    borderRadius: 'var(--radius-sm)',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    color: 'var(--text-secondary)',
    overflow: 'auto',
    maxHeight: 280,
    whiteSpace: 'pre-wrap',
    lineHeight: 1.5,
  },
}
