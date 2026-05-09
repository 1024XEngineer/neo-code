import {
  Check,
  Copy,
  Download,
  ExternalLink,
  type LucideIcon,
  Loader2,
  Maximize2,
  RotateCcw,
  X,
  ZoomIn,
  ZoomOut,
} from 'lucide-react'
import type { SVGProps } from 'react'
import { code } from '@streamdown/code'
import { cjk } from '@streamdown/cjk'
import { Streamdown } from 'streamdown'
import 'streamdown/styles.css'

interface MarkdownContentProps {
  content: string
  streaming?: boolean
}

const streamdownPlugins = { code, cjk }

type StreamdownIconProps = SVGProps<SVGSVGElement> & { size?: number }

// 将 lucide 图标的 size 收敛为 number，匹配 streamdown 的 IconMap 类型约束。
function adaptIcon(Icon: LucideIcon) {
  return ({ size, ...props }: StreamdownIconProps) => {
    const normalizedSize = typeof size === 'number' ? size : undefined
    return <Icon {...props} size={normalizedSize} />
  }
}

const streamdownIcons = {
  CheckIcon: adaptIcon(Check),
  CopyIcon: adaptIcon(Copy),
  DownloadIcon: adaptIcon(Download),
  ExternalLinkIcon: adaptIcon(ExternalLink),
  Loader2Icon: adaptIcon(Loader2),
  Maximize2Icon: adaptIcon(Maximize2),
  RotateCcwIcon: adaptIcon(RotateCcw),
  XIcon: adaptIcon(X),
  ZoomInIcon: adaptIcon(ZoomIn),
  ZoomOutIcon: adaptIcon(ZoomOut),
}

const streamdownTranslations = {
  copyCode: '复制代码',
  copied: '已复制',
  copyLink: '复制链接',
  openExternalLink: '打开外部链接？',
  externalLinkWarning: '你即将访问外部网站。',
  close: '关闭',
  downloadFile: '下载文件',
  viewFullscreen: '全屏查看',
  exitFullscreen: '退出全屏',
}

/** Markdown 渲染器，支持 GFM；流式输出时分段增量渲染 */
export default function MarkdownContent({ content, streaming }: MarkdownContentProps) {
  return (
    <div className="markdown-body">
      <Streamdown
        className="markdown-streamdown"
        mode={streaming ? 'streaming' : 'static'}
        parseIncompleteMarkdown={!!streaming}
        controls={{
          code: { copy: true, download: false },
          table: false,
          mermaid: false,
        }}
        plugins={streamdownPlugins}
        icons={streamdownIcons}
        translations={streamdownTranslations}
        isAnimating={!!streaming}
      >
        {content || ''}
      </Streamdown>
    </div>
  )
}
