import { contextBridge, ipcRenderer } from 'electron'

/** 暴露安全 API 到渲染进程 */
contextBridge.exposeInMainWorld('electronAPI', {
	/** 获取认证 Token */
	getToken: () => ipcRenderer.invoke('gateway:getToken'),

	/** 获取 Gateway 地址 */
	getAddress: () => ipcRenderer.invoke('gateway:getAddress'),

	/** 获取当前工作区目录 */
	getWorkdir: () => ipcRenderer.invoke('gateway:getWorkdir'),

	/** 选择新工作区目录并重启 Gateway */
	selectWorkdir: () => ipcRenderer.invoke('gateway:selectWorkdir') as Promise<{ canceled: boolean; workdir: string }>,

	/** 纯目录选择器（不重启 Gateway） */
	pickDirectory: () => ipcRenderer.invoke('dialog:pickDirectory') as Promise<{ canceled: boolean; filePaths: string[] }>,

	/** 窗口控制 */
	minimize: () => ipcRenderer.invoke('window:minimize'),
	maximize: () => ipcRenderer.invoke('window:maximize'),
	close: () => ipcRenderer.invoke('window:close'),

	/** 监听主进程 Gateway 状态变更 */
	onGatewayStatus: (callback: (data: { ready: boolean; error?: string }) => void) => {
		const handler = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data as { ready: boolean; error?: string })
		ipcRenderer.on('gateway:status', handler)
		return () => { ipcRenderer.removeListener('gateway:status', handler) }
	},

	/** 监听更新可用事件 */
	onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
		const handler = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data as { version: string; releaseNotes?: string })
		ipcRenderer.on('updater:available', handler)
		return () => { ipcRenderer.removeListener('updater:available', handler) }
	},

	/** 监听更新下载完成事件 */
	onUpdateDownloaded: (callback: (info: { version: string }) => void) => {
		const handler = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data as { version: string })
		ipcRenderer.on('updater:downloaded', handler)
		return () => { ipcRenderer.removeListener('updater:downloaded', handler) }
	},

	/** 退出并安装更新 */
	quitAndInstall: () => ipcRenderer.invoke('updater:quitAndInstall'),
})
