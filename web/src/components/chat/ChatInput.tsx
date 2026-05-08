import { useState, useRef, useEffect, useCallback } from 'react'
import { useChatStore, createUserMessage } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore, isValidSessionId } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { formatTokenCount } from '@/utils/format'
import { useGatewayAPI } from '@/context/RuntimeProvider'
import {
  builtinSlashCommands,
  matchSlashCommands,
  parseSlashCommand,
  isSlashCommand,
  type AnySlashCommand,
  type SkillSlashCommand,
  isSkillCommand,
} from '@/utils/slashCommands'
import SlashCommandMenu from './SlashCommandMenu'
import SkillPicker from './SkillPicker'
import ModelSelector from './ModelSelector'
import { Send, Square } from 'lucide-react'

export default function ChatInput() {
  const gatewayAPI = useGatewayAPI()
  const text = useComposerStore((s) => s.composerText)
  const setText = useComposerStore((s) => s.setComposerText)
  const [rows, setRows] = useState(1)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const runCancelledRef = useRef(false)
  const composingRef = useRef(false)
  const isGenerating = useChatStore((s) => s.isGenerating)
  const addMessage = useChatStore((s) => s.addMessage)
  const addSystemMessage = useChatStore((s) => s.addSystemMessage)
  const setGenerating = useChatStore((s) => s.setGenerating)
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const agentMode = useChatStore((s) => s.agentMode)
  const setAgentMode = useChatStore((s) => s.setAgentMode)
  const permissionMode = useChatStore((s) => s.permissionMode)
  const setPermissionMode = useChatStore((s) => s.setPermissionMode)

  const [showSlashMenu, setShowSlashMenu] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [matchedCommands, setMatchedCommands] = useState<AnySlashCommand[]>([])
  const [availableSkillCommands, setAvailableSkillCommands] = useState<SkillSlashCommand[]>([])
  const [showSkillPicker, setShowSkillPicker] = useState(false)

  useEffect(() => {
    if (!showSlashMenu || !gatewayAPI) return
    gatewayAPI.listAvailableSkills(sessionId || undefined).then((result) => {
      const skills = result.payload?.skills || []
      const skillCommands: SkillSlashCommand[] = skills.map((s) => ({
        id: `skill-${s.descriptor.id}`,
        usage: `/${s.descriptor.id}`,
        description: s.descriptor.description || '技能',
        hasArgument: false, isSkill: true,
        skillId: s.descriptor.id, active: s.active,
      }))
      setAvailableSkillCommands(skillCommands)
    }).catch(() => { setAvailableSkillCommands([]) })
  }, [showSlashMenu, gatewayAPI, sessionId])

  useEffect(() => {
    if (!isSlashCommand(text)) { setShowSlashMenu(false); return }
    const allCommands: AnySlashCommand[] = [...builtinSlashCommands, ...availableSkillCommands]
    const matched = matchSlashCommands(text, allCommands)
    if (matched.length > 0) { setMatchedCommands(matched); setShowSlashMenu(true); setSelectedIndex(0) }
    else { setShowSlashMenu(false) }
  }, [text, availableSkillCommands])

  useEffect(() => {
    const lines = text.split('\n').length
    setRows(Math.min(Math.max(lines, 1), 8))
  }, [text])

  const executeSlashCommand = useCallback(async (input: string): Promise<boolean> => {
    const parsed = parseSlashCommand(input)
    if (!parsed) return false
    const { command, argument } = parsed
    const currentSessionId = sessionId
    const api = gatewayAPI
    if (!api) { useUIStore.getState().showToast('Gateway not connected', 'error'); return true }

    switch (command) {
      case '/help': {
        const allUsages = [...builtinSlashCommands.map((c) => c.usage), '/<skill-id>']
        const maxLen = Math.max(...allUsages.map((u) => u.length))
        const helpLines = [
          '可用命令：',
          ...builtinSlashCommands.map((cmd) => {
            const pad = ' '.repeat(maxLen - cmd.usage.length)
            return `  ${cmd.usage}${pad}  — ${cmd.description}`
          }),
          `  /${'<skill-id>'.padEnd(maxLen - 1)}  — 激活/停用技能`,
        ]
        addSystemMessage(helpLines.join('\n'))
        return true
      }
      case '/compact': {
        if (!isValidSessionId(currentSessionId)) { useUIStore.getState().showToast('Send a message first to start a session', 'error'); return true }
        try { await api.compact(currentSessionId, '') } catch (err) { console.error('Compact failed:', err); useUIStore.getState().showToast('Compaction failed', 'error') }
        return true
      }
      case '/memo': {
        if (!isValidSessionId(currentSessionId)) { useUIStore.getState().showToast('Send a message first to start a session', 'error'); return true }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_list', {})
          addSystemMessage((result as any)?.payload?.content || 'Memo query complete')
        } catch (err) { console.error('Memo list failed:', err); useUIStore.getState().showToast('Failed to query memo', 'error') }
        return true
      }
      case '/remember': {
        if (!argument) { useUIStore.getState().showToast('Usage: /remember <content>', 'error'); return true }
        if (!isValidSessionId(currentSessionId)) { useUIStore.getState().showToast('Send a message first to start a session', 'error'); return true }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remember', { type: 'user', title: argument, content: argument })
          addSystemMessage((result as any)?.payload?.content || 'Memo saved')
        } catch (err) { console.error('Remember failed:', err); useUIStore.getState().showToast('Failed to save memo', 'error') }
        return true
      }
      case '/forget': {
        if (!argument) { useUIStore.getState().showToast('Usage: /forget <keyword>', 'error'); return true }
        if (!isValidSessionId(currentSessionId)) { useUIStore.getState().showToast('Send a message first to start a session', 'error'); return true }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remove', { keyword: argument, scope: 'all' })
          addSystemMessage((result as any)?.payload?.content || 'Memo deleted')
        } catch (err) { console.error('Forget failed:', err); useUIStore.getState().showToast('Failed to delete memo', 'error') }
        return true
      }
      case '/skills': { setShowSkillPicker(true); return true }
      default: {
        if (isGenerating) { useUIStore.getState().showToast('Cannot toggle skill while generating', 'info'); return true }
        const skillCmd = availableSkillCommands.find((s) => s.usage === command)
        if (skillCmd && isValidSessionId(currentSessionId)) {
          try {
            if (skillCmd.active) await api.deactivateSessionSkill(currentSessionId, skillCmd.skillId)
            else await api.activateSessionSkill(currentSessionId, skillCmd.skillId)
            setAvailableSkillCommands((prev) => prev.map((item) => item.skillId === skillCmd.skillId ? { ...item, active: !item.active } : item))
          } catch (err) { console.error('Skill toggle failed:', err); useUIStore.getState().showToast('Skill operation failed', 'error') }
          return true
        }
        return false
      }
    }
  }, [gatewayAPI, sessionId, addSystemMessage, availableSkillCommands, isGenerating])

  async function handleSubmit() {
    const input = text.trim()
    if (!input) return
    if (isGenerating) {
      if (isSlashCommand(input)) useUIStore.getState().showToast('Cannot run commands while generating', 'info')
      return
    }
    if (isSlashCommand(input)) {
      setText(''); setShowSlashMenu(false)
      const handled = await executeSlashCommand(input)
      if (handled) return
    }
    setText('')
    const userMsg = createUserMessage(input)
    addMessage(userMsg)
    useRuntimeInsightStore.getState().setTodoSnapshot({
      items: [], summary: { total: 0, required_total: 0, required_completed: 0, required_failed: 0, required_open: 0 },
    })
    setGenerating(true)
    runCancelledRef.current = false
    try {
      if (!gatewayAPI) return
      const isNewSession = !isValidSessionId(sessionId)
      const ack = await gatewayAPI.run({ session_id: isNewSession ? undefined : sessionId, new_session: isNewSession ? true : undefined, input_text: input, mode: agentMode })
      if (!runCancelledRef.current) {
        const gwStore = useGatewayStore.getState()
        const sessStore = useSessionStore.getState()
        if (ack.run_id) gwStore.setCurrentRunId(ack.run_id)
        if (ack.session_id) { sessStore.setCurrentSessionId(ack.session_id); gatewayAPI?.bindStream({ session_id: ack.session_id, channel: 'all' }).catch(() => {}) }
      }
    } catch (err) {
      if (!runCancelledRef.current) {
        setGenerating(false); useChatStore.getState().removeMessage(userMsg.id)
        console.error('Run failed:', err); useUIStore.getState().showToast('Failed to send message', 'error')
      }
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (composingRef.current) return
    if (!showSlashMenu) {
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSubmit() }
      return
    }
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); setSelectedIndex((prev) => (prev + 1) % matchedCommands.length); return
      case 'ArrowUp': e.preventDefault(); setSelectedIndex((prev) => (prev - 1 + matchedCommands.length) % matchedCommands.length); return
      case 'Enter': e.preventDefault(); const cmd = matchedCommands[selectedIndex]; if (cmd) handleSelectCommand(cmd); return
      case 'Escape': e.preventDefault(); setShowSlashMenu(false); return
      case 'Tab': e.preventDefault(); const c = matchedCommands[selectedIndex]; if (c) { setText(c.usage + ' '); textareaRef.current?.focus() }; return
    }
  }

  function handleSelectCommand(cmd: AnySlashCommand) {
    if (isSkillCommand(cmd)) { setText(cmd.usage); setShowSlashMenu(false); executeSlashCommand(cmd.usage); return }
    if (cmd.hasArgument) { setText(cmd.usage + ' '); setShowSlashMenu(false); textareaRef.current?.focus() }
    else { setText(''); setShowSlashMenu(false); executeSlashCommand(cmd.usage) }
  }

  async function handleCancel() {
    runCancelledRef.current = true
    const runId = useGatewayStore.getState().currentRunId
    if (runId && gatewayAPI) { try { await gatewayAPI.cancel({ run_id: runId }) } catch (err) { console.error('Cancel failed:', err) } }
    useChatStore.getState().resetGeneratingState()
  }

  const isEmpty = !text.trim()

  return (
    <>
      {showSkillPicker && gatewayAPI && (
        <SkillPicker gatewayAPI={gatewayAPI} sessionId={sessionId || ''} onClose={() => setShowSkillPicker(false)} />
      )}
      <div className="input-area">
        {showSlashMenu && matchedCommands.length > 0 && (
          <div style={{ maxWidth: 740, margin: '0 auto', position: 'relative' }}>
            <div className="slash-menu">
              <SlashCommandMenu
                commands={matchedCommands}
                selectedIndex={selectedIndex}
                onSelect={handleSelectCommand}
                onHover={setSelectedIndex}
                query={text.trim()}
              />
            </div>
          </div>
        )}
        <div className="input-box">
          <textarea
            ref={textareaRef}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            onCompositionStart={() => { composingRef.current = true }}
            onCompositionEnd={() => { composingRef.current = false }}
            placeholder="输入指令或问题...  Enter 发送，Shift+Enter 换行"
            rows={rows}
          />
          <div className="input-toolbar">
            <div className="input-toolbar-left" style={{ flex: 1 }}>
              <button
                className={`input-mode-toggle ${agentMode === 'plan' ? 'plan' : ''}`}
                title={agentMode === 'plan' ? '规划模式' : '构建模式'}
                onClick={() => { if (!isGenerating) setAgentMode(agentMode === 'plan' ? 'build' : 'plan') }}
              >
                {agentMode === 'plan' ? 'Plan' : 'Build'}
              </button>
              {agentMode === 'build' && (
                <div
                  aria-label="Build permission mode"
                  role="group"
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 2,
                    marginLeft: 6,
                    padding: 2,
                    borderRadius: 'var(--radius-sm)',
                    background: 'var(--bg-hover)',
                    opacity: isGenerating ? 0.5 : 1,
                  }}
                >
                  <button
                    type="button"
                    aria-pressed={permissionMode === 'default'}
                    style={permissionModeButtonStyle(permissionMode === 'default')}
                    onClick={() => {
                      if (isGenerating) return
                      setPermissionMode('default')
                    }}
                  >
                    default
                  </button>
                  <button
                    type="button"
                    aria-pressed={permissionMode === 'bypass'}
                    style={permissionModeButtonStyle(permissionMode === 'bypass')}
                    onClick={() => {
                      if (isGenerating) return
                      setPermissionMode('bypass')
                    }}
                  >
                    bypass
                  </button>
                </div>
              )}
              <BudgetTokenStrip />
            </div>
            <ModelSelector />
            <button
              className={`input-send-btn ${isEmpty && !isGenerating ? '' : ''} ${isGenerating ? 'stop' : ''}`}
              onClick={isGenerating ? handleCancel : handleSubmit}
              disabled={isEmpty && !isGenerating}
              title={isGenerating ? '停止生成' : '发送'}
            >
              {isGenerating ? <Square size={16} /> : <Send size={16} />}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}

function BudgetTokenStrip() {
  const budgetChecked = useRuntimeInsightStore((s) => s.budgetChecked)
  const budgetUsageRatio = useRuntimeInsightStore((s) => s.budgetUsageRatio)
  const budgetEstimateFailed = useRuntimeInsightStore((s) => s.budgetEstimateFailed)
  const ledgerReconciled = useRuntimeInsightStore((s) => s.ledgerReconciled)
  const tokenUsage = useChatStore((s) => s.tokenUsage)
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const totalTokens = tokenUsage ? tokenUsage.input_tokens + tokenUsage.output_tokens : 0
  const ratio = budgetUsageRatio ?? 0
  const pct = Math.min(Math.round(ratio * 100), 100)

  // SVG ring: radius 8, circumference ~50
  const r = 7
  const circ = 2 * Math.PI * r
  const dash = (ratio * circ).toFixed(1)
  let ringColor = 'var(--text-tertiary)'
  if (budgetEstimateFailed || ratio > 0.8) ringColor = 'var(--error)'
  else if (ratio > 0.6) ringColor = 'var(--warning)'

  // Click outside to close
  useEffect(() => {
    if (!open) return
    function click(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', click)
    return () => document.removeEventListener('mousedown', click)
  }, [open])

  if (!budgetChecked && !totalTokens) return null

  return (
    <div ref={ref} style={{ position: 'relative' }}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 4, cursor: 'pointer', padding: '2px 4px', borderRadius: 'var(--radius-sm)' }}>
        <svg width={18} height={18} style={{ display: 'block' }}>
          <circle cx={9} cy={9} r={r} fill="none" stroke="var(--bg-active)" strokeWidth="2" />
          {budgetChecked && (
            <circle cx={9} cy={9} r={r} fill="none" stroke={ringColor} strokeWidth="2"
              strokeDasharray={`${dash} ${circ}`} strokeLinecap="round"
              transform="rotate(-90 9 9)" style={{ transition: 'stroke-dasharray 0.3s var(--ease-out), stroke 0.3s var(--ease-out)' }}
            />
          )}
        </svg>
        {totalTokens > 0 && (
          <span style={{ fontSize: 10, color: 'var(--text-tertiary)', fontFamily: 'var(--font-mono)' }}>
            {formatTokenCount(totalTokens)}
          </span>
        )}
      </div>

      {open && (
        <div className="budget-popover" style={{ bottom: 28, right: -8 }}>
          <div className="budget-popover-title">用量明细</div>
          {budgetEstimateFailed ? (
            <div style={{ color: 'var(--error)', fontSize: 11 }}>{budgetEstimateFailed.message}</div>
          ) : budgetChecked ? (
            <>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Budget</span>
                <span className="budget-popover-value">{formatTokenCount(budgetChecked.prompt_budget)}</span>
              </div>
              {budgetChecked.context_window && (
                <div className="budget-popover-row">
                  <span className="budget-popover-label">Context</span>
                  <span className="budget-popover-value">{formatTokenCount(budgetChecked.context_window)}</span>
                </div>
              )}
              <div className="budget-popover-row">
                <span className="budget-popover-label">已用</span>
                <span className="budget-popover-value">{formatTokenCount(budgetChecked.estimated_input_tokens)} ({pct}%)</span>
              </div>
              {totalTokens > 0 && (
                <div className="budget-popover-row">
                  <span className="budget-popover-label">本轮 Tokens</span>
                  <span className="budget-popover-value">{formatTokenCount(totalTokens)}</span>
                </div>
              )}
            </>
          ) : null}
          {ledgerReconciled && (
            <>
              <div style={{ height: 1, background: 'var(--bg-active)', margin: '6px 0' }} />
              <div className="budget-popover-row">
                <span className="budget-popover-label">Input</span>
                <span className="budget-popover-value">{formatTokenCount(ledgerReconciled.input_tokens)}</span>
              </div>
              <div className="budget-popover-row">
                <span className="budget-popover-label">Output</span>
                <span className="budget-popover-value">{formatTokenCount(ledgerReconciled.output_tokens)}</span>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function permissionModeButtonStyle(active: boolean): React.CSSProperties {
  return {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 62,
    height: 28,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: active ? 'var(--bg-elevated)' : 'transparent',
    color: active ? 'var(--text-primary)' : 'var(--text-tertiary)',
    fontSize: 12,
    fontWeight: 600,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    transition: 'all var(--duration-fast) var(--ease-out)',
  }
}
