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

	it('detects slash command shape', () => {
		expect(isSlashCommand('/')).toBe(false)
		expect(isSlashCommand('/a')).toBe(true)
		expect(isSlashCommand('  /skills')).toBe(true)
		expect(isSlashCommand('abc')).toBe(false)
	})

	it('matches commands by usage/description/id', () => {
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
		expect(matchSlashCommands('/help', commands).map((c) => c.id)).toContain('help')
		expect(matchSlashCommands('/com', commands).map((c) => c.id)).toContain('compact')
		expect(matchSlashCommands('/my', commands).map((c) => c.id)).toContain('my-skill')
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
