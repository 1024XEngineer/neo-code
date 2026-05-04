import { describe, it, expect, afterEach } from 'vitest'
import { detectRuntimeMode } from './RuntimeProvider'

const originalUserAgent = navigator.userAgent

function setUserAgent(value: string) {
  Object.defineProperty(window.navigator, 'userAgent', {
    value,
    configurable: true,
  })
}

function setElectronAPI(value: Window['electronAPI']) {
  Object.defineProperty(window, 'electronAPI', {
    value,
    configurable: true,
    writable: true,
  })
}

afterEach(() => {
  setUserAgent(originalUserAgent)
  setElectronAPI(undefined)
})

describe('detectRuntimeMode', () => {
  it('uses electron mode when preload exposes electronAPI', () => {
    setUserAgent('Mozilla/5.0 Chrome/120 Safari/537.36')
    setElectronAPI({} as Window['electronAPI'])

    expect(detectRuntimeMode()).toBe('electron')
  })

  it('keeps electron mode when Electron UA is present but preload failed', () => {
    setUserAgent('Mozilla/5.0 Chrome/120 Safari/537.36 Electron/33.2.0')
    setElectronAPI(undefined)

    expect(detectRuntimeMode()).toBe('electron')
  })

  it('uses browser mode outside Electron', () => {
    setUserAgent('Mozilla/5.0 Chrome/120 Safari/537.36')
    setElectronAPI(undefined)

    expect(detectRuntimeMode()).toBe('browser')
  })
})
