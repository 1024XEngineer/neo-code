import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import GitDiffPreviewEditor from './GitDiffPreviewEditor'

vi.mock('@monaco-editor/react', () => ({
  DiffEditor: ({
    original,
    modified,
    language,
    theme,
    options,
  }: {
    original: string
    modified: string
    language: string
    theme: string
    options: { renderSideBySide?: boolean }
  }) => (
    <div
      data-testid="monaco-diff-editor"
      data-language={language}
      data-theme={theme}
      data-side-by-side={String(Boolean(options.renderSideBySide))}
    >
      {original}::{modified}
    </div>
  ),
}))

describe('GitDiffPreviewEditor', () => {
  it('renders diff editor with language detection and side by side mode', () => {
    render(
      <GitDiffPreviewEditor
        path="src/main.go"
        originalContent="before"
        modifiedContent="after"
        theme="dark"
        renderSideBySide={true}
      />,
    )

    const editor = screen.getByTestId('monaco-diff-editor')
    expect(editor).toHaveAttribute('data-language', 'go')
    expect(editor).toHaveAttribute('data-theme', 'vs-dark')
    expect(editor).toHaveAttribute('data-side-by-side', 'true')
    expect(editor.textContent).toContain('before::after')
  })
})
