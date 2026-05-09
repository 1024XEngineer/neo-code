import { Streamdown } from 'streamdown'
import 'streamdown/styles.css'

interface MarkdownContentProps {
  content: string
  streaming?: boolean
}

/** Markdown 渲染器，支持 GFM；流式输出时分段增量渲染 */
export default function MarkdownContent({ content, streaming }: MarkdownContentProps) {
  return (
    <div className="markdown-body">
      <Streamdown
        className="markdown-streamdown"
        parseIncompleteMarkdown
        isAnimating={!!streaming}
      >
        {content || ''}
      </Streamdown>
    </div>
  )
}
