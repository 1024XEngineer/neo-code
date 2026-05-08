import { useState, useRef, useEffect, useCallback } from 'react'
import { useChatStore, createUserMessage } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore, isValidSessionId } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
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
import { Send, Square } from 'lucide-react'

/** 聊天输入框 */
export default function ChatInput() {
  const gatewayAPI = useGatewayAPI()
  const text = useComposerStore((s) => s.composerText)
  const setText = useComposerStore((s) => s.setComposerText)
  const [rows, setRows] = useState(1)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const runCancelledRef = useRef(false)
  const isGenerating = useChatStore((s) => s.isGenerating)
  const addMessage = useChatStore((s) => s.addMessage)
  const addSystemMessage = useChatStore((s) => s.addSystemMessage)
  const setGenerating = useChatStore((s) => s.setGenerating)
  const sessionId = useSessionStore((s) => s.currentSessionId)
  const agentMode = useChatStore((s) => s.agentMode)
  const setAgentMode = useChatStore((s) => s.setAgentMode)
  const permissionMode = useChatStore((s) => s.permissionMode)
  const setPermissionMode = useChatStore((s) => s.setPermissionMode)

  // Slash command 菜单状态
  const [showSlashMenu, setShowSlashMenu] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [matchedCommands, setMatchedCommands] = useState<AnySlashCommand[]>([])
  const [availableSkillCommands, setAvailableSkillCommands] = useState<SkillSlashCommand[]>([])

  // 技能选择弹层
  const [showSkillPicker, setShowSkillPicker] = useState(false)

  // 动态加载技能列表用于 slash 菜单
  useEffect(() => {
    if (!showSlashMenu || !gatewayAPI) return

    gatewayAPI.listAvailableSkills(sessionId || undefined).then((result) => {
      const skills = result.payload?.skills || []
      const skillCommands: SkillSlashCommand[] = skills.map((s) => ({
        id: `skill-${s.descriptor.id}`,
        usage: `/${s.descriptor.id}`,
        description: s.descriptor.description || '技能',
        hasArgument: false,
        isSkill: true,
        skillId: s.descriptor.id,
        active: s.active,
      }))
      setAvailableSkillCommands(skillCommands)
    }).catch(() => {
      setAvailableSkillCommands([])
    })
  }, [showSlashMenu, gatewayAPI, sessionId])

  // 合并内置命令和技能命令，并做匹配
  useEffect(() => {
    if (!isSlashCommand(text)) {
      setShowSlashMenu(false)
      return
    }
    const allCommands: AnySlashCommand[] = [...builtinSlashCommands, ...availableSkillCommands]
    const matched = matchSlashCommands(text, allCommands)
    if (matched.length > 0) {
      setMatchedCommands(matched)
      setShowSlashMenu(true)
      setSelectedIndex(0)
    } else {
      setShowSlashMenu(false)
    }
  }, [text, availableSkillCommands])

  // 行数自适应
  useEffect(() => {
    const lines = text.split('\n').length
    setRows(Math.min(Math.max(lines, 1), 8))
  }, [text])

  /** 执行 slash command */
  const executeSlashCommand = useCallback(async (input: string): Promise<boolean> => {
    const parsed = parseSlashCommand(input)
    if (!parsed) return false

    const { command, argument } = parsed
    const currentSessionId = sessionId
    const api = gatewayAPI

    if (!api) {
      useUIStore.getState().showToast('Gateway not connected', 'error')
      return true
    }

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
        if (!isValidSessionId(currentSessionId)) {
          useUIStore.getState().showToast('Send a message first to start a session', 'error')
          return true
        }
        try {
          await api.compact(currentSessionId, '')
        } catch (err) {
          console.error('Compact failed:', err)
          useUIStore.getState().showToast('Compaction failed', 'error')
        }
        return true
      }

      case '/memo': {
        if (!isValidSessionId(currentSessionId)) {
          useUIStore.getState().showToast('Send a message first to start a session', 'error')
          return true
        }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_list', {})
          const content = (result as any)?.payload?.content || 'Memo query complete'
          addSystemMessage(content)
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
        if (!isValidSessionId(currentSessionId)) {
          useUIStore.getState().showToast('Send a message first to start a session', 'error')
          return true
        }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remember', {
            type: 'user',
            title: argument,
            content: argument,
          })
          const content = (result as any)?.payload?.content || 'Memo saved'
          addSystemMessage(content)
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
        if (!isValidSessionId(currentSessionId)) {
          useUIStore.getState().showToast('Send a message first to start a session', 'error')
          return true
        }
        try {
          const result = await api.executeSystemTool(currentSessionId, '', 'memo_remove', {
            keyword: argument,
            scope: 'all',
          })
          const content = (result as any)?.payload?.content || 'Memo deleted'
          addSystemMessage(content)
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
        if (isGenerating) {
          useUIStore.getState().showToast('Cannot toggle skill while generating; wait for the current run to finish.', 'info')
          return true
        }
        // 检查是否是技能快捷命令
        const skillCmd = availableSkillCommands.find((s) => s.usage === command)
        if (skillCmd && isValidSessionId(currentSessionId)) {
          try {
            if (skillCmd.active) {
              await api.deactivateSessionSkill(currentSessionId, skillCmd.skillId)
            } else {
              await api.activateSessionSkill(currentSessionId, skillCmd.skillId)
            }
            // 刷新技能列表状态
            setAvailableSkillCommands((prev) =>
              prev.map((item) =>
                item.skillId === skillCmd.skillId ? { ...item, active: !item.active } : item
              )
            )
          } catch (err) {
            console.error('Skill toggle failed:', err)
            useUIStore.getState().showToast('Skill operation failed', 'error')
          }
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
      if (isSlashCommand(input)) {
        useUIStore.getState().showToast('Cannot run commands while generating; wait for the current run to finish.', 'info')
      }
      return
    }

    // Slash command 拦截
    if (isSlashCommand(input)) {
      setText('')
      setShowSlashMenu(false)
      const handled = await executeSlashCommand(input)
      if (handled) return
      // 未知命令回退为普通消息
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

      // 仅在未被取消时回写 session_id 和 run_id
      if (!runCancelledRef.current) {
        const gwStore = useGatewayStore.getState()
        const sessStore = useSessionStore.getState()
        if (ack.run_id) {
          gwStore.setCurrentRunId(ack.run_id)
        }
        if (ack.session_id) {
          sessStore.setCurrentSessionId(ack.session_id)
          // 将 stream 绑定到新会话，确保后续事件的 frameSessionId 正确
          if (gatewayAPI) {
            gatewayAPI.bindStream({ session_id: ack.session_id, channel: 'all' }).catch(() => {})
          }
        }
      }
    } catch (err) {
      if (!runCancelledRef.current) {
        setGenerating(false)
        useChatStore.getState().removeMessage(userMsg.id)
        console.error('Run failed:', err)
        useUIStore.getState().showToast('Failed to send message; please try again.', 'error')
      }
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!showSlashMenu) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        handleSubmit()
      }
      return
    }

    // 菜单打开时的键盘导航
    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault()
        setSelectedIndex((prev) => (prev + 1) % matchedCommands.length)
        return
      }
      case 'ArrowUp': {
        e.preventDefault()
        setSelectedIndex((prev) => (prev - 1 + matchedCommands.length) % matchedCommands.length)
        return
      }
      case 'Enter': {
        e.preventDefault()
        const cmd = matchedCommands[selectedIndex]
        if (cmd) {
          handleSelectCommand(cmd)
        }
        return
      }
      case 'Escape': {
        e.preventDefault()
        setShowSlashMenu(false)
        return
      }
      case 'Tab': {
        e.preventDefault()
        const cmd = matchedCommands[selectedIndex]
        if (cmd) {
          setText(cmd.usage + ' ')
          textareaRef.current?.focus()
        }
        return
      }
    }
  }

  function handleSelectCommand(cmd: AnySlashCommand) {
    if (isSkillCommand(cmd)) {
      // 技能命令：直接切换激活状态
      setText(cmd.usage)
      setShowSlashMenu(false)
      executeSlashCommand(cmd.usage)
      return
    }

    if (cmd.hasArgument) {
      // 需要参数的命令：填充 usage 到输入框，保留菜单关闭让用户继续输入
      setText(cmd.usage + ' ')
      setShowSlashMenu(false)
      textareaRef.current?.focus()
    } else {
      // 无参数命令：直接执行
      setText('')
      setShowSlashMenu(false)
      executeSlashCommand(cmd.usage)
    }
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

  return (
    <>
      {showSkillPicker && gatewayAPI && (
        <SkillPicker
          gatewayAPI={gatewayAPI}
          sessionId={sessionId || ''}
          onClose={() => setShowSkillPicker(false)}
        />
      )}
      <div style={styles.container} ref={containerRef}>
        <div style={{ position: 'relative' }}>
          {showSlashMenu && matchedCommands.length > 0 && (
            <SlashCommandMenu
              commands={matchedCommands}
              selectedIndex={selectedIndex}
              onSelect={handleSelectCommand}
              onHover={setSelectedIndex}
              query={text.trim()}
            />
          )}
          <div style={styles.inputBox}>
            <textarea
              ref={textareaRef}
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="输入指令或问题... (Shift+Enter 换行, / 查看命令)"
              rows={rows}
              style={styles.textarea}
            />
            <div style={styles.toolbar}>
              <div style={styles.leftActions}>
                <button
                  style={{
                    ...styles.modeBtn,
                    background: agentMode === 'plan' ? 'var(--accent-soft, rgba(99,102,241,0.12))' : 'transparent',
                    color: agentMode === 'plan' ? 'var(--accent)' : 'var(--text-tertiary)',
                    opacity: isGenerating ? 0.5 : 1,
                    cursor: isGenerating ? 'not-allowed' : 'pointer',
                  }}
                  title={agentMode === 'plan' ? '当前模式：Plan（规划模式）' : '当前模式：Build（构建模式）'}
                  onClick={() => {
                    if (isGenerating) return
                    setAgentMode(agentMode === 'plan' ? 'build' : 'plan')
                  }}
                >
                  {agentMode === 'plan' ? 'Plan' : 'Build'}
                </button>
                {agentMode === 'build' && (
                  <div
                    aria-label="Build permission mode"
                    role="group"
                    style={{
                      ...styles.permissionModeGroup,
                      opacity: isGenerating ? 0.5 : 1,
                    }}
                  >
                    <button
                      type="button"
                      aria-pressed={permissionMode === 'default'}
                      style={{
                        ...styles.permissionModeBtn,
                        ...(permissionMode === 'default' ? styles.permissionModeBtnActive : null),
                        cursor: isGenerating ? 'not-allowed' : 'pointer',
                      }}
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
                      style={{
                        ...styles.permissionModeBtn,
                        ...(permissionMode === 'bypass' ? styles.permissionModeBtnActive : null),
                        cursor: isGenerating ? 'not-allowed' : 'pointer',
                      }}
                      onClick={() => {
                        if (isGenerating) return
                        setPermissionMode('bypass')
                      }}
                    >
                      bypass
                    </button>
                  </div>
                )}
              </div>
              <button
                style={{
                  ...styles.sendBtn,
                  opacity: text.trim() || isGenerating ? 1 : 0.4,
                  cursor: text.trim() || isGenerating ? 'pointer' : 'default',
                }}
                onClick={isGenerating ? handleCancel : handleSubmit}
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

const styles: Record<string, React.CSSProperties> = {
  container: {
    padding: '12px 16px 20px',
    flexShrink: 0,
    background: 'var(--bg-primary)',
  },
  inputBox: {
    border: '1px solid var(--border-primary)',
    borderRadius: 'var(--radius-lg)',
    background: 'var(--bg-secondary)',
    overflow: 'hidden',
    transition: 'border-color 0.2s',
  },
  textarea: {
    width: '100%',
    padding: '10px 12px',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-primary)',
    fontSize: 14,
    fontFamily: 'var(--font-ui)',
    lineHeight: 1.6,
    resize: 'none',
    outline: 'none',
    minHeight: 20,
    maxHeight: 200,
  },
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 8px',
    borderTop: '1px solid var(--border-primary)',
  },
  leftActions: {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
  },
  permissionModeGroup: {
    display: 'flex',
    alignItems: 'center',
    gap: 2,
    marginLeft: 6,
    padding: 2,
    borderRadius: 'var(--radius-sm)',
    background: 'var(--bg-tertiary)',
  },
  permissionModeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 62,
    height: 28,
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    color: 'var(--text-tertiary)',
    fontSize: 12,
    fontWeight: 600,
    fontFamily: 'var(--font-ui)',
    transition: 'all 0.15s',
  },
  permissionModeBtnActive: {
    background: 'var(--bg-primary)',
    color: 'var(--text-primary)',
    boxShadow: 'var(--shadow-1)',
  },
  modeBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: 28,
    padding: '0 8px',
    borderRadius: 'var(--radius-sm)',
    border: 'none',
    background: 'transparent',
    fontSize: 12,
    fontWeight: 600,
    fontFamily: 'var(--font-ui)',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
  sendBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 32,
    height: 32,
    borderRadius: 'var(--radius-md)',
    border: 'none',
    background: 'var(--accent)',
    color: '#fff',
    cursor: 'pointer',
    transition: 'all 0.15s',
  },
}
