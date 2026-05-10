import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import SlashCommandMenu from './SlashCommandMenu'
import type { AnySlashCommand } from '@/utils/slashCommands'

const builtin: AnySlashCommand = {
  id: 'compact',
  usage: '/compact',
  description: 'compress context',
  hasArgument: false,
}

const skill: AnySlashCommand = {
  id: 'skill.demo',
  usage: '/skill.demo',
  description: 'demo skill',
  hasArgument: false,
  isSkill: true,
  skillId: 'skill.demo',
  active: true,
}

describe('SlashCommandMenu', () => {
  ;(HTMLElement.prototype as { scrollIntoView?: () => void }).scrollIntoView = vi.fn()

  it('returns null when commands is empty', () => {
    const { container } = render(
      <SlashCommandMenu commands={[]} selectedIndex={0} onSelect={vi.fn()} onHover={vi.fn()} query="/" />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders builtin and skill sections without owning absolute positioning', () => {
    render(
      <SlashCommandMenu
        commands={[builtin, skill]}
        selectedIndex={0}
        onSelect={vi.fn()}
        onHover={vi.fn()}
        query="/com"
      />,
    )

    const menu = screen.getByTestId('slash-command-menu')
    expect(menu).toBeInTheDocument()
    expect(menu).not.toHaveStyle({ position: 'absolute' })
    expect(screen.getByText('命令')).toBeInTheDocument()
    expect(screen.getByText('技能')).toBeInTheDocument()
    expect(screen.getByText('已激活')).toBeInTheDocument()
    expect(screen.getAllByText((_, el) => Boolean(el?.textContent?.includes('/compact'))).length).toBeGreaterThan(0)
  })

  it('triggers hover and select callbacks', () => {
    const onSelect = vi.fn()
    const onHover = vi.fn()

    render(
      <SlashCommandMenu
        commands={[builtin, skill]}
        selectedIndex={1}
        onSelect={onSelect}
        onHover={onHover}
        query="/"
      />,
    )

    fireEvent.mouseEnter(screen.getByText('/compact'))
    fireEvent.click(screen.getByText('/skill.demo'))

    expect(onHover).toHaveBeenCalledWith(0)
    expect(onSelect).toHaveBeenCalledWith(skill)
  })
})
