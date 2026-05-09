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
    options: { renderSideBySide?: boolean; lineNumbers?: string; fontFamily?: string }
  }) => (
    <div
      data-testid="monaco-diff-editor"
      data-language={language}
      data-theme={theme}
      data-side-by-side={String(Boolean(options.renderSideBySide))}
      data-line-numbers={options.lineNumbers}
      data-font-family={options.fontFamily}
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
    expect(editor).toHaveAttribute('data-line-numbers', 'on')
    expect(editor).toHaveAttribute('data-font-family', 'var(--font-mono)')
    expect(editor.textContent).toContain('before::after')
  })

  it('hides line numbers in inline diff mode to avoid duplicated columns', () => {
    render(
      <GitDiffPreviewEditor
        path="src/main.go"
        originalContent="before"
        modifiedContent="after"
        theme="dark"
        renderSideBySide={false}
      />,
    )

    const editor = screen.getByTestId('monaco-diff-editor')
    expect(editor).toHaveAttribute('data-side-by-side', 'false')
    expect(editor).toHaveAttribute('data-line-numbers', 'on')
    expect(screen.getByTestId('git-diff-preview-host')).toHaveClass('git-diff-preview-host-inline')
  })
})
