import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import MarkdownContent from './MarkdownContent'

describe('MarkdownContent', () => {
  it('renders GFM tables correctly', async () => {
    render(<MarkdownContent content={'| A | B |\n| - | - |\n| 1 | 2 |'} />)
    expect(await screen.findByText('A')).toBeTruthy()
    expect(await screen.findByText('B')).toBeTruthy()
    expect(await screen.findByText('1')).toBeTruthy()
    expect(await screen.findByText('2')).toBeTruthy()
  })

  it('keeps incomplete streaming markdown visible without crashing', async () => {
    render(<MarkdownContent content={'```ts\nconst a = 1'} streaming />)
    expect(await screen.findByText(/const a = 1/)).toBeTruthy()
  })
})
