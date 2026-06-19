import { create } from 'zustand'

export const acceptedImageMimeTypes = ['image/png', 'image/jpeg', 'image/webp'] as const
// 文本附件白名单，与后端 session.DefaultTextAssetWhitelist 保持一致（txt/md/json/yaml/yml/csv）。
export const acceptedTextMimeTypes = [
  'text/plain',
  'text/markdown',
  'application/json',
  'text/yaml',
  'application/x-yaml',
  'text/csv',
] as const
// 浏览器对部分文本扩展名（如 .md/.csv）可能不返回 MIME，需要按扩展名兜底校验。
export const acceptedTextExtensions = ['.txt', '.md', '.json', '.yaml', '.yml', '.csv'] as const
export const maxComposerAttachmentBytes = 20 * 1024 * 1024
// 文本附件字节上限，与后端 session.DefaultMaxTextAssetBytes（256 KiB）对齐，避免无效上传。
export const maxTextAttachmentBytes = 256 * 1024

export type AttachmentKind = 'image' | 'text'
// ResolvedAttachmentKind 用于文件校验阶段的三路判定，unknown 表示既非图片也非文本。
export type ResolvedAttachmentKind = AttachmentKind | 'unknown'

export interface ComposerAttachment {
  id: string
  file: File
  // 图片附件用 blob URL 做缩略图预览；文本附件不创建 blob URL，留空字符串。
  previewUrl: string
  status: 'pending' | 'uploading' | 'uploaded' | 'error'
  error?: string
  // 附件类别，由 createComposerAttachment 按 MIME/扩展名判定，供预览与展示分流使用。
  kind: AttachmentKind
}

interface ComposerState {
  composerText: string
  attachments: ComposerAttachment[]
  setComposerText: (text: string) => void
  addAttachmentFiles: (files: File[]) => void
  removeAttachment: (id: string) => void
  clearAttachments: (revoke?: boolean) => void
  setAttachmentStatus: (id: string, status: ComposerAttachment['status'], error?: string) => void
}

export const useComposerStore = create<ComposerState>((set) => ({
  composerText: '',
  attachments: [],
  setComposerText: (composerText) => set({ composerText }),
  addAttachmentFiles: (files) => set((state) => ({
    attachments: [
      ...state.attachments,
      ...files.map(createComposerAttachment),
    ],
  })),
  removeAttachment: (id) => set((state) => {
    const target = state.attachments.find((attachment) => attachment.id === id)
    revokePreviewURL(target?.previewUrl)
    return { attachments: state.attachments.filter((attachment) => attachment.id !== id) }
  }),
  clearAttachments: (revoke = true) => set((state) => {
    if (revoke) state.attachments.forEach((attachment) => revokePreviewURL(attachment.previewUrl))
    return { attachments: [] }
  }),
  setAttachmentStatus: (id, status, error) => set((state) => ({
    attachments: state.attachments.map((attachment) => (
      attachment.id === id ? { ...attachment, status, error } : attachment
    )),
  })),
}))

function createComposerAttachment(file: File): ComposerAttachment {
  const resolved = resolveAttachmentKind(file)
  // unknown 类型在 handleFilesSelected 校验阶段已被拒绝，此处仅做类型收窄（不可达分支）。
  const kind: AttachmentKind = resolved === 'text' ? 'text' : 'image'
  return {
    id: `att_${Date.now()}_${Math.random().toString(36).slice(2)}`,
    file,
    // 文本附件不创建 blob URL：避免无意义内存占用，文本预览由 chip（文件名+大小）承载。
    previewUrl: kind === 'image' ? createPreviewURL(file) : '',
    status: 'pending',
    kind,
  }
}

// resolveAttachmentKind 按浏览器声明的 MIME 判定附件类别；MIME 缺失时按扩展名兜底。
// 图片 MIME 优先判定为 image，文本白名单内的扩展名/MIME 判定为 text，其余返回 unknown。
export function resolveAttachmentKind(file: File): ResolvedAttachmentKind {
  const mime = (file.type || '').toLowerCase()
  if (mime.startsWith('image/')) return 'image'
  if ((acceptedTextMimeTypes as readonly string[]).includes(mime)) return 'text'
  // 浏览器对 .md/.csv 等扩展名常返回空 MIME 或 application/octet-stream，按扩展名兜底。
  const name = (file.name || '').toLowerCase()
  if (acceptedTextExtensions.some((ext) => name.endsWith(ext))) return 'text'
  return 'unknown'
}

function createPreviewURL(file: File) {
  if (typeof URL !== 'undefined' && typeof URL.createObjectURL === 'function') {
    return URL.createObjectURL(file)
  }
  return ''
}

function revokePreviewURL(url?: string) {
  if (!url || typeof URL === 'undefined' || typeof URL.revokeObjectURL !== 'function') return
  URL.revokeObjectURL(url)
}
