import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type RefObject, type WheelEvent as ReactWheelEvent } from 'react'
import { ChevronRight, Check, FileDiff, Loader2, PanelRightClose, RefreshCw, X } from 'lucide-react'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'
import {
  CHANGES_PREVIEW_TAB_ID,
  GIT_DIFF_PREVIEW_TAB_ID,
  useUIStore,
  type FileChange,
  type FilePreviewTab,
  type GitDiffFilePreviewTab,
  type GitDiffSummary,
  type PreviewTab,
} from '@/stores/useUIStore'
import type { DiffHunk, DiffLine } from '@/utils/patchParser'
import type { GitDiffEntry } from '@/api/protocol'

const LazyCodePreviewEditor = lazy(() => import('./CodePreviewEditor'))
const LazyGitDiffPreviewEditor = lazy(() => import('./GitDiffPreviewEditor'))

const changeStatusMeta: Record<string, { label: string; color: string; bg: string }> = {
  added: { label: '新增', color: 'var(--success)', bg: 'rgba(22, 163, 74, 0.12)' },
  modified: { label: '修改', color: 'var(--warning)', bg: 'rgba(217, 119, 6, 0.14)' },
  deleted: { label: '删除', color: 'var(--error)', bg: 'rgba(220, 38, 38, 0.12)' },
  accepted: { label: '已接受', color: 'var(--success)', bg: 'rgba(22, 163, 74, 0.12)' },
  rejected: { label: '已拒绝', color: 'var(--text-tertiary)', bg: 'var(--bg-active)' },
}

const gitDiffStatusMeta: Record<GitDiffEntry['status'], { label: string; short: string; color: string; bg: string }> = {
  added: { label: '新增', short: 'A', color: 'var(--success)', bg: 'rgba(22, 163, 74, 0.12)' },
  modified: { label: '修改', short: 'M', color: 'var(--warning)', bg: 'rgba(217, 119, 6, 0.14)' },
  deleted: { label: '删除', short: 'D', color: 'var(--error)', bg: 'rgba(220, 38, 38, 0.12)' },
  renamed: { label: '重命名', short: 'R', color: 'var(--accent)', bg: 'rgba(59, 130, 246, 0.12)' },
  copied: { label: '复制', short: 'C', color: 'var(--accent-hover)', bg: 'rgba(14, 165, 233, 0.12)' },
  untracked: { label: '未跟踪', short: 'U', color: 'var(--success)', bg: 'rgba(34, 197, 94, 0.12)' },
  conflicted: { label: '冲突', short: '!', color: 'var(--error)', bg: 'rgba(239, 68, 68, 0.12)' },
}

function getChangeStatusMeta(status: string) {
  return changeStatusMeta[status] || changeStatusMeta.modified
}

function getChangeType(change: { status: string; additions: number; deletions: number }) {
  if (['added', 'modified', 'deleted'].includes(change.status)) return change.status as 'added' | 'modified' | 'deleted'
  if (change.additions > 0 && change.deletions > 0) return 'modified'
  if (change.additions > 0) return 'added'
  if (change.deletions > 0) return 'deleted'
  return 'modified'
}

function getChangeCounts(fileChanges: { status: string; additions: number; deletions: number }[]) {
  return fileChanges.reduce(
    (counts, change) => {
      const type = getChangeType(change)
      if (type === 'added') counts.added += 1
      if (type === 'modified') counts.modified += 1
      if (type === 'deleted') counts.deleted += 1
      return counts
    },
    { added: 0, modified: 0, deleted: 0 },
  )
}

function getGitDiffCounts(files: GitDiffEntry[]) {
  return files.reduce(
    (counts, file) => {
      counts[file.status] += 1
      return counts
    },
    {
      added: 0,
      modified: 0,
      deleted: 0,
      renamed: 0,
      copied: 0,
      untracked: 0,
      conflicted: 0,
    } as Record<GitDiffEntry['status'], number>,
  )
}

function getDisplayHunks(change: FileChange): DiffHunk[] {
  if (change.hunks && change.hunks.length > 0) {
    return change.hunks
  }
  if (change.diff && change.diff.length > 0) {
    return [{
      header: '',
      lines: change.diff,
      additions: change.additions,
      deletions: change.deletions,
    }]
  }
  return []
}

function getPreviewBadge(tab: PreviewTab, fileChanges: FileChange[]) {
  if (tab.kind === 'file') {
    const matched = fileChanges.find((change) => change.path === tab.path)
    if (!matched) return null
    const type = getChangeType(matched)
    if (type === 'added') return { label: 'A', color: 'var(--success)' }
    if (type === 'deleted') return { label: 'D', color: 'var(--error)' }
    return { label: 'M', color: 'var(--warning)' }
  }
  if (tab.kind === 'git-diff-file') {
    const meta = gitDiffStatusMeta[tab.status]
    return { label: meta.short, color: meta.color }
  }
  return null
}

function getContextPreviewTabID(activeTab: PreviewTab | undefined) {
  if (!activeTab) return CHANGES_PREVIEW_TAB_ID
  if (activeTab.kind === 'file') return CHANGES_PREVIEW_TAB_ID
  if (activeTab.kind === 'git-diff-file') return GIT_DIFF_PREVIEW_TAB_ID
  return activeTab.id
}

function DiffLineView({ line }: { line: DiffLine }) {
  const lineStyles: Record<DiffLine['type'], CSSProperties> = {
    add: { color: 'var(--diff-add-text)', background: 'var(--diff-add-bg)' },
    del: { color: 'var(--diff-del-text)', background: 'var(--diff-del-bg)' },
    header: { color: 'var(--diff-header-text)', background: 'var(--diff-header-bg)' },
    context: { color: 'var(--diff-context-text)' },
  }

  const prefix = line.type === 'add' ? '+' : line.type === 'del' ? '-' : line.type === 'context' ? ' ' : ''

  return (
    <div style={{ ...styles.diffLine, ...lineStyles[line.type] }}>
      <span style={styles.diffPrefix}>{prefix}</span>
      <span style={styles.diffText}>{line.content}</span>
    </div>
  )
}

function FileChangeItem({
  change,
  expanded,
  onToggle,
  scrollContainerRef,
}: {
  change: FileChange
  expanded: boolean
  onToggle: () => void
  scrollContainerRef: RefObject<HTMLDivElement | null>
}) {
  const acceptFileChange = useUIStore((state) => state.acceptFileChange)
  const meta = getChangeStatusMeta(change.status)
  const reviewed = change.status === 'accepted' || change.status === 'rejected'
  const displayHunks = getDisplayHunks(change)

  // 将 diff 内层收到的纵向滚轮显式转发给外层列表，避免 hover 在 hunk 上时卡住整列滚动。
  const handleHunkWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) {
      return
    }
    const scrollContainer = scrollContainerRef.current
    if (!scrollContainer) {
      return
    }
    scrollContainer.scrollBy({ top: event.deltaY })
    event.preventDefault()
  }

  return (
    <div data-testid={`change-card-${change.id}`} style={styles.changeCard}>
      <button style={styles.changeHeader} onClick={onToggle}>
        <span style={{ ...styles.chevron, transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          <ChevronRight size={12} />
        </span>
        <span style={styles.fileIcon}>
          <FileDiff size={14} />
        </span>
        <span style={styles.changeMain}>
          <span style={styles.pathText}>{change.path}</span>
          <span style={styles.changeStats}>
            <span style={{ ...styles.statusPill, color: meta.color, background: meta.bg }}>{meta.label}</span>
            <span style={styles.additions}>+{change.additions}</span>
            <span style={styles.deletions}>-{change.deletions}</span>
          </span>
        </span>
      </button>

      {expanded && (
        <div style={styles.expandedArea}>
          <div style={styles.actionsRow}>
            <button
              style={{ ...styles.actionBtn, color: reviewed ? 'var(--text-tertiary)' : 'var(--success)' }}
              onClick={() => acceptFileChange(change.id)}
              disabled={reviewed}
              title="标记为已审阅"
            >
              <Check size={13} />
              接受
            </button>
          </div>
          <div data-testid={`diff-scroller-${change.id}`} style={styles.diffScroller}>
            {displayHunks.length === 0 ? (
              <div style={styles.emptyDiff}>当前文件没有可展示的 diff</div>
            ) : (
              displayHunks.map((hunk, index) => (
                <div
                  key={`${change.id}-hunk-${index}`}
                  data-testid={`diff-hunk-${change.id}-${index}`}
                  style={styles.hunkBlock}
                >
                  <div
                    data-testid={`diff-hunk-scroller-${change.id}-${index}`}
                    style={styles.hunkScroller}
                    onWheel={handleHunkWheel}
                  >
                    {hunk.lines.map((line, lineIndex) => (
                      <DiffLineView key={`${change.id}-${index}-${lineIndex}`} line={line} />
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function ChangesView() {
  const fileChanges = useUIStore((state) => state.fileChanges)
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const counts = useMemo(() => getChangeCounts(fileChanges), [fileChanges])
  const scrollAreaRef = useRef<HTMLDivElement | null>(null)

  const toggleExpanded = (id: string) => {
    setExpandedIds((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <div style={styles.viewContainer}>
      <div style={styles.viewHeader}>
        <span style={styles.viewTitle}>文件变更</span>
        <div style={styles.summaryRow}>
          <span>{fileChanges.length} 个文件</span>
          <span style={styles.summaryDivider} />
          <span style={{ color: 'var(--success)' }}>{counts.added} 新增</span>
          <span style={{ color: 'var(--warning)' }}>{counts.modified} 修改</span>
          <span style={{ color: 'var(--error)' }}>{counts.deleted} 删除</span>
        </div>
      </div>

      <div ref={scrollAreaRef} data-testid="changes-scroll-area" style={styles.scrollArea}>
        {fileChanges.length === 0 ? (
          <div style={styles.emptyState}>当前会话暂无文件变更</div>
        ) : (
          <div data-testid="changes-content-stack" style={styles.contentStack}>
            {fileChanges.map((change) => (
              <FileChangeItem
                key={change.id}
                change={change}
                expanded={expandedIds.has(change.id)}
                onToggle={() => toggleExpanded(change.id)}
                scrollContainerRef={scrollAreaRef}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function PreviewFallback({ message = '正在加载代码编辑器...' }: { message?: string }) {
  return (
    <div style={styles.previewEmptyState}>
      <Loader2 size={16} style={{ animation: 'spin 1s linear infinite' }} />
      <span>{message}</span>
    </div>
  )
}

function FilePreviewView({ tab }: { tab: FilePreviewTab }) {
  const theme = useUIStore((state) => state.theme)

  let body = null
  if (tab.loading) {
    body = <PreviewFallback message="正在加载文件预览..." />
  } else if (tab.error) {
    body = <div style={styles.previewEmptyState}>读取失败: {tab.error}</div>
  } else if (tab.is_binary) {
    body = <div style={styles.previewEmptyState}>该文件为二进制内容，暂不支持预览。</div>
  } else if (tab.truncated) {
    body = <div style={styles.previewEmptyState}>该文件超过 512 KiB，当前预览不会加载正文。</div>
  } else if (tab.loaded && tab.content.length === 0) {
    body = <div style={styles.previewEmptyState}>空文件</div>
  } else {
    body = (
      <Suspense fallback={<PreviewFallback />}>
        <LazyCodePreviewEditor path={tab.path} content={tab.content} theme={theme} />
      </Suspense>
    )
  }

  return (
    <div style={styles.viewContainer}>
      <div style={styles.viewHeader} data-testid="file-preview-header">
        <div style={styles.previewPath} data-testid="file-preview-path" title={tab.path}>{tab.path}</div>
      </div>
      {body}
    </div>
  )
}

function GitDiffFileView({ tab }: { tab: GitDiffFilePreviewTab }) {
  const theme = useUIStore((state) => state.theme)
  const changesPanelWidth = useUIStore((state) => state.changesPanelWidth)

  let body = null
  if (tab.loading) {
    body = <PreviewFallback message="正在加载 Git Diff..." />
  } else if (tab.error) {
    body = <div style={styles.previewEmptyState}>读取失败: {tab.error}</div>
  } else if (tab.is_binary) {
    body = <div style={styles.previewEmptyState}>该文件包含二进制内容，暂不支持 Diff 预览。</div>
  } else if (tab.truncated) {
    body = <div style={styles.previewEmptyState}>该文件超过 512 KiB，当前 Diff 预览不会加载正文。</div>
  } else {
    body = (
      <Suspense fallback={<PreviewFallback message="正在加载 Diff 编辑器..." />}>
        <LazyGitDiffPreviewEditor
          path={tab.path}
          originalContent={tab.original_content}
          modifiedContent={tab.modified_content}
          theme={theme}
          renderSideBySide={changesPanelWidth >= 520}
        />
      </Suspense>
    )
  }

  return (
    <div style={styles.viewContainer}>
      <div style={styles.viewHeader} data-testid="git-diff-file-preview-header">
        <div style={styles.previewPath} data-testid="git-diff-file-preview-path" title={tab.path}>{tab.path}</div>
      </div>
      {body}
    </div>
  )
}

function GitDiffView({
  summary,
  loading,
  error,
  onRefresh,
  onOpenFile,
}: {
  summary: GitDiffSummary
  loading: boolean
  error: string
  onRefresh: () => void
  onOpenFile: (path: string) => void
}) {
  const counts = useMemo(() => getGitDiffCounts(summary.files), [summary.files])

  return (
    <div style={styles.viewContainer}>
      <div style={styles.viewHeader}>
        <div style={styles.titleRow}>
          <span style={styles.viewTitle}>Git Diff</span>
          <button type="button" style={styles.iconBtn} onClick={onRefresh} title="刷新 Git Diff">
            <RefreshCw size={14} />
          </button>
        </div>
        {summary.in_git_repo ? (
          <>
            <div style={styles.summaryRow}>
              <span>分支 {summary.branch || 'HEAD'}</span>
              <span style={styles.summaryDivider} />
              <span>ahead {summary.ahead}</span>
              <span>behind {summary.behind}</span>
              <span style={styles.summaryDivider} />
              <span>{summary.total_count} 个文件</span>
            </div>
            <div style={styles.summaryWrap}>
              <span style={{ color: gitDiffStatusMeta.added.color }}>{counts.added} 新增</span>
              <span style={{ color: gitDiffStatusMeta.modified.color }}>{counts.modified} 修改</span>
              <span style={{ color: gitDiffStatusMeta.deleted.color }}>{counts.deleted} 删除</span>
              <span style={{ color: gitDiffStatusMeta.untracked.color }}>{counts.untracked} 未跟踪</span>
              <span style={{ color: gitDiffStatusMeta.renamed.color }}>{counts.renamed} 重命名</span>
              <span style={{ color: gitDiffStatusMeta.copied.color }}>{counts.copied} 复制</span>
              <span style={{ color: gitDiffStatusMeta.conflicted.color }}>{counts.conflicted} 冲突</span>
            </div>
          </>
        ) : (
          <div style={styles.previewMeta}>当前工作区不是 Git 仓库</div>
        )}
      </div>

      <div data-testid="git-diff-scroll-area" style={styles.scrollArea}>
        {loading && <PreviewFallback message="正在加载 Git Diff 列表..." />}
        {!loading && error && <div style={styles.emptyState}>加载失败: {error}</div>}
        {!loading && !error && !summary.in_git_repo && (
          <div style={styles.emptyState}>当前工作区不是 Git 仓库</div>
        )}
        {!loading && !error && summary.in_git_repo && summary.files.length === 0 && (
          <div style={styles.emptyState}>当前工作树没有未提交变更</div>
        )}
        {!loading && !error && summary.in_git_repo && summary.files.length > 0 && (
          <div data-testid="git-diff-content-stack" style={styles.contentStack}>
            {summary.files.map((file) => {
              const meta = gitDiffStatusMeta[file.status]
              return (
                <button
                  key={file.path}
                  type="button"
                  data-testid={`git-diff-entry-${file.path}`}
                  style={styles.gitDiffItem}
                  onClick={() => onOpenFile(file.path)}
                >
                  <span style={{ ...styles.gitDiffBadge, color: meta.color, background: meta.bg }}>{meta.short}</span>
                  <span style={styles.gitDiffItemMain}>
                    <span style={styles.pathText}>{file.path}</span>
                    {file.old_path && <span style={styles.gitDiffOldPath}>{file.old_path} → {file.path}</span>}
                  </span>
                </button>
              )
            })}
            {summary.truncated && <div style={styles.previewMeta}>列表已按上限截断，仅显示前 200 个变更文件。</div>}
          </div>
        )}
      </div>
    </div>
  )
}

function PreviewTabStrip({
  tabs,
  activeTabId,
  fileChanges,
  onActivate,
  onClose,
}: {
  tabs: PreviewTab[]
  activeTabId: string
  fileChanges: FileChange[]
  onActivate: (id: string) => void
  onClose: (id: string) => void
}) {
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({})
  const fixedTabs = useMemo(
    () => tabs.filter((tab) => tab.kind === 'changes' || tab.kind === 'git-diff'),
    [tabs],
  )
  const previewFileTabs = useMemo(
    () => tabs.filter((tab) => tab.kind === 'file' || tab.kind === 'git-diff-file'),
    [tabs],
  )
  const activeTab = useMemo(
    () => tabs.find((tab) => tab.id === activeTabId),
    [activeTabId, tabs],
  )
  const contextTabId = useMemo(
    () => getContextPreviewTabID(activeTab),
    [activeTab],
  )

  useEffect(() => {
    const active = tabRefs.current[activeTabId]
    if (active && document.activeElement && active.closest('[data-preview-tablist="1"]')?.contains(document.activeElement)) {
      active.focus()
    }
  }, [activeTabId, tabs])

  const moveFocus = (nextIndex: number) => {
    const nextTab = tabs[nextIndex]
    if (!nextTab) return
    onActivate(nextTab.id)
    tabRefs.current[nextTab.id]?.focus()
  }

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>, index: number) => {
    if (event.key === 'ArrowRight') {
      event.preventDefault()
      moveFocus((index + 1) % tabs.length)
      return
    }
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      moveFocus((index - 1 + tabs.length) % tabs.length)
      return
    }
    if (event.key === 'Home') {
      event.preventDefault()
      moveFocus(0)
      return
    }
    if (event.key === 'End') {
      event.preventDefault()
      moveFocus(tabs.length - 1)
    }
  }

  const renderTab = (tab: PreviewTab, index: number, appearance: 'switcher' | 'chip') => {
    const badge = getPreviewBadge(tab, fileChanges)
    const active = tab.id === activeTabId
    const contextActive = appearance === 'switcher' && !active && tab.id === contextTabId
    const title = tab.kind === 'file' || tab.kind === 'git-diff-file' ? tab.path : tab.title

    if (appearance === 'switcher') {
      return (
        <button
          key={tab.id}
          ref={(node) => {
            tabRefs.current[tab.id] = node
          }}
          id={`preview-tab-${tab.id}`}
          data-testid={`preview-tab-${tab.id}`}
          data-context-active={contextActive ? 'true' : 'false'}
          role="tab"
          type="button"
          aria-selected={active}
          aria-controls={`preview-panel-${tab.id}`}
          tabIndex={active ? 0 : -1}
          title={title}
          onClick={() => onActivate(tab.id)}
          onKeyDown={(event) => handleKeyDown(event, index)}
          style={{
            ...styles.switcherButton,
            ...(active ? styles.switcherButtonActive : {}),
            ...(contextActive ? styles.switcherButtonContext : {}),
          }}
        >
          <span style={styles.switcherLabel}>{tab.title}</span>
        </button>
      )
    }

    return (
      <div
        key={tab.id}
        style={{
          ...styles.chipItem,
          ...(active ? styles.chipItemActive : {}),
        }}
        title={title}
      >
        <button
          ref={(node) => {
            tabRefs.current[tab.id] = node
          }}
          id={`preview-tab-${tab.id}`}
          data-testid={`preview-tab-${tab.id}`}
          role="tab"
          type="button"
          aria-selected={active}
          aria-controls={`preview-panel-${tab.id}`}
          tabIndex={active ? 0 : -1}
          onClick={() => onActivate(tab.id)}
          onKeyDown={(event) => handleKeyDown(event, index)}
          style={styles.chipButton}
        >
          {badge && <span style={{ ...styles.chipDot, background: badge.color }} />}
          <span style={styles.chipLabel}>{tab.title}</span>
        </button>
        {tab.closable && (
          <button
            type="button"
            data-testid={`preview-tab-close-${tab.id}`}
            aria-label={`close ${tab.title}`}
            onClick={(event) => {
              event.stopPropagation()
              onClose(tab.id)
            }}
            style={{
              ...styles.chipCloseButton,
              ...(active ? styles.chipCloseButtonActive : {}),
            }}
          >
            <X size={12} />
          </button>
        )}
      </div>
    )
  }

  return (
    <div role="tablist" aria-label="right preview tabs" data-preview-tablist="1" style={styles.tabStack}>
      <div data-testid="preview-primary-tabs" style={styles.switcherRow}>
        <div style={styles.switcherRail}>
          {fixedTabs.map((tab) => {
            const index = tabs.findIndex((entry) => entry.id === tab.id)
            return renderTab(tab, index, 'switcher')
          })}
        </div>
      </div>
      {previewFileTabs.length > 0 && (
        <div data-testid="preview-secondary-tabs" style={styles.chipRow}>
          {previewFileTabs.map((tab) => {
            const index = tabs.findIndex((entry) => entry.id === tab.id)
            return renderTab(tab, index, 'chip')
          })}
        </div>
      )}
    </div>
  )
}

export default function FileChangePanel() {
  const gatewayAPI = useGatewayAPI()
  const currentWorkspaceHash = useWorkspaceStore((state) => state.currentWorkspaceHash)
  const fileChanges = useUIStore((state) => state.fileChanges)
  const gitDiffSummary = useUIStore((state) => state.gitDiffSummary)
  const gitDiffLoading = useUIStore((state) => state.gitDiffLoading)
  const gitDiffError = useUIStore((state) => state.gitDiffError)
  const gitDiffRefreshToken = useUIStore((state) => state.gitDiffRefreshToken)
  const previewTabs = useUIStore((state) => state.previewTabs)
  const activePreviewTabId = useUIStore((state) => state.activePreviewTabId)
  const activatePreviewTab = useUIStore((state) => state.activatePreviewTab)
  const closePreviewTab = useUIStore((state) => state.closePreviewTab)
  const openGitDiffTab = useUIStore((state) => state.openGitDiffTab)
  const setGitDiffLoading = useUIStore((state) => state.setGitDiffLoading)
  const setGitDiffSummary = useUIStore((state) => state.setGitDiffSummary)
  const setGitDiffError = useUIStore((state) => state.setGitDiffError)
  const setGitDiffTabLoading = useUIStore((state) => state.setGitDiffTabLoading)
  const setGitDiffTabContent = useUIStore((state) => state.setGitDiffTabContent)
  const setGitDiffTabError = useUIStore((state) => state.setGitDiffTabError)
  const toggleChangesPanel = useUIStore((state) => state.toggleChangesPanel)

  const activeTab = useMemo(
    () => previewTabs.find((tab) => tab.id === activePreviewTabId) || previewTabs[0],
    [activePreviewTabId, previewTabs],
  )
  const hasOpenGitDiffFileTab = useMemo(
    () => previewTabs.some((tab) => tab.kind === 'git-diff-file'),
    [previewTabs],
  )
  const fileChangesSignature = useMemo(
    () => fileChanges.map((change) => `${change.path}:${change.status}:${change.additions}:${change.deletions}`).join('|'),
    [fileChanges],
  )

  const loadGitDiffSummary = useCallback(async () => {
    if (!gatewayAPI) return
    setGitDiffLoading(true)
    try {
      const result = await gatewayAPI.listGitDiffFiles()
      setGitDiffSummary(result.payload)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load Git diff'
      setGitDiffError(message)
    }
  }, [gatewayAPI, setGitDiffError, setGitDiffLoading, setGitDiffSummary])

  const openGitDiffFile = useCallback(async (path: string) => {
    if (!gatewayAPI) return

    const currentTab = useUIStore.getState().previewTabs.find(
      (tab): tab is GitDiffFilePreviewTab => tab.kind === 'git-diff-file' && tab.path === path,
    )
    const { id, created } = openGitDiffTab(path)
    if (currentTab && !created) {
      if (currentTab.loading) return
      if (currentTab.loaded && !currentTab.error) return
      setGitDiffTabLoading(id)
    }

    try {
      const result = await gatewayAPI.readGitDiffFile({ path })
      setGitDiffTabContent(id, result.payload)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to read Git diff preview'
      setGitDiffTabError(id, message)
    }
  }, [gatewayAPI, openGitDiffTab, setGitDiffTabContent, setGitDiffTabError, setGitDiffTabLoading])

  useEffect(() => {
    if (!gatewayAPI) return
    void loadGitDiffSummary()
  }, [gatewayAPI, currentWorkspaceHash, loadGitDiffSummary])

  useEffect(() => {
    if (activePreviewTabId !== GIT_DIFF_PREVIEW_TAB_ID && !hasOpenGitDiffFileTab) return
    void loadGitDiffSummary()
  }, [activePreviewTabId, hasOpenGitDiffFileTab, gitDiffRefreshToken, loadGitDiffSummary])

  useEffect(() => {
    if (activePreviewTabId !== GIT_DIFF_PREVIEW_TAB_ID && !hasOpenGitDiffFileTab) return
    if (!fileChangesSignature) return
    const timer = window.setTimeout(() => {
      void loadGitDiffSummary()
    }, 300)
    return () => window.clearTimeout(timer)
  }, [activePreviewTabId, fileChangesSignature, hasOpenGitDiffFileTab, loadGitDiffSummary])

  return (
    <div style={styles.container}>
      <div style={styles.dockHeader}>
        <PreviewTabStrip
          tabs={previewTabs}
          activeTabId={activeTab?.id || ''}
          fileChanges={fileChanges}
          onActivate={activatePreviewTab}
          onClose={closePreviewTab}
        />
        <button style={styles.closeBtn} onClick={toggleChangesPanel} title="关闭右侧预览">
          <PanelRightClose size={16} />
        </button>
      </div>

      <div
        id={`preview-panel-${activeTab?.id || CHANGES_PREVIEW_TAB_ID}`}
        role="tabpanel"
        aria-labelledby={`preview-tab-${activeTab?.id || CHANGES_PREVIEW_TAB_ID}`}
        data-testid="file-change-panel-body"
        style={styles.panelBody}
      >
        {activeTab?.kind === 'file' && <FilePreviewView tab={activeTab} />}
        {activeTab?.kind === 'git-diff-file' && <GitDiffFileView tab={activeTab} />}
        {activeTab?.kind === 'git-diff' && (
          <GitDiffView
            summary={gitDiffSummary}
            loading={gitDiffLoading}
            error={gitDiffError}
            onRefresh={loadGitDiffSummary}
            onOpenFile={openGitDiffFile}
          />
        )}
        {(!activeTab || activeTab.kind === 'changes') && <ChangesView />}
      </div>
    </div>
  )
}

const styles: Record<string, CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    minHeight: 0,
    overflow: 'hidden',
    background: 'var(--bg-secondary)',
  },
  dockHeader: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 8,
    padding: '8px 10px 10px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
  },
  tabStack: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    minWidth: 0,
    flex: 1,
  },
  switcherRow: {
    display: 'flex',
    alignItems: 'center',
    minWidth: 0,
  },
  switcherRail: {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    minWidth: 0,
    padding: 2,
    borderRadius: 'var(--radius-md)',
    background: 'rgba(148, 163, 184, 0.08)',
  },
  switcherButton: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: 28,
    padding: '0 12px',
    border: 'none',
    borderRadius: 'var(--radius-sm)',
    background: 'transparent',
    color: 'var(--text-secondary)',
    minWidth: 0,
    cursor: 'pointer',
    transition: 'all var(--duration-fast) var(--ease-out)',
  },
  switcherButtonActive: {
    background: 'var(--bg-active)',
    color: 'var(--text-primary)',
    boxShadow: 'inset 0 -1px 0 var(--accent)',
  },
  switcherButtonContext: {
    background: 'rgba(148, 163, 184, 0.08)',
    color: 'var(--text-primary)',
    boxShadow: 'inset 0 -1px 0 var(--border-primary)',
  },
  switcherLabel: {
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
    fontWeight: 500,
  },
  chipRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    overflowX: 'auto',
    paddingBottom: 2,
  },
  chipItem: {
    display: 'flex',
    alignItems: 'center',
    minWidth: 0,
    maxWidth: 220,
    height: 26,
    paddingLeft: 2,
    borderRadius: 'var(--radius-full)',
    border: '1px solid transparent',
    background: 'rgba(148, 163, 184, 0.08)',
    color: 'var(--text-secondary)',
    flexShrink: 0,
  },
  chipItemActive: {
    background: 'var(--bg-active)',
    borderColor: 'var(--border-primary)',
    color: 'var(--text-primary)',
  },
  chipButton: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    minWidth: 0,
    maxWidth: 188,
    height: '100%',
    padding: '0 10px',
    border: 'none',
    background: 'transparent',
    color: 'inherit',
    cursor: 'pointer',
    textAlign: 'left',
  },
  chipDot: {
    width: 6,
    height: 6,
    borderRadius: 'var(--radius-full)',
    flexShrink: 0,
  },
  chipLabel: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontFamily: 'var(--font-ui)',
    fontSize: 11,
    fontWeight: 500,
  },
  chipCloseButton: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 18,
    height: 18,
    marginRight: 4,
    border: 'none',
    borderRadius: 'var(--radius-full)',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    flexShrink: 0,
  },
  chipCloseButtonActive: {
    color: 'var(--text-secondary)',
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    cursor: 'pointer',
    flexShrink: 0,
  },
  panelBody: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    padding: '8px 10px 0',
  },
  viewContainer: {
    display: 'flex',
    flexDirection: 'column',
    flex: 1,
    minHeight: 0,
    overflow: 'hidden',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-md)',
    boxShadow: 'var(--shadow-surface)',
  },
  viewHeader: {
    padding: '8px 10px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
  },
  viewTitle: {
    display: 'block',
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-ui)',
  },
  titleRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    justifyContent: 'space-between',
  },
  summaryRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginTop: 8,
    color: 'var(--text-tertiary)',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
    flexWrap: 'wrap',
  },
  summaryWrap: {
    display: 'flex',
    gap: 10,
    marginTop: 8,
    flexWrap: 'wrap',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  summaryDivider: {
    width: 1,
    height: 10,
    background: 'var(--border-primary)',
  },
  scrollArea: {
    flex: 1,
    minHeight: 0,
    overflow: 'auto',
    padding: 10,
  },
  contentStack: {
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  emptyState: {
    padding: 16,
    borderRadius: 10,
    border: '1px dashed var(--border-primary)',
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
  },
  changeCard: {
    border: '1px solid var(--border-primary)',
    borderRadius: 10,
    overflow: 'hidden',
    background: 'var(--bg-primary)',
    flexShrink: 0,
  },
  changeHeader: {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 12px',
    border: 'none',
    background: 'transparent',
    color: 'inherit',
    cursor: 'pointer',
    textAlign: 'left',
  },
  chevron: {
    display: 'inline-flex',
    color: 'var(--text-tertiary)',
    transition: 'transform 0.15s ease',
    flexShrink: 0,
  },
  fileIcon: {
    display: 'inline-flex',
    color: 'var(--text-secondary)',
    flexShrink: 0,
  },
  changeMain: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    minWidth: 0,
    flex: 1,
  },
  pathText: {
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    color: 'var(--text-primary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  changeStats: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    flexShrink: 0,
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  statusPill: {
    display: 'inline-flex',
    alignItems: 'center',
    padding: '2px 6px',
    borderRadius: 999,
    fontSize: 11,
    fontWeight: 600,
    fontFamily: 'var(--font-ui)',
  },
  additions: {
    color: 'var(--success)',
    fontFamily: 'var(--font-mono)',
  },
  deletions: {
    color: 'var(--error)',
    fontFamily: 'var(--font-mono)',
  },
  expandedArea: {
    display: 'flex',
    flexDirection: 'column',
    borderTop: '1px solid var(--border-primary)',
    background: 'rgba(148, 163, 184, 0.03)',
  },
  actionsRow: {
    display: 'flex',
    justifyContent: 'flex-end',
    padding: '8px 10px 0',
  },
  actionBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 6,
    padding: '6px 10px',
    border: '1px solid var(--border-primary)',
    borderRadius: 8,
    background: 'var(--bg-primary)',
    cursor: 'pointer',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
  diffScroller: {
    padding: 10,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  hunkBlock: {
    border: '1px solid rgba(148, 163, 184, 0.15)',
    borderRadius: 8,
    overflow: 'hidden',
  },
  hunkScroller: {
    overflowX: 'auto',
    overflowY: 'hidden',
    maxWidth: '100%',
  },
  diffLine: {
    display: 'grid',
    gridTemplateColumns: '16px minmax(0, 1fr)',
    gap: 8,
    padding: '4px 8px',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
    whiteSpace: 'pre',
    width: 'max-content',
    minWidth: '100%',
  },
  diffPrefix: {
    color: 'inherit',
  },
  diffText: {
    display: 'block',
  },
  emptyDiff: {
    padding: 12,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-ui)',
    fontSize: 12,
  },
  previewEmptyState: {
    flex: 1,
    minHeight: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    color: 'var(--text-tertiary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    padding: 16,
    textAlign: 'center',
  },
  previewPath: {
    marginTop: 0,
    color: 'var(--text-secondary)',
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
    minWidth: 0,
    lineHeight: '18px',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  previewMeta: {
    marginTop: 6,
    color: 'var(--text-tertiary)',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
  },
  iconBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 24,
    height: 24,
    borderRadius: 8,
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    flexShrink: 0,
  },
  gitDiffItem: {
    display: 'flex',
    alignItems: 'flex-start',
    gap: 10,
    width: '100%',
    padding: '10px 12px',
    borderRadius: 10,
    border: '1px solid var(--border-primary)',
    background: 'var(--bg-primary)',
    color: 'inherit',
    cursor: 'pointer',
    textAlign: 'left',
    flexShrink: 0,
  },
  gitDiffBadge: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 20,
    height: 20,
    borderRadius: 999,
    fontSize: 11,
    fontWeight: 700,
    fontFamily: 'var(--font-ui)',
    flexShrink: 0,
  },
  gitDiffItemMain: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
    minWidth: 0,
    flex: 1,
  },
  gitDiffOldPath: {
    color: 'var(--text-tertiary)',
    fontSize: 11,
    fontFamily: 'var(--font-ui)',
    wordBreak: 'break-all',
  },
}
