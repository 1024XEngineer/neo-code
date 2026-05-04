import { useUIStore } from '@/stores/useUIStore'
import TodoTab from './insight/TodoTab'
import VerificationHistoryTab from './insight/VerificationHistoryTab'
import CheckpointTab from './insight/CheckpointTab'
import { PanelRightClose, ListChecks, ShieldCheck, GitCommitHorizontal } from 'lucide-react'

const TABS: { key: 'todo' | 'verification' | 'checkpoint'; label: string; icon: React.ReactNode }[] = [
  { key: 'todo', label: 'Todo', icon: <ListChecks size={13} /> },
  { key: 'verification', label: '验证', icon: <ShieldCheck size={13} /> },
  { key: 'checkpoint', label: '检查点', icon: <GitCommitHorizontal size={13} /> },
]

/** 右侧 Insight 面板主壳 —— tab 路由到 Todo / Verification / Checkpoint */
export default function InsightPanel() {
  const activeTab = useUIStore((s) => s.insightActiveTab)
  const setActiveTab = useUIStore((s) => s.setInsightActiveTab)
  const toggleInsightPanel = useUIStore((s) => s.toggleInsightPanel)

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <div style={styles.headerTop}>
          <span style={styles.headerTitle}>Insight</span>
          <button style={styles.closeBtn} onClick={toggleInsightPanel} title="关闭 Insight">
            <PanelRightClose size={16} />
          </button>
        </div>
        <div style={styles.tabRow}>
          {TABS.map((tab) => (
            <button
              key={tab.key}
              style={{
                ...styles.tabBtn,
                ...(activeTab === tab.key ? styles.tabBtnActive : {}),
              }}
              onClick={() => setActiveTab(tab.key)}
            >
              {tab.icon}
              <span>{tab.label}</span>
            </button>
          ))}
        </div>
      </div>

      <div style={styles.scrollArea}>
        {activeTab === 'todo' && <TodoTab />}
        {activeTab === 'verification' && <VerificationHistoryTab />}
        {activeTab === 'checkpoint' && <CheckpointTab />}
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
    marginBottom: 8,
  },
  headerTitle: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
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
  tabRow: {
    display: 'flex',
    gap: 2,
  },
  tabBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 5,
    padding: '4px 10px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    fontSize: 11,
    cursor: 'pointer',
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
  },
  tabBtnActive: {
    background: 'var(--bg-active)',
    color: 'var(--text-primary)',
    fontWeight: 500,
  },
  scrollArea: {
    flex: 1,
    overflowY: 'auto',
    padding: 8,
  },
  placeholder: {
    height: '100%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-tertiary)',
    fontSize: 12,
  },
}
