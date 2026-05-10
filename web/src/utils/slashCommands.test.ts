import { describe, expect, it } from 'vitest'
import {
  builtinSlashCommands,
  isBuiltinCommand,
  isKnownSlashCommand,
  isSkillCommand,
  isSlashCommand,
  matchSlashCommands,
  parseSlashCommand,
  type AnySlashCommand,
} from './slashCommands'

describe('slashCommands utils', () => {
  it('parses command with and without argument', () => {
    expect(parseSlashCommand('/help')).toEqual({ command: '/help', argument: '' })
    expect(parseSlashCommand('/remember user is Alice')).toEqual({
      command: '/remember',
      argument: 'user is Alice',
    })
    expect(parseSlashCommand('hello')).toBeNull()
  })

  it('treats slash trigger as a valid command input shape', () => {
    expect(isSlashCommand('/')).toBe(true)
    expect(isSlashCommand('/a')).toBe(true)
    expect(isSlashCommand('  /skills')).toBe(true)
    expect(isSlashCommand('abc')).toBe(false)
  })

  it('returns all current web commands for bare slash', () => {
    const matched = matchSlashCommands('/', builtinSlashCommands)
    expect(matched.map((command) => command.id)).toEqual(builtinSlashCommands.map((command) => command.id))
  })

  it('matches commands with prefix and fuzzy fallbacks', () => {
    const skill: AnySlashCommand = {
      id: 'my-skill',
      usage: '/skill.my',
      description: 'my custom skill',
      hasArgument: false,
      isSkill: true,
      skillId: 'my-skill',
      active: false,
    }
    const commands = [...builtinSlashCommands, skill]

    expect(matchSlashCommands('/com', commands).map((command) => command.id)).toContain('compact')
    expect(matchSlashCommands('/mem', commands).map((command) => command.id)).toContain('memo')
    expect(matchSlashCommands('/w', commands).length).toBeGreaterThan(0)
    expect(matchSlashCommands('/my', commands).map((command) => command.id)).toContain('my-skill')
  })

  it('hides suggestions for complete commands without trailing space', () => {
    expect(matchSlashCommands('/help', builtinSlashCommands)).toEqual([])
    expect(matchSlashCommands('/remember', builtinSlashCommands)).toEqual([])
    expect(matchSlashCommands('/remember ', builtinSlashCommands).map((command) => command.id)).toContain('remember')
  })

  it('checks known builtin slash command', () => {
    expect(isKnownSlashCommand('/help')).toBe(true)
    expect(isKnownSlashCommand('/help foo')).toBe(true)
    expect(isKnownSlashCommand('/skill.my')).toBe(false)
  })

  it('guards builtin and skill commands', () => {
    const builtin = builtinSlashCommands[0]
    const skill: AnySlashCommand = {
      id: 'my-skill',
      usage: '/skill.my',
      description: 'desc',
      hasArgument: false,
      isSkill: true,
      skillId: 'my-skill',
      active: true,
    }
    expect(isBuiltinCommand(builtin)).toBe(true)
    expect(isSkillCommand(builtin)).toBe(false)
    expect(isBuiltinCommand(skill)).toBe(false)
    expect(isSkillCommand(skill)).toBe(true)
  })
})
