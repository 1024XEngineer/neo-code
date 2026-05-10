/**
 * Slash Command 定义、解析与匹配工具模块
 * 与 Web 端当前已支持的命令集保持一致
 */

export interface SlashCommand {
  id: string
  usage: string
  description: string
  hasArgument: boolean
  argumentPlaceholder?: string
}

export interface SkillSlashCommand extends SlashCommand {
  isSkill: true
  skillId: string
  active: boolean
}

export type AnySlashCommand = SlashCommand | SkillSlashCommand

export const builtinSlashCommands: SlashCommand[] = [
  {
    id: 'help',
    usage: '/help',
    description: '显示所有可用命令',
    hasArgument: false,
  },
  {
    id: 'compact',
    usage: '/compact',
    description: '压缩当前会话上下文',
    hasArgument: false,
  },
  {
    id: 'memo',
    usage: '/memo',
    description: '显示持久化备忘录索引',
    hasArgument: false,
  },
  {
    id: 'remember',
    usage: '/remember',
    description: '保存持久化备忘录',
    hasArgument: true,
    argumentPlaceholder: '内容',
  },
  {
    id: 'forget',
    usage: '/forget',
    description: '按关键词删除备忘录',
    hasArgument: true,
    argumentPlaceholder: '关键词',
  },
  {
    id: 'skills',
    usage: '/skills',
    description: '列出可用技能并管理',
    hasArgument: false,
  },
]

const builtinUsages = new Set(builtinSlashCommands.map((command) => command.usage.toLowerCase()))

/**
 * 解析 slash command 输入，提取命令与参数部分。
 */
export function parseSlashCommand(input: string): { command: string; argument: string } | null {
  const trimmed = input.trim()
  if (!trimmed.startsWith('/')) return null

  const firstSpace = trimmed.indexOf(' ')
  if (firstSpace === -1) {
    return { command: trimmed.toLowerCase(), argument: '' }
  }

  const command = trimmed.slice(0, firstSpace).toLowerCase()
  const argument = trimmed.slice(firstSpace + 1).trim()
  return { command, argument }
}

/**
 * 判断输入是否进入 slash 提示态；单独输入 "/" 也应视为有效触发。
 */
export function isSlashCommand(input: string): boolean {
  return input.trim().startsWith('/')
}

/**
 * 判断输入是否已经完整匹配某个命令；完整命令且无尾随空格时不再继续提示。
 */
function isCompleteSlashCommand(input: string, commands: AnySlashCommand[]): boolean {
  const normalizedInput = input.trimLeft().toLowerCase()
  if (normalizedInput.trimRight() !== normalizedInput) {
    return false
  }
  return commands.some((command) => command.usage.trim().toLowerCase() === normalizedInput)
}

/**
 * 计算模糊匹配分值；分值越小表示匹配越优先，返回 null 表示不匹配。
 */
function buildFuzzyMatchScore(target: string, needle: string): number | null {
  if (!target || !needle) return null
  if (target === needle) return 0
  if (target.startsWith(needle)) return 100 + target.length

  const includeIndex = target.indexOf(needle)
  if (includeIndex >= 0) return 200 + includeIndex

  let cursor = 0
  let spanStart = -1
  let spanEnd = -1
  for (const char of needle) {
    const foundAt = target.indexOf(char, cursor)
    if (foundAt < 0) return null
    if (spanStart < 0) spanStart = foundAt
    spanEnd = foundAt
    cursor = foundAt + 1
  }

  if (spanStart < 0 || spanEnd < 0) return null
  const spanLength = spanEnd - spanStart + 1
  return 500 + spanLength + spanStart
}

/**
 * 根据输入过滤匹配命令列表；优先 usage/id，再回退 description。
 */
export function matchSlashCommands(input: string, commands: AnySlashCommand[]): AnySlashCommand[] {
  if (!isSlashCommand(input)) return []

  const query = input.trimLeft().toLowerCase()
  if (query === '/') return commands
  if (isCompleteSlashCommand(query, commands)) return []

  const needle = query.slice(1).trim()
  if (!needle) return commands

  const matches = commands
    .map((command, index) => {
      const usageScore = buildFuzzyMatchScore(command.usage.toLowerCase().slice(1), needle)
      const idScore = buildFuzzyMatchScore(command.id.toLowerCase(), needle)
      const descriptionScore = buildFuzzyMatchScore(command.description.toLowerCase(), needle)

      const bestScore = [usageScore, idScore, descriptionScore == null ? null : descriptionScore + 1000]
        .filter((score): score is number => score != null)
        .sort((left, right) => left - right)[0]

      if (bestScore == null) return null
      return { command, index, score: bestScore }
    })
    .filter((entry): entry is { command: AnySlashCommand; index: number; score: number } => entry != null)
    .sort((left, right) => left.score - right.score || left.index - right.index)
    .map((entry) => entry.command)

  if (matches.length > 0) return matches
  if (needle.length === 1) return commands
  return []
}

/**
 * 判断输入是否匹配一个已知的完整内置命令。
 */
export function isKnownSlashCommand(input: string): boolean {
  const parsed = parseSlashCommand(input)
  if (!parsed) return false
  return builtinUsages.has(parsed.command)
}

/**
 * 判断是否为内置命令（非技能命令）。
 */
export function isBuiltinCommand(cmd: AnySlashCommand): cmd is SlashCommand {
  return !('isSkill' in cmd)
}

/**
 * 判断是否为技能命令。
 */
export function isSkillCommand(cmd: AnySlashCommand): cmd is SkillSlashCommand {
  return 'isSkill' in cmd && cmd.isSkill
}
