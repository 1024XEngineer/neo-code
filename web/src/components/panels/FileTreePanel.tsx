import { useState, useEffect, useCallback } from 'react'
import { useUIStore } from '@/stores/useUIStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import { type FileEntry } from '@/api/protocol'
import {
  Folder,
  FolderOpen,
  File,
  FileCode,
  FileText,
  FileJson,
  ChevronRight,
  PanelRightClose,
  Loader2,
} from 'lucide-react'

const fileIconMap: Record<string, React.ReactNode> = {
  js: <FileCode size={14} />,
  jsx: <FileCode size={14} />,
  ts: <FileCode size={14} />,
  tsx: <FileCode size={14} />,
  json: <FileJson size={14} />,
  md: <FileText size={14} />,
}

function getFileIcon(filename: string) {
  const ext = filename.split('.').pop() || ''
  return fileIconMap[ext] || <File size={14} />
}

interface FileTreeNode {
  entry: FileEntry
  children?: FileTreeNode[]
}

function buildFileTree(entries: FileEntry[]): FileTreeNode[] {
  const rootNodes: FileTreeNode[] = []
  const dirMap = new Map<string, FileTreeNode>()

  // 先创建所有目录节点
  for (const entry of entries) {
    if (entry.is_dir) {
      const node: FileTreeNode = { entry, children: [] }
      dirMap.set(entry.path, node)
    }
  }

  // 再分配所有节点到父目录
  for (const entry of entries) {
    const parentPath = entry.path.split('/').slice(0, -1).join('/')
    if (parentPath && dirMap.has(parentPath)) {
      const parent = dirMap.get(parentPath)!
      if (entry.is_dir) {
        parent.children!.push(dirMap.get(entry.path)!)
      } else {
        parent.children!.push({ entry })
      }
    } else if (!parentPath) {
      // 根级别节点
      if (entry.is_dir) {
        rootNodes.push(dirMap.get(entry.path)!)
      } else {
        rootNodes.push({ entry })
      }
    } else {
      // 父目录缺失：作为根节点挂载（容错处理，避免节点丢失）
      if (entry.is_dir) {
        rootNodes.push(dirMap.get(entry.path)!)
      } else {
        rootNodes.push({ entry })
      }
    }
  }

  return rootNodes
}

interface FileTreeItemProps {
  node: FileTreeNode
  depth?: number
  dirCache: Map<string, FileTreeNode[]>
  onLoadDir: (path: string) => Promise<void>
}

function FileTreeItem({ node, depth = 0, dirCache, onLoadDir }: FileTreeItemProps) {
  const [expanded, setExpanded] = useState(false)
  const [localLoading, setLocalLoading] = useState(false)
  const isFolder = node.entry.is_dir

  const cachedChildren = dirCache.get(node.entry.path)
  const children = cachedChildren !== undefined ? cachedChildren : node.children
  const isLoaded = cachedChildren !== undefined

  const handleClick = async () => {
    if (!isFolder) return
    if (!isLoaded) {
      setLocalLoading(true)
      try {
        await onLoadDir(node.entry.path)
        setExpanded(true)
      } finally {
        setLocalLoading(false)
      }
    } else {
      setExpanded(!expanded)
    }
  }

  return (
    <div>
      <button
        style={{
          ...styles.treeItem,
          paddingLeft: 8 + depth * 14,
        }}
        onClick={handleClick}
      >
        {isFolder && (
          <span
            style={{
              ...styles.chevron,
              transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
            }}
          >
            {localLoading ? (
              <Loader2 size={12} style={{ animation: 'spin 1s linear infinite' }} />
            ) : (
              <ChevronRight size={12} />
            )}
          </span>
        )}
        <span style={styles.treeIcon}>
          {isFolder
            ? expanded
              ? <FolderOpen size={14} />
              : <Folder size={14} />
            : getFileIcon(node.entry.name)}
        </span>
        <span style={styles.treeName}>{node.entry.name}</span>
      </button>
      {isFolder && expanded && (
        children && children.length > 0 ? (
          children.map((child) => (
            <FileTreeItem
              key={child.entry.path}
              node={child}
              depth={depth + 1}
              dirCache={dirCache}
              onLoadDir={onLoadDir}
            />
          ))
        ) : isLoaded ? (
          <div
            style={{
              paddingLeft: 8 + (depth + 1) * 14,
              paddingTop: 2,
              paddingBottom: 2,
              fontSize: 11,
              color: 'var(--text-tertiary)',
              fontFamily: 'var(--font-ui)',
            }}
          >
            空文件夹
          </div>
        ) : null
      )}
    </div>
  )
}

export default function FileTreePanel() {
  const toggleFileTreePanel = useUIStore((s) => s.toggleFileTreePanel)
  const gatewayAPI = useGatewayAPI()
  const currentWorkspaceHash = useWorkspaceStore((s) => s.currentWorkspaceHash)
  const workspaces = useWorkspaceStore((s) => s.workspaces)
  const currentWorkspace = workspaces.find((w) => w.hash === currentWorkspaceHash)
  const [rootNodes, setRootNodes] = useState<FileTreeNode[]>([])
  const [dirCache, setDirCache] = useState<Map<string, FileTreeNode[]>>(new Map())
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [currentPath, setCurrentPath] = useState('')

  const loadDir = useCallback(async (path: string) => {
    if (!gatewayAPI) return
    try {
      const result = await gatewayAPI.listFiles({ path })
      const nodes = buildFileTree(result.payload.files)
      setDirCache((prev) => {
        const next = new Map(prev)
        next.set(path, nodes)
        return next
      })
    } catch (err) {
      console.error(`loadDir(${path}) failed:`, err)
      throw err
    }
  }, [gatewayAPI])

  const loadRoot = useCallback(async () => {
    if (!gatewayAPI) return
    setLoading(true)
    setError('')
    try {
      const result = await gatewayAPI.listFiles({ path: '' })
      setRootNodes(buildFileTree(result.payload.files))
      setCurrentPath('')
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load file list'
      setError(msg)
      console.error('listFiles failed:', err)
    } finally {
      setLoading(false)
    }
  }, [gatewayAPI])

  useEffect(() => {
    loadRoot()
  }, [loadRoot])

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <div style={styles.headerTop}>
          <span style={styles.headerTitle}>工作区</span>
          <button
            style={styles.closeBtn}
            onClick={toggleFileTreePanel}
            title="关闭文件目录"
          >
            <PanelRightClose size={16} />
          </button>
        </div>
        <div style={styles.headerPath}>{currentPath || currentWorkspace?.name || currentWorkspace?.path || '.'}</div>
      </div>

      <div style={styles.scrollArea}>
        {loading && (
          <div style={styles.emptyState}>
            <Loader2 size={16} style={{ animation: 'spin 1s linear infinite' }} />
            <span style={styles.emptyText}>加载中...</span>
          </div>
        )}
        {!loading && error && (
          <div style={styles.emptyState}>
            <span style={{ ...styles.emptyText, color: 'var(--error)' }}>
              加载失败: {error}
            </span>
          </div>
        )}
        {!loading && !error && rootNodes.length === 0 && (
          <div style={styles.emptyState}>
            <span style={styles.emptyText}>工作区为空</span>
          </div>
        )}
        {!loading &&
          !error &&
          rootNodes.map((node) => (
            <FileTreeItem
              key={node.entry.path}
              node={node}
              depth={0}
              dirCache={dirCache}
              onLoadDir={loadDir}
            />
          ))}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    background: 'var(--bg-secondary)',
  },
  header: {
    padding: '12px 14px',
    borderBottom: '1px solid var(--border-primary)',
    flexShrink: 0,
  },
  headerTop: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 4,
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  headerPath: {
    fontSize: 11,
    color: 'var(--text-tertiary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
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
  },
  scrollArea: {
    flex: 1,
    overflowY: 'auto',
    padding: '6px 4px',
  },
  treeItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    width: '100%',
    padding: '4px 8px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-secondary)',
    fontSize: 12,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    textAlign: 'left',
    transition: 'all 0.15s',
  },
  chevron: {
    display: 'flex',
    transition: 'transform 0.2s',
    color: 'var(--text-tertiary)',
    width: 14,
    flexShrink: 0,
  },
  treeIcon: {
    display: 'flex',
    flexShrink: 0,
    color: 'var(--text-tertiary)',
  },
  treeName: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
  },
  emptyState: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    padding: '20px 8px',
    color: 'var(--text-tertiary)',
  },
  emptyText: {
    fontSize: 12,
    fontFamily: 'var(--font-ui)',
  },
}
