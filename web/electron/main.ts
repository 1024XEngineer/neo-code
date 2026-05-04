import { app, BrowserWindow, ipcMain, shell, dialog } from 'electron'
import { spawn, type ChildProcess } from 'child_process'
import { join, dirname } from 'path'
import { fileURLToPath } from 'url'
import { existsSync, readFileSync } from 'fs'
import { homedir } from 'os'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'

const __dirname = dirname(fileURLToPath(import.meta.url))

const DEFAULT_BASE_PORT = 8080
const MAX_PORT_ATTEMPTS = 10

let mainWindow: BrowserWindow | null = null
let gatewayProcess: ChildProcess | null = null
let gatewayReady = false
let gatewayAddress = ''
let gatewayToken = ''
let currentWorkdir = process.env['NEOCODE_WORKDIR'] ?? ''
let isQuitting = false

/** 安全发送 Gateway 状态，避免窗口销毁后访问 webContents 触发主进程异常 */
function sendGatewayStatus(data: { ready: boolean; error?: string }): void {
	if (isQuitting) return
	if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) return
	mainWindow.webContents.send('gateway:status', data)
}

/** 创建主窗口 */
function createWindow(): void {
	mainWindow = new BrowserWindow({
		width: 1200,
		height: 800,
		minWidth: 800,
		minHeight: 600,
		show: false,
		title: 'NeoCode',
		titleBarStyle: 'hiddenInset',
		webPreferences: {
			preload: join(__dirname, 'preload.cjs'),
			sandbox: false,
			contextIsolation: true,
			nodeIntegration: false,
		},
	})

	mainWindow.on('ready-to-show', () => {
		mainWindow?.show()
	})

	mainWindow.on('closed', () => {
		mainWindow = null
	})

	mainWindow.webContents.setWindowOpenHandler((details) => {
		shell.openExternal(details.url)
		return { action: 'deny' }
	})

	// Block unexpected in-page navigations that could lead to blank pages
	mainWindow.webContents.on('will-navigate', (event, url) => {
		const devUrl = process.env['ELECTRON_RENDERER_URL'] ?? ''
		const isDevServer = is.dev && devUrl !== '' && url.startsWith(devUrl)
		const isFileProtocol = url.startsWith('file://')

		if (isDevServer || isFileProtocol) {
			return // allow
		}

		event.preventDefault()
		console.warn(`[Electron] Blocked navigation to: ${url}`)
	})

	// Intercept browser back/forward to prevent navigating to invalid URLs
	mainWindow.on('app-command', (e, cmd) => {
		if (cmd === 'browser-backward' || cmd === 'browser-forward') {
			e.preventDefault()
		}
	})

	// 开发模式加载 Vite dev server，生产模式加载打包文件
	if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
		mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL'])
	} else {
		mainWindow.loadFile(join(__dirname, '../dist/index.html'))
	}
}

// ---- Gateway 进程管理 ----

/** 查找 Gateway 可执行文件路径 */
function findGatewayBinary(): string | null {
	const explicit = process.env['NEOCODE_GATEWAY_BIN']
	if (explicit) return explicit
	const candidates = [
		// 开发模式：build-gateway.js 的输出目录
		...(is.dev ? [
			join(__dirname, '..', 'build', 'neocode-gateway'),
			join(__dirname, '..', 'build', 'neocode-gateway.exe'),
		] : []),
		// 打包模式：resources 目录
		join(process.resourcesPath, 'neocode-gateway'),
		join(process.resourcesPath, 'neocode-gateway.exe'),
		// 打包模式：可执行文件同目录
		join(app.getPath('exe'), '..', 'neocode-gateway'),
		join(app.getPath('exe'), '..', 'neocode-gateway.exe'),
	]
	for (const p of candidates) {
		if (existsSync(p)) return p
	}
	return null
}

/** 从 ~/.neocode/auth.json 读取认证 token */
function loadGatewayToken(): string {
	try {
		const authPath = join(homedir(), '.neocode', 'auth.json')
		const raw = readFileSync(authPath, 'utf-8')
		const auth = JSON.parse(raw) as { token?: string }
		const token = auth.token ?? ''
		console.log(`[Electron] Loaded gateway token from ${authPath}: ${token ? `${token.slice(0, 8)}...` : '(empty)'}`)
		return token
	} catch (err) {
		console.warn(`[Electron] Failed to load gateway token:`, err)
		return ''
	}
}

/** 检测 Gateway 是否健康 */
async function checkHealthz(address: string): Promise<boolean> {
	const url = /^https?:\/\//i.test(address) ? address : `http://${address}`
	try {
		const res = await fetch(`${url}/healthz`, { signal: AbortSignal.timeout(2000) })
		return res.ok
	} catch {
		return false
	}
}

/** 等待 Gateway 健康检查通过 */
async function waitForHealthz(address: string, timeoutMs: number, intervalMs: number): Promise<boolean> {
	const url = /^https?:\/\//i.test(address) ? address : `http://${address}`
	const deadline = Date.now() + timeoutMs
	while (Date.now() < deadline) {
		try {
			const res = await fetch(`${url}/healthz`, { signal: AbortSignal.timeout(2000) })
			if (res.ok) return true
		} catch {
			// continue polling
		}
		await new Promise((r) => setTimeout(r, intervalMs))
	}
	return false
}

/** 从环境变量解析显式指定的端口 */
function findExplicitPort(): number | null {
	const envPort = process.env['NEOCODE_GATEWAY_PORT']
	if (envPort) {
		const p = parseInt(envPort, 10)
		if (p > 0 && p < 65536) return p
	}
	const envAddr = process.env['NEOCODE_GATEWAY']
	if (envAddr) {
		const m = envAddr.match(/:(\d+)$/)
		if (m) {
			const p = parseInt(m[1], 10)
			if (p > 0 && p < 65536) return p
		}
	}
	return null
}

/** 尝试在指定地址启动 Gateway */
async function tryStartGateway(binary: string, httpAddress: string): Promise<boolean> {
	console.log(`[Electron] Starting Gateway: ${binary} on ${httpAddress}`)
	const args = ['--http-listen', httpAddress]
	if (currentWorkdir) {
		args.push('--workdir', currentWorkdir)
	}
	const proc = spawn(binary, args, {
		detached: false,
		stdio: 'pipe',
	})
	gatewayProcess = proc

	proc.stdout?.on('data', (data: Buffer) => {
		console.log(`[Gateway stdout] ${data.toString().trim()}`)
	})
	proc.stderr?.on('data', (data: Buffer) => {
		console.error(`[Gateway stderr] ${data.toString().trim()}`)
	})
	proc.on('exit', (code) => {
		console.warn(`[Electron] Gateway exited with code ${code}`)
		if (gatewayProcess !== proc) return
		gatewayProcess = null
		gatewayReady = false
		sendGatewayStatus({
			ready: false,
			error: code === 0 ? 'Gateway process exited' : `Gateway process crashed (exit code ${code})`,
		})
	})

	const ready = await waitForHealthz(httpAddress, 15000, 500)
	if (ready) {
		gatewayAddress = httpAddress
		gatewayToken = loadGatewayToken()
		gatewayReady = true
		console.log(`[Electron] Gateway is ready at ${httpAddress}`)
	}
	return ready
}

/** 启动 Gateway 子进程并等待就绪（自动端口轮询） */
async function startGateway(): Promise<void> {
	const binary = findGatewayBinary()
	if (!binary) {
		console.warn('[Electron] Gateway binary not found, checking for external gateway')
		gatewayAddress = process.env['NEOCODE_GATEWAY'] ?? '127.0.0.1:8080'
		gatewayToken = process.env['NEOCODE_TOKEN'] ?? ''
		gatewayReady = await checkHealthz(gatewayAddress)
		if (!gatewayReady) {
			sendGatewayStatus({ ready: false, error: 'Gateway binary not found and no external gateway detected' })
		}
		return
	}

	const explicitPort = findExplicitPort()
	if (explicitPort !== null) {
		console.log(`[Electron] Using specified port ${explicitPort}`)
		const addr = `127.0.0.1:${explicitPort}`
		if (await checkHealthz(addr)) {
			console.log(`[Electron] Gateway already running at ${addr}`)
			gatewayAddress = addr
			gatewayToken = loadGatewayToken()
			gatewayReady = true
			return
		}
		if (await tryStartGateway(binary, addr)) return
		sendGatewayStatus({ ready: false, error: `Gateway failed to start on port ${explicitPort}` })
		return
	}

	for (let port = DEFAULT_BASE_PORT; port < DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS; port++) {
		const addr = `127.0.0.1:${port}`
		if (await checkHealthz(addr)) {
			console.log(`[Electron] Gateway already running at ${addr}`)
			gatewayAddress = addr
			gatewayToken = loadGatewayToken()
			gatewayReady = true
			return
		}
		console.log(`[Electron] Trying port ${port}...`)
		if (await tryStartGateway(binary, addr)) return
		if (gatewayProcess) {
			gatewayProcess.kill()
			gatewayProcess = null
		}
	}

	console.error(`[Electron] All ports ${DEFAULT_BASE_PORT}-${DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS - 1} are unavailable`)
	gatewayReady = false
	sendGatewayStatus({ ready: false, error: `All ports ${DEFAULT_BASE_PORT}-${DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS - 1} are unavailable` })
}

/** 停止 Gateway 子进程 */
function stopGateway(): void {
	if (gatewayProcess) {
		console.log('[Electron] Stopping Gateway')
		gatewayProcess.kill()
		gatewayProcess = null
	}
}

// ---- IPC 处理 ----

/** 获取认证 Token */
ipcMain.handle('gateway:getToken', () => {
	const token = gatewayToken || process.env['NEOCODE_TOKEN'] || ''
	console.log(`[Electron] IPC getToken → ${token ? `${token.slice(0, 8)}...` : '(empty)'}`)
	return token
})

/** 获取 Gateway 地址 */
ipcMain.handle('gateway:getAddress', () => {
	const addr = gatewayAddress || process.env['NEOCODE_GATEWAY'] || '127.0.0.1:8080'
	console.log(`[Electron] IPC getAddress → ${addr} (gatewayAddress=${gatewayAddress}, ready=${gatewayReady})`)
	return addr
})

/** 获取当前工作区目录 */
ipcMain.handle('gateway:getWorkdir', () => currentWorkdir)

/** 选择新工作区目录并重启 Gateway */
ipcMain.handle('gateway:selectWorkdir', async () => {
	if (!mainWindow) return { canceled: true, workdir: currentWorkdir }
	const result = await dialog.showOpenDialog(mainWindow, {
		properties: ['openDirectory'],
		defaultPath: currentWorkdir || app.getPath('home'),
	})
	if (result.canceled || result.filePaths.length === 0) {
		return { canceled: true, workdir: currentWorkdir }
	}
	const newWorkdir = result.filePaths[0]
	if (newWorkdir === currentWorkdir) {
		return { canceled: false, workdir: currentWorkdir }
	}
	currentWorkdir = newWorkdir
	console.log(`[Electron] Workdir changed to: ${currentWorkdir}`)
	// 重启 Gateway 以应用新工作区
	stopGateway()
	await startGateway()
	return { canceled: false, workdir: currentWorkdir }
})

/** 纯目录选择器，不修改 Gateway 工作目录 */
ipcMain.handle('dialog:pickDirectory', async () => {
	if (!mainWindow) return { canceled: true, filePaths: [] as string[] }
	const result = await dialog.showOpenDialog(mainWindow, {
		properties: ['openDirectory'],
		defaultPath: currentWorkdir || app.getPath('home'),
	})
	return { canceled: result.canceled, filePaths: result.filePaths }
})

/** 窗口控制 */
ipcMain.handle('window:minimize', () => mainWindow?.minimize())
ipcMain.handle('window:maximize', () => {
	if (mainWindow?.isMaximized()) {
		mainWindow.unmaximize()
	} else {
		mainWindow?.maximize()
	}
})
ipcMain.handle('window:close', () => mainWindow?.close())

// ---- App 生命周期 ----

app.whenReady().then(async () => {
	electronApp.setAppUserModelId('com.neocode.app')

	app.on('browser-window-created', (_, window) => {
		optimizer.watchWindowShortcuts(window)
	})

	await startGateway()
	createWindow()

	app.on('activate', () => {
		if (BrowserWindow.getAllWindows().length === 0) createWindow()
	})
})

app.on('before-quit', (event) => {
	if (gatewayProcess) {
		isQuitting = true
		event.preventDefault()
		gatewayProcess.on('exit', () => app.quit())
		gatewayProcess.kill()
	} else {
		isQuitting = true
		stopGateway()
	}
})

app.on('window-all-closed', () => {
	if (process.platform !== 'darwin') {
		app.quit()
	}
})
