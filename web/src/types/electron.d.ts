/** Electron preload 暴露的 API 类型 */
export interface ElectronAPI {
  getToken: () => Promise<string>
  getAddress: () => Promise<string>
  getWorkdir: () => Promise<string>
  selectWorkdir: () => Promise<{ canceled: boolean; workdir: string }>
  pickDirectory: () => Promise<{ canceled: boolean; filePaths: string[] }>
  minimize: () => Promise<void>
  maximize: () => Promise<void>
  close: () => Promise<void>
  onGatewayStatus: (callback: (data: { ready: boolean; error?: string }) => void) => () => void
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => () => void
  onUpdateDownloaded: (callback: (info: { version: string }) => void) => () => void
  quitAndInstall: () => Promise<void>
}

declare global {
  interface Window {
    electronAPI?: ElectronAPI
  }
}
