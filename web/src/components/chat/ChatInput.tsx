import { useState, useRef, useEffect, useCallback, useMemo } from 'react'
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
import { LoaderCircle, Send, Square } from 'lucide-react'

const slashMenuAnchorStyle: React.CSSProperties = {
  position: 'absolute',
  left: 0,
  bottom: 'calc(100% + 8px)',
  zIndex: 100,
}

const budgetWarningThresholdRatio = 0.9
const budgetDangerThresholdRatio = 0.95

/** 将网关返回的技能列表转换成输入框使用的 slash 命令结构。 */
function buildSkillSlashCommands(
  skills: Array<{ descriptor: { id: string; description?: string }; active?: boolean }>,
): SkillSlashCommand[] {
  return skills.map((skill) => ({
    id: `skill-${skill.descriptor.id}`,
    usage: `/${skill.descriptor.id}`,
    description: skill.descriptor.description || '技能',
    hasArgument: false,
    isSkill: true,
    skillId: skill.descriptor.id,
    active: Boolean(skill.active),
  }))
}

/** 用当前命令定义生成帮助文本，避免菜单与帮助内容漂移。 */
function buildSlashHelpText(commands: AnySlashCommand[]): string {
  const helpCommands = commands.length > 0 ? commands : builtinSlashCommands
  const maxLen = helpCommands.reduce((max, command) => Math.max(max, command.usage.length), 0)
  const lines = helpCommands.map((command) => {
    const pad = ' '.repeat(maxLen - command.usage.length)
    const description = isSkillCommand(command) && command.active
      ? `${command.description} (已激活)`
      : command.description
    return `  ${command.usage}${pad}  - ${description}`
  })
  return ['可用命令：', ...lines].join('\n')
}

/** 统一提取系统工具返回文本，兼容 payload.content 与 payload.Content。 */
function extractSystemToolContent(result: unknown, fallback: string): string {
  const payload = (result as { payload?: { content?: string; Content?: string } } | null)?.payload
  const content = payload?.content ?? payload?.Content
  return content || fallback
}

/** 将预算事件转换为输入框圆环的语义状态，保持阈值和颜色判断集中。 */
function resolveBudgetRingState(
  budgetChecked: ReturnType<typeof useRuntimeInsightStore.getState>['budgetChecked'],
  budgetEstimateFailed: ReturnType<typeof useRuntimeInsightStore.getState>['budgetEstimateFailed'],
) {
  if (budgetEstimateFailed) {
    return {
      color: 'var(--error)',
      label: '预算估算失败',
      ratio: 0,
    }
  }
  if (!budgetChecked) {
    return {
      color: 'var(--text-tertiary)',
      label: '暂无预算数据',
      ratio: 0,
    }
  }

  const estimatedTokens = Math.max(0, budgetChecked.estimated_input_tokens)
  const promptBudget = Math.max(0, budgetChecked.prompt_budget)
  const contextLimit = Math.max(0, budgetChecked.context_window || promptBudget)
  const ringRatio = contextLimit > 0 ? Math.min(estimatedTokens / contextLimit, 1) : 0

  if (
    budgetChecked.action === 'stop' ||
    (contextLimit > 0 && estimatedTokens >= contextLimit * budgetDangerThresholdRatio) ||
    (!budgetChecked.context_window && promptBudget > 0 && estimatedTokens >= promptBudget)
  ) {
    return {
      color: 'var(--error)',
      label: '接近上下文上限',
      ratio: ringRatio,
    }
  }
  if (
    budgetChecked.action === 'compact' ||
    (promptBudget > 0 && estimatedTokens >= promptBudget * budgetWarningThresholdRatio)
  ) {
    return {
      color: 'var(--warning)',
      label: '接近自动压缩阈值',
      ratio: ringRatio,
    }
  }
  return {
    color: 'var(--success)',
    label: '正常',
    ratio: ringRatio,
  }
}

export default function ChatInput() {
  const gatewayAPI = useGatewayAPI()
  const text = useComposerStore((state) => state.composerText)
  const setText = useComposerStore((state) => state.setComposerText)
  const [rows, setRows] = useState(1)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const runCancelledRef = useRef(false)
  const composingRef = useRef(false)
  const isGenerating = useChatStore((state) => state.isGenerating)
  const isCompacting = useChatStore((state) => state.isCompacting)
  const compactMessage = useChatStore((state) => state.compactMessage)
  const addMessage = useChatStore((state) => state.addMessage)
  const addSystemMessage = useChatStore((state) => state.addSystemMessage)
  const setGenerating = useChatStore((state) => state.setGenerating)
  const sessionId = useSessionStore((state) => state.currentSessionId)
  const agentMode = useChatStore((state) => state.agentMode)
  const setAgentMode = useChatStore((state) => state.setAgentMode)
  const permissionMode = useChatStore((state) => state.permissionMode)
  const setPermissionMode = useChatStore((state) => state.setPermissionMode)

  const [showSlashMenu, setShowSlashMenu] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [matchedCommands, setMatchedCommands] = useState<AnySlashCommand[]>([])
  const [availableSkillCommands, setAvailableSkillCommands] = useState<SkillSlashCommand[]>([])
  const [showSkillPicker, setShowSkillPicker] = useState(false)
  const allSlashCommands = useMemo<AnySlashCommand[]>(
    () => [...builtinSlashCommands, ...availableSkillCommands],
    [availableSkillCommands],
  )

  useEffect(() => {
    if (!gatewayAPI || !text.trimLeft().startsWith('/')) return
    let cancelled = false
    gatewayAPI.listAvailableSkills(sessionId || undefined).then((result) => {
      if (cancelled) return
      const skills = result.payload?.skills || []
      setAvailableSkillCommands(buildSkillSlashCommands(skills))
    }).catch(() => {
      if (!cancelled) setAvailableSkillCommands([])
    })
    return () => {
      cancelled = true
    }
  }, [text, gatewayAPI, sessionId])

  useEffect(() => {
    if (!isSlashCommand(text)) {
      setMatchedCommands([])
      setShowSlashMenu(false)
      return
    }

    const matched = matchSlashCommands(text, allSlashCommands)
    setMatchedCommands(matched)
    setShowSlashMenu(matched.length > 0)
    if (matched.length > 0) setSelectedIndex(0)
  }, [text, allSlashCommands])

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
    if (isCompacting) {
      useUIStore.getState().showToast('Context compaction is still running', 'info')
      return true
    }
    if (!api) {
      useUIStore.getState().showToast('Gateway not connected', 'error')
      return true
    }

    switch (command) {
      case '/help': {
        addSystemMessage(buildSlashHelpText(allSlashCommands))
        return true
      }
      case '/compact': {
        if (!isValidSessionId(currentSessionId)) {
          useUIStore.getState().showToast('Send a message first to start a session', 'error')
          return true
        }
        useChatStore.getState().startCompacting('manual', 'Compacting context...')
        try {
          await api.compact(currentSessionId, '')
        } catch (err) {
          console.error('Compact failed:', err)
          if (useChatStore.getState().isCompacting) {
            useChatStore.getState().finishCompacting()
            useUIStore.getState().showToast('Compaction failed', 'error')
          }
        } finally {
          useChatStore.getState().finishCompacting()
        }
        return true
      }
      case '/memo': {
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_list', {})
          addSystemMessage(extractSystemToolContent(result, 'Memo query complete'))
        } catch (err) {
          console.error('Memo list failed:', err)
          useUIStore.getState().showToast('Failed to query memo', 'error')
        }
        return true
      }
      case '/remember': {
        if (!argument) {
          useUIStore.getState().showToast('Usage: /remember <content>', 'error')
          return true
        }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remember', {
            type: 'user',
            title: argument,
            content: argument,
          })
          addSystemMessage(extractSystemToolContent(result, 'Memo saved'))
        } catch (err) {
          console.error('Remember failed:', err)
          useUIStore.getState().showToast('Failed to save memo', 'error')
        }
        return true
      }
      case '/forget': {
        if (!argument) {
          useUIStore.getState().showToast('Usage: /forget <keyword>', 'error')
          return true
        }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remove', {
            keyword: argument,
            scope: 'all',
          })
          addSystemMessage(extractSystemToolContent(result, 'Memo deleted'))
        } catch (err) {
          console.error('Forget failed:', err)
          useUIStore.getState().showToast('Failed to delete memo', 'error')
        }
        return true
      }
      case '/skills': {
        setShowSkillPicker(true)
        return true
      }
      default: {
        if (isGenerating || isCompacting) {
          useUIStore.getState().showToast(isCompacting ? 'Context compaction is still running' : 'Cannot toggle skill while generating', 'info')
          return true
        }
        const skillCommand = availableSkillCommands.find((skill) => skill.usage === command)
        if (skillCommand && isValidSessionId(currentSessionId)) {
          try {
            if (skillCommand.active) {
              await api.deactivateSessionSkill(currentSessionId, skillCommand.skillId)
            } else {
              await api.activateSessionSkill(currentSessionId, skillCommand.skillId)
            }
            setAvailableSkillCommands((prev) => prev.map((item) => (
              item.skillId === skillCommand.skillId
                ? { ...item, active: !item.active }
                : item
            )))
          } catch (err) {
            console.error('Skill toggle failed:', err)
            useUIStore.getState().showToast('Skill operation failed', 'error')
          }
          return true
        }
        return false
      }
    }
  }, [gatewayAPI, sessionId, addSystemMessage, availableSkillCommands, isGenerating, isCompacting, allSlashCommands])

  async function handleSubmit() {
    const input = text.trim()
    if (!input) return

    if (isCompacting) {
      useUIStore.getState().showToast('Context compaction is still running', 'info')
      return
    }

    if (isGenerating) {
      if (isSlashCommand(input)) useUIStore.getState().showToast('Cannot run commands while generating', 'info')
      return
    }

    if (isSlashCommand(input)) {
      setText('')
      setShowSlashMenu(false)
      const handled = await executeSlashCommand(input)
      if (handled) return
    }

    setText('')
    const userMsg = createUserMessage(input)
    addMessage(userMsg)
    useRuntimeInsightStore.getState().setTodoSnapshot({
      items: [],
      summary: { total: 0, required_total: 0, required_completed: 0, required_failed: 0, required_open: 0 },
    })
    setGenerating(true)
    runCancelledRef.current = false

    try {
      if (!gatewayAPI) return
      const isNewSession = !isValidSessionId(sessionId)
      const ack = await gatewayAPI.run({
        session_id: isNewSession ? undefined : sessionId,
        new_session: isNewSession ? true : undefined,
        input_text: input,
        mode: agentMode,
      })
      if (!runCancelledRef.current) {
        const gatewayStore = useGatewayStore.getState()
        const sessionStore = useSessionStore.getState()
        if (ack.run_id) gatewayStore.setCurrentRunId(ack.run_id)
        if (ack.session_id) {
          sessionStore.setCurrentSessionId(ack.session_id)
          gatewayAPI.bindStream({ session_id: ack.session_id, channel: 'all' }).catch(() => {})
        }
      }
    } catch (err) {
      if (!runCancelledRef.current) {
        setGenerating(false)
        useChatStore.getState().removeMessage(userMsg.id)
        console.error('Run failed:', err)
        useUIStore.getState().showToast('Failed to send message', 'error')
      }
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (composingRef.current) return

    if (!showSlashMenu) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        handleSubmit()
      }
      return
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setSelectedIndex((prev) => (prev + 1) % matchedCommands.length)
        return
      case 'ArrowUp':
        e.preventDefault()
        setSelectedIndex((prev) => (prev - 1 + matchedCommands.length) % matchedCommands.length)
        return
      case 'Enter': {
        e.preventDefault()
        const command = matchedCommands[selectedIndex]
        if (command) handleSelectCommand(command)
        return
      }
      case 'Escape':
        e.preventDefault()
        setShowSlashMenu(false)
        return
      case 'Tab': {
        e.preventDefault()
        const command = matchedCommands[selectedIndex]
        if (command) {
          setText(`${command.usage} `)
          textareaRef.current?.focus()
        }
        return
      }
    }
  }

  function handleSelectCommand(cmd: AnySlashCommand) {
    if (isSkillCommand(cmd)) {
      setText(cmd.usage)
      setShowSlashMenu(false)
      void executeSlashCommand(cmd.usage)
      return
    }

    if (cmd.hasArgument) {
      setText(`${cmd.usage} `)
      setShowSlashMenu(false)
      textareaRef.current?.focus()
      return
    }

    setText('')
    setShowSlashMenu(false)
    void executeSlashCommand(cmd.usage)
  }

  async function handleCancel() {
    runCancelledRef.current = true
    const runId = useGatewayStore.getState().currentRunId
    if (runId && gatewayAPI) {
      try {
        await gatewayAPI.cancel({ run_id: runId })
      } catch (err) {
        console.error('Cancel failed:', err)
      }
    }
    useChatStore.getState().resetGeneratingState()
  }

  const isEmpty = !text.trim()
  const controlsLocked = isGenerating || isCompacting

  return (
    <>
      {showSkillPicker && gatewayAPI && (
        <SkillPicker gatewayAPI={gatewayAPI} sessionId={sessionId || ''} onClose={() => setShowSkillPicker(false)} />
      )}
      <div className="input-area">
        <div style={{ maxWidth: 740, margin: '0 auto', position: 'relative' }}>
          {showSlashMenu && matchedCommands.length > 0 && (
            <div style={slashMenuAnchorStyle}>
              <SlashCommandMenu
                commands={matchedCommands}
                selectedIndex={selectedIndex}
                onSelect={handleSelectCommand}
                onHover={setSelectedIndex}
                query={text.trim()}
              />
            </div>
          )}
          <div className={`input-box ${isCompacting ? 'compacting' : ''}`}>
            {isCompacting && (
              <div className="compact-status-row" role="status" aria-live="polite">
                <LoaderCircle size={14} className="compact-status-spinner" />
                <span>{compactMessage || 'Compacting context...'}</span>
              </div>
            )}
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
                  disabled={controlsLocked}
                  onClick={() => { if (!controlsLocked) setAgentMode(agentMode === 'plan' ? 'build' : 'plan') }}
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
                      opacity: controlsLocked ? 0.5 : 1,
                    }}
                  >
                    <button
                      type="button"
                      aria-pressed={permissionMode === 'default'}
                      disabled={controlsLocked}
                      style={permissionModeButtonStyle(permissionMode === 'default')}
                      onClick={() => {
                        if (controlsLocked) return
                        setPermissionMode('default')
                      }}
                    >
                      default
                    </button>
                    <button
                      type="button"
                      aria-pressed={permissionMode === 'bypass'}
                      disabled={controlsLocked}
                      style={permissionModeButtonStyle(permissionMode === 'bypass')}
                      onClick={() => {
                        if (controlsLocked) return
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
                className={`input-send-btn ${isGenerating ? 'stop' : ''}`}
                onClick={isGenerating ? handleCancel : handleSubmit}
                disabled={isCompacting || (isEmpty && !isGenerating)}
                title={isCompacting ? 'Context compaction is still running' : isGenerating ? '停止生成' : '发送'}
              >
                {isGenerating ? <Square size={16} /> : <Send size={16} />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

function BudgetTokenStrip() {
  const budgetChecked = useRuntimeInsightStore((state) => state.budgetChecked)
  const budgetUsageRatio = useRuntimeInsightStore((state) => state.budgetUsageRatio)
  const budgetEstimateFailed = useRuntimeInsightStore((state) => state.budgetEstimateFailed)
  const ledgerReconciled = useRuntimeInsightStore((state) => state.ledgerReconciled)
  const tokenUsage = useChatStore((state) => state.tokenUsage)
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const [popoverStyle, setPopoverStyle] = useState<React.CSSProperties>({})

  const totalTokens = tokenUsage ? tokenUsage.input_tokens + tokenUsage.output_tokens : 0
  const budgetRingState = resolveBudgetRingState(budgetChecked, budgetEstimateFailed)
  const ratio = budgetRingState.ratio
  const budgetPct = budgetUsageRatio ?? 0
  const pct = Math.min(Math.round(ratio * 100), 100)
  const budgetThresholdPct = Math.min(Math.round(budgetPct * 100), 100)

  // SVG ring: radius 8, circumference ~50
  const r = 7
  const circ = 2 * Math.PI * r
  const dash = (ratio * circ).toFixed(1)
  const ringColor = budgetRingState.color

  // Click outside to close
  useEffect(() => {
    if (!open) return

    /** 根据锚点位置动态计算弹层，避免被容器裁剪或超出视口。 */
    function updatePopoverPosition() {
      const anchor = ref.current
      if (!anchor) return
      const rect = anchor.getBoundingClientRect()
      const width = 260
      const leftMin = 12
      const leftMax = Math.max(leftMin, window.innerWidth - width - 12)
      const preferredLeft = rect.right - width + 6
      const left = Math.min(leftMax, Math.max(leftMin, preferredLeft))
      const bottom = Math.max(36, window.innerHeight - rect.top + 8)
      setPopoverStyle({
        position: 'fixed',
        left,
        bottom,
        width,
      })
    }

    updatePopoverPosition()
    function click(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('resize', updatePopoverPosition)
    window.addEventListener('scroll', updatePopoverPosition, true)
    document.addEventListener('mousedown', click)
    return () => {
      window.removeEventListener('resize', updatePopoverPosition)
      window.removeEventListener('scroll', updatePopoverPosition, true)
      document.removeEventListener('mousedown', click)
    }
  }, [open])

  if (!budgetChecked && !totalTokens) return null

  return (
    <div
      ref={ref}
      style={{ position: 'relative' }}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          cursor: 'pointer',
          padding: '2px 4px',
          borderRadius: 'var(--radius-sm)',
        }}
      >
        <svg width={18} height={18} style={{ display: 'block' }}>
          <circle cx={9} cy={9} r={r} fill="none" stroke="var(--bg-active)" strokeWidth="2" />
          {budgetChecked && (
            <circle
              data-testid="budget-token-ring"
              cx={9}
              cy={9}
              r={r}
              fill="none"
              stroke={ringColor}
              strokeWidth="2"
              strokeDasharray={`${dash} ${circ}`}
              strokeLinecap="round"
              transform="rotate(-90 9 9)"
              style={{ transition: 'stroke-dasharray 0.3s var(--ease-out), stroke 0.3s var(--ease-out)' }}
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
        <div className="budget-popover" style={popoverStyle}>
          <div className="budget-popover-title">用量明细</div>
          {budgetEstimateFailed ? (
            <div style={{ color: 'var(--error)', fontSize: 11 }}>{budgetEstimateFailed.message}</div>
          ) : budgetChecked ? (
            <>
              <div className="budget-popover-row">
                <span className="budget-popover-label">状态</span>
                <span className="budget-popover-value" style={{ color: ringColor }}>{budgetRingState.label}</span>
              </div>
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
                <span className="budget-popover-value">
                  {formatTokenCount(budgetChecked.estimated_input_tokens)} ({budgetThresholdPct}%)
                </span>
              </div>
              {budgetChecked.context_window && (
                <div className="budget-popover-row">
                  <span className="budget-popover-label">上限占用</span>
                  <span className="budget-popover-value">{pct}%</span>
                </div>
              )}
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
