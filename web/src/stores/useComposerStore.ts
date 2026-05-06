import { create } from 'zustand'

interface ComposerState {
  composerText: string
  setComposerText: (text: string) => void
}

export const useComposerStore = create<ComposerState>((set) => ({
  composerText: '',
  setComposerText: (composerText) => set({ composerText }),
}))
