import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import CodeBlock from './CodeBlock'

interface MarkdownContentProps {
  content: string
  streaming?: boolean
}

/** 轻量级代码块：融入文本流的行内样式 */
function InlineCodeBlock({ code }: { code: string }) {
  return (
    <code
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: '0.9em',
        padding: '0.15em 0.35em',
        borderRadius: 'var(--radius-sm)',
        background: 'var(--bg-tertiary)',
        color: 'var(--text-primary)',
      }}
    >
      {code}
    </code>
  )
}

/** 将代码块映射到 CodeBlock 组件 */
function CodeComponent({
  inline,
  className,
  children,
  ...props
}: {
  inline?: boolean
  className?: string
  children?: React.ReactNode
}) {
  if (inline) {
    return (
      <code className="markdown-inline-code" {...props}>
        {children}
      </code>
    )
  }

  const match = /language-(\w+)/.exec(className || '')
  const language = match ? match[1] : 'text'
  const code = String(children).replace(/\n$/, '')
  const lines = code.split('\n')

  // 单行且无明确语言的代码块降级为轻量渲染
  if (lines.length <= 1 && language === 'text') {
    return <InlineCodeBlock code={code} />
  }

  return <CodeBlock code={code} language={language} />
}

function splitStreamingContent(content: string): { completed: string; pending: string } {
  if (!content) return { completed: '', pending: '' }

  // 1. 检测未闭合的代码块
  const fenceMatches = content.match(/```/g)
  if (fenceMatches && fenceMatches.length % 2 === 1) {
    const lastFenceIdx = content.lastIndexOf('```')
    return {
      completed: content.slice(0, lastFenceIdx),
      pending: content.slice(lastFenceIdx),
    }
  }

  // 2. 不在代码块中，按段落分割
  const lastDoubleNewline = content.lastIndexOf('\n\n')
  if (lastDoubleNewline !== -1) {
    if (lastDoubleNewline >= content.length - 2) {
      // \n\n 在末尾，找上一段
      const prevDoubleNewline = content.lastIndexOf('\n\n', lastDoubleNewline - 1)
      if (prevDoubleNewline !== -1) {
        return {
          completed: content.slice(0, prevDoubleNewline + 2),
          pending: content.slice(prevDoubleNewline + 2),
        }
      }
      return { completed: '', pending: content }
    }
    return {
      completed: content.slice(0, lastDoubleNewline + 2),
      pending: content.slice(lastDoubleNewline + 2),
    }
  }

  // 3. 单段：包含完整行内语法或较长时尝试渲染
  const hasBold = /\*\*[^*\n]+\*\*/.test(content)
  const hasCode = /`[^`\n]+`/.test(content)
  const hasItalic = /(?<!\*)\*[^*\n]+\*(?!\*)/.test(content)
  if (hasBold || hasCode || hasItalic || content.length > 300) {
    return { completed: content, pending: '' }
  }

  return { completed: '', pending: content }
}

/** Markdown 渲染器，支持 GFM；流式输出时分段增量渲染 */
export default function MarkdownContent({ content, streaming }: MarkdownContentProps) {
  const { completed, pending } = useMemo(
    () => (streaming ? splitStreamingContent(content) : { completed: content, pending: '' }),
    [content, streaming],
  )

  return (
    <div className="markdown-body">
      {completed && (
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ code: CodeComponent as any }}>
          {completed}
        </ReactMarkdown>
      )}
      {pending && <span style={{ whiteSpace: 'pre-wrap' }}>{pending}</span>}
    </div>
  )
}
