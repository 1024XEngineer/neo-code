import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useComposerStore, resolveAttachmentKind } from './useComposerStore'

beforeEach(() => {
  vi.restoreAllMocks()
  if (typeof URL.createObjectURL !== 'function') {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn() })
  }
  if (typeof URL.revokeObjectURL !== 'function') {
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  }
  useComposerStore.setState({ composerText: '', attachments: [] })
})

describe('useComposerStore', () => {
  it('starts with empty text', () => {
    expect(useComposerStore.getState().composerText).toBe('')
  })

  it('setComposerText updates the value', () => {
    useComposerStore.getState().setComposerText('hello')
    expect(useComposerStore.getState().composerText).toBe('hello')
  })

  it('overwrites existing text on subsequent setComposerText calls', () => {
    useComposerStore.getState().setComposerText('first')
    useComposerStore.getState().setComposerText('second')
    expect(useComposerStore.getState().composerText).toBe('second')
  })

  it('adds image attachments with preview URLs', () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')
    const file = new File(['img'], 'a.png', { type: 'image/png' })

    useComposerStore.getState().addAttachmentFiles([file])

    const [attachment] = useComposerStore.getState().attachments
    expect(createObjectURL).toHaveBeenCalledWith(file)
    expect(attachment).toMatchObject({
      file,
      previewUrl: 'blob:preview-1',
      status: 'pending',
    })
  })

  it('revokes preview URL when removing attachments', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    useComposerStore.getState().addAttachmentFiles([new File(['img'], 'a.png', { type: 'image/png' })])
    const attachmentId = useComposerStore.getState().attachments[0].id

    useComposerStore.getState().removeAttachment(attachmentId)

    expect(useComposerStore.getState().attachments).toEqual([])
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
  })

  it('can clear sent attachments without revoking object URLs', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    useComposerStore.getState().addAttachmentFiles([new File(['img'], 'a.png', { type: 'image/png' })])

    useComposerStore.getState().clearAttachments(false)

    expect(useComposerStore.getState().attachments).toEqual([])
    expect(revokeObjectURL).not.toHaveBeenCalled()
  })

  it('stores upload status and errors per attachment', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')
    useComposerStore.getState().addAttachmentFiles([new File(['img'], 'a.png', { type: 'image/png' })])
    const attachmentId = useComposerStore.getState().attachments[0].id

    useComposerStore.getState().setAttachmentStatus(attachmentId, 'error', 'too large')

    expect(useComposerStore.getState().attachments[0]).toMatchObject({
      status: 'error',
      error: 'too large',
    })
  })

  it('adds text attachments with kind text and no preview URL', () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:should-not-be-called')
    const file = new File(['# title'], 'notes.md', { type: 'text/markdown' })

    useComposerStore.getState().addAttachmentFiles([file])

    const [attachment] = useComposerStore.getState().attachments
    // 文本附件不应创建 blob URL，避免无意义内存占用。
    expect(createObjectURL).not.toHaveBeenCalled()
    expect(attachment).toMatchObject({
      file,
      previewUrl: '',
      status: 'pending',
      kind: 'text',
    })
  })

  it('classifies text files by extension when browser omits MIME', () => {
    // 浏览器对 .csv/.md 等扩展名常返回空 MIME，应按扩展名兜底判定为 text。
    const file = new File(['a,b\n1,2'], 'data.csv', { type: '' })

    useComposerStore.getState().addAttachmentFiles([file])

    const [attachment] = useComposerStore.getState().attachments
    expect(attachment.kind).toBe('text')
    expect(attachment.previewUrl).toBe('')
  })

  it('keeps image attachments as kind image with preview URL', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')
    useComposerStore.getState().addAttachmentFiles([new File(['img'], 'a.png', { type: 'image/png' })])

    const [attachment] = useComposerStore.getState().attachments
    expect(attachment.kind).toBe('image')
    expect(attachment.previewUrl).toBe('blob:preview-1')
  })

  it('does not call revokeObjectURL when removing a text attachment', () => {
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    useComposerStore.getState().addAttachmentFiles([new File(['x'], 'a.md', { type: 'text/markdown' })])
    const attachmentId = useComposerStore.getState().attachments[0].id

    useComposerStore.getState().removeAttachment(attachmentId)

    expect(useComposerStore.getState().attachments).toEqual([])
    // 文本附件 previewUrl 为空，revokePreviewURL 会直接跳过。
    expect(revokeObjectURL).not.toHaveBeenCalled()
  })
})

describe('resolveAttachmentKind', () => {
  it('classifies image MIME as image', () => {
    expect(resolveAttachmentKind(new File([], 'a.png', { type: 'image/png' }))).toBe('image')
  })

  it('classifies text MIME as text', () => {
    expect(resolveAttachmentKind(new File([], 'a.md', { type: 'text/markdown' }))).toBe('text')
  })

  it('classifies by extension when MIME is empty', () => {
    expect(resolveAttachmentKind(new File([], 'data.csv', { type: '' }))).toBe('text')
  })

  it('returns unknown for unsupported types like PDF', () => {
    expect(resolveAttachmentKind(new File([], 'doc.pdf', { type: 'application/pdf' }))).toBe('unknown')
  })

  it('returns unknown for binary with no recognized extension', () => {
    expect(resolveAttachmentKind(new File([], 'archive.zip', { type: 'application/zip' }))).toBe('unknown')
  })
})
