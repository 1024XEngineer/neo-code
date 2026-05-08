import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import CodePreviewEditor, { detectMonacoLanguage } from './CodePreviewEditor'

vi.mock('@monaco-editor/react', () => ({
  default: ({
    path,
    value,
    language,
    theme,
    options,
  }: {
    path: string
    value: string
    language: string
    theme: string
    options: { readOnly?: boolean }
  }) => (
    <div
      data-testid="monaco-editor"
      data-path={path}
      data-value={value}
      data-language={language}
      data-theme={theme}
      data-readonly={String(Boolean(options.readOnly))}
    />
  ),
}))

describe('detectMonacoLanguage', () => {
  it('maps known extensions to Monaco languages', () => {
    expect(detectMonacoLanguage('cmd/neocode/main.go')).toBe('go')
    expect(detectMonacoLanguage('src/app.tsx')).toBe('typescript')
    expect(detectMonacoLanguage('docs/readme.md')).toBe('markdown')
    expect(detectMonacoLanguage('api/openapi.yaml')).toBe('yaml')
    expect(detectMonacoLanguage('types/global.d.ts')).toBe('typescript')
  })

  it('falls back to plaintext for unknown extensions', () => {
    expect(detectMonacoLanguage('foo.unknown')).toBe('plaintext')
    expect(detectMonacoLanguage('LICENSE')).toBe('plaintext')
  })
})

describe('CodePreviewEditor', () => {
  it('renders Monaco in readonly preview mode with mapped language and theme', () => {
    render(<CodePreviewEditor path="cmd/neocode/main.go" content="package main" theme="dark" />)

    const editor = screen.getByTestId('monaco-editor')
    expect(editor).toHaveAttribute('data-path', 'cmd/neocode/main.go')
    expect(editor).toHaveAttribute('data-value', 'package main')
    expect(editor).toHaveAttribute('data-language', 'go')
    expect(editor).toHaveAttribute('data-theme', 'vs-dark')
    expect(editor).toHaveAttribute('data-readonly', 'true')
  })

  it('uses the explicit language when provided', () => {
    render(<CodePreviewEditor path="notes.txt" content="hello" theme="light" language="markdown" />)

    const editor = screen.getByTestId('monaco-editor')
    expect(editor).toHaveAttribute('data-language', 'markdown')
    expect(editor).toHaveAttribute('data-theme', 'vs')
  })
})
