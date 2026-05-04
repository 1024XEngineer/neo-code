import { app, session, ipcMain, BrowserWindow, shell, dialog } from "electron";
import { spawn } from "child_process";
import { dirname, join } from "path";
import { fileURLToPath } from "url";
import { existsSync, readFileSync } from "fs";
import { homedir } from "os";
const is = {
  dev: !app.isPackaged
};
const platform = {
  isWindows: process.platform === "win32",
  isMacOS: process.platform === "darwin",
  isLinux: process.platform === "linux"
};
const electronApp = {
  setAppUserModelId(id) {
    if (platform.isWindows)
      app.setAppUserModelId(is.dev ? process.execPath : id);
  },
  setAutoLaunch(auto) {
    if (platform.isLinux)
      return false;
    const isOpenAtLogin = () => {
      return app.getLoginItemSettings().openAtLogin;
    };
    if (isOpenAtLogin() !== auto) {
      app.setLoginItemSettings({ openAtLogin: auto });
      return isOpenAtLogin() === auto;
    } else {
      return true;
    }
  },
  skipProxy() {
    return session.defaultSession.setProxy({ mode: "direct" });
  }
};
const optimizer = {
  watchWindowShortcuts(window, shortcutOptions) {
    if (!window)
      return;
    const { webContents } = window;
    const { escToCloseWindow = false, zoom = false } = shortcutOptions || {};
    webContents.on("before-input-event", (event, input) => {
      if (input.type === "keyDown") {
        if (!is.dev) {
          if (input.code === "KeyR" && (input.control || input.meta))
            event.preventDefault();
          if (input.code === "KeyI" && (input.alt && input.meta || input.control && input.shift)) {
            event.preventDefault();
          }
        } else {
          if (input.code === "F12") {
            if (webContents.isDevToolsOpened()) {
              webContents.closeDevTools();
            } else {
              webContents.openDevTools({ mode: "undocked" });
              console.log("Open dev tool...");
            }
          }
        }
        if (escToCloseWindow) {
          if (input.code === "Escape" && input.key !== "Process") {
            window.close();
            event.preventDefault();
          }
        }
        if (!zoom) {
          if (input.code === "Minus" && (input.control || input.meta))
            event.preventDefault();
          if (input.code === "Equal" && input.shift && (input.control || input.meta))
            event.preventDefault();
        }
      }
    });
  },
  registerFramelessWindowIpc() {
    ipcMain.on("win:invoke", (event, action) => {
      const win = BrowserWindow.fromWebContents(event.sender);
      if (win) {
        if (action === "show") {
          win.show();
        } else if (action === "showInactive") {
          win.showInactive();
        } else if (action === "min") {
          win.minimize();
        } else if (action === "max") {
          const isMaximized = win.isMaximized();
          if (isMaximized) {
            win.unmaximize();
          } else {
            win.maximize();
          }
        } else if (action === "close") {
          win.close();
        }
      }
    });
  }
};
const __dirname$1 = dirname(fileURLToPath(import.meta.url));
const DEFAULT_BASE_PORT = 8080;
const MAX_PORT_ATTEMPTS = 10;
let mainWindow = null;
let gatewayProcess = null;
let gatewayReady = false;
let gatewayAddress = "";
let gatewayToken = "";
let currentWorkdir = process.env["NEOCODE_WORKDIR"] ?? app.getPath("home");
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 800,
    minHeight: 600,
    show: false,
    title: "NeoCode",
    titleBarStyle: "hiddenInset",
    webPreferences: {
      preload: join(__dirname$1, "preload.cjs"),
      sandbox: false,
      contextIsolation: true,
      nodeIntegration: false
    }
  });
  mainWindow.on("ready-to-show", () => {
    mainWindow == null ? void 0 : mainWindow.show();
  });
  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url);
    return { action: "deny" };
  });
  mainWindow.webContents.on("will-navigate", (event, url) => {
    const devUrl = process.env["ELECTRON_RENDERER_URL"] ?? "";
    const isDevServer = is.dev && devUrl !== "" && url.startsWith(devUrl);
    const isFileProtocol = url.startsWith("file://");
    if (isDevServer || isFileProtocol) {
      return;
    }
    event.preventDefault();
    console.warn(`[Electron] Blocked navigation to: ${url}`);
  });
  mainWindow.on("app-command", (e, cmd) => {
    if (cmd === "browser-backward" || cmd === "browser-forward") {
      e.preventDefault();
    }
  });
  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    mainWindow.loadFile(join(__dirname$1, "../dist/index.html"));
  }
}
function findGatewayBinary() {
  const explicit = process.env["NEOCODE_GATEWAY_BIN"];
  if (explicit) return explicit;
  const candidates = [
    // 开发模式：build-gateway.js 的输出目录
    ...is.dev ? [
      join(__dirname$1, "..", "build", "neocode-gateway"),
      join(__dirname$1, "..", "build", "neocode-gateway.exe")
    ] : [],
    // 打包模式：resources 目录
    join(process.resourcesPath, "neocode-gateway"),
    join(process.resourcesPath, "neocode-gateway.exe"),
    // 打包模式：可执行文件同目录
    join(app.getPath("exe"), "..", "neocode-gateway"),
    join(app.getPath("exe"), "..", "neocode-gateway.exe")
  ];
  for (const p of candidates) {
    if (existsSync(p)) return p;
  }
  return null;
}
function loadGatewayToken() {
  try {
    const authPath = join(homedir(), ".neocode", "auth.json");
    const raw = readFileSync(authPath, "utf-8");
    const auth = JSON.parse(raw);
    const token = auth.token ?? "";
    console.log(`[Electron] Loaded gateway token from ${authPath}: ${token ? `${token.slice(0, 8)}...` : "(empty)"}`);
    return token;
  } catch (err) {
    console.warn(`[Electron] Failed to load gateway token:`, err);
    return "";
  }
}
async function checkHealthz(address) {
  const url = /^https?:\/\//i.test(address) ? address : `http://${address}`;
  try {
    const res = await fetch(`${url}/healthz`, { signal: AbortSignal.timeout(2e3) });
    return res.ok;
  } catch {
    return false;
  }
}
async function waitForHealthz(address, timeoutMs, intervalMs) {
  const url = /^https?:\/\//i.test(address) ? address : `http://${address}`;
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/healthz`, { signal: AbortSignal.timeout(2e3) });
      if (res.ok) return true;
    } catch {
    }
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  return false;
}
function findExplicitPort() {
  const envPort = process.env["NEOCODE_GATEWAY_PORT"];
  if (envPort) {
    const p = parseInt(envPort, 10);
    if (p > 0 && p < 65536) return p;
  }
  const envAddr = process.env["NEOCODE_GATEWAY"];
  if (envAddr) {
    const m = envAddr.match(/:(\d+)$/);
    if (m) {
      const p = parseInt(m[1], 10);
      if (p > 0 && p < 65536) return p;
    }
  }
  return null;
}
async function tryStartGateway(binary, httpAddress) {
  var _a, _b;
  console.log(`[Electron] Starting Gateway: ${binary} on ${httpAddress}`);
  const proc = spawn(binary, ["--http-listen", httpAddress, "--workdir", currentWorkdir], {
    detached: false,
    stdio: "pipe"
  });
  gatewayProcess = proc;
  (_a = proc.stdout) == null ? void 0 : _a.on("data", (data) => {
    console.log(`[Gateway stdout] ${data.toString().trim()}`);
  });
  (_b = proc.stderr) == null ? void 0 : _b.on("data", (data) => {
    console.error(`[Gateway stderr] ${data.toString().trim()}`);
  });
  proc.on("exit", (code) => {
    console.warn(`[Electron] Gateway exited with code ${code}`);
    if (gatewayProcess !== proc) return;
    gatewayProcess = null;
    gatewayReady = false;
    mainWindow == null ? void 0 : mainWindow.webContents.send("gateway:status", {
      ready: false,
      error: code === 0 ? "Gateway process exited" : `Gateway process crashed (exit code ${code})`
    });
  });
  const ready = await waitForHealthz(httpAddress, 15e3, 500);
  if (ready) {
    gatewayAddress = httpAddress;
    gatewayToken = loadGatewayToken();
    gatewayReady = true;
    console.log(`[Electron] Gateway is ready at ${httpAddress}`);
  }
  return ready;
}
async function startGateway() {
  const binary = findGatewayBinary();
  if (!binary) {
    console.warn("[Electron] Gateway binary not found, checking for external gateway");
    gatewayAddress = process.env["NEOCODE_GATEWAY"] ?? "127.0.0.1:8080";
    gatewayToken = process.env["NEOCODE_TOKEN"] ?? "";
    gatewayReady = await checkHealthz(gatewayAddress);
    if (!gatewayReady) {
      mainWindow == null ? void 0 : mainWindow.webContents.send("gateway:status", { ready: false, error: "Gateway binary not found and no external gateway detected" });
    }
    return;
  }
  const explicitPort = findExplicitPort();
  if (explicitPort !== null) {
    console.log(`[Electron] Using specified port ${explicitPort}`);
    const addr = `127.0.0.1:${explicitPort}`;
    if (await checkHealthz(addr)) {
      console.log(`[Electron] Gateway already running at ${addr}`);
      gatewayAddress = addr;
      gatewayToken = loadGatewayToken();
      gatewayReady = true;
      return;
    }
    if (await tryStartGateway(binary, addr)) return;
    mainWindow == null ? void 0 : mainWindow.webContents.send("gateway:status", { ready: false, error: `Gateway failed to start on port ${explicitPort}` });
    return;
  }
  for (let port = DEFAULT_BASE_PORT; port < DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS; port++) {
    const addr = `127.0.0.1:${port}`;
    if (await checkHealthz(addr)) {
      console.log(`[Electron] Gateway already running at ${addr}`);
      gatewayAddress = addr;
      gatewayToken = loadGatewayToken();
      gatewayReady = true;
      return;
    }
    console.log(`[Electron] Trying port ${port}...`);
    if (await tryStartGateway(binary, addr)) return;
    if (gatewayProcess) {
      gatewayProcess.kill();
      gatewayProcess = null;
    }
  }
  console.error(`[Electron] All ports ${DEFAULT_BASE_PORT}-${DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS - 1} are unavailable`);
  gatewayReady = false;
  mainWindow == null ? void 0 : mainWindow.webContents.send("gateway:status", { ready: false, error: `All ports ${DEFAULT_BASE_PORT}-${DEFAULT_BASE_PORT + MAX_PORT_ATTEMPTS - 1} are unavailable` });
}
function stopGateway() {
  if (gatewayProcess) {
    console.log("[Electron] Stopping Gateway");
    gatewayProcess.kill();
    gatewayProcess = null;
  }
}
ipcMain.handle("gateway:getToken", () => {
  const token = gatewayToken || process.env["NEOCODE_TOKEN"] || "";
  console.log(`[Electron] IPC getToken → ${token ? `${token.slice(0, 8)}...` : "(empty)"}`);
  return token;
});
ipcMain.handle("gateway:getAddress", () => {
  const addr = gatewayAddress || process.env["NEOCODE_GATEWAY"] || "127.0.0.1:8080";
  console.log(`[Electron] IPC getAddress → ${addr} (gatewayAddress=${gatewayAddress}, ready=${gatewayReady})`);
  return addr;
});
ipcMain.handle("gateway:getWorkdir", () => currentWorkdir);
ipcMain.handle("gateway:selectWorkdir", async () => {
  if (!mainWindow) return { canceled: true, workdir: currentWorkdir };
  const result = await dialog.showOpenDialog(mainWindow, {
    properties: ["openDirectory"],
    defaultPath: currentWorkdir
  });
  if (result.canceled || result.filePaths.length === 0) {
    return { canceled: true, workdir: currentWorkdir };
  }
  const newWorkdir = result.filePaths[0];
  if (newWorkdir === currentWorkdir) {
    return { canceled: false, workdir: currentWorkdir };
  }
  currentWorkdir = newWorkdir;
  console.log(`[Electron] Workdir changed to: ${currentWorkdir}`);
  stopGateway();
  await startGateway();
  return { canceled: false, workdir: currentWorkdir };
});
ipcMain.handle("window:minimize", () => mainWindow == null ? void 0 : mainWindow.minimize());
ipcMain.handle("window:maximize", () => {
  if (mainWindow == null ? void 0 : mainWindow.isMaximized()) {
    mainWindow.unmaximize();
  } else {
    mainWindow == null ? void 0 : mainWindow.maximize();
  }
});
ipcMain.handle("window:close", () => mainWindow == null ? void 0 : mainWindow.close());
app.whenReady().then(async () => {
  electronApp.setAppUserModelId("com.neocode.app");
  app.on("browser-window-created", (_, window) => {
    optimizer.watchWindowShortcuts(window);
  });
  await startGateway();
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});
app.on("before-quit", (event) => {
  if (gatewayProcess) {
    event.preventDefault();
    gatewayProcess.on("exit", () => app.quit());
    gatewayProcess.kill();
  } else {
    stopGateway();
  }
});
app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
