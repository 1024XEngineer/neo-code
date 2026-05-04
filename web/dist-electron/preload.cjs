"use strict";
const electron = require("electron");
electron.contextBridge.exposeInMainWorld("electronAPI", {
  /** 获取认证 Token */
  getToken: () => electron.ipcRenderer.invoke("gateway:getToken"),
  /** 获取 Gateway 地址 */
  getAddress: () => electron.ipcRenderer.invoke("gateway:getAddress"),
  /** 获取当前工作区目录 */
  getWorkdir: () => electron.ipcRenderer.invoke("gateway:getWorkdir"),
  /** 选择新工作区目录并重启 Gateway */
  selectWorkdir: () => electron.ipcRenderer.invoke("gateway:selectWorkdir"),
  /** 窗口控制 */
  minimize: () => electron.ipcRenderer.invoke("window:minimize"),
  maximize: () => electron.ipcRenderer.invoke("window:maximize"),
  close: () => electron.ipcRenderer.invoke("window:close"),
  /** 监听主进程 Gateway 状态变更 */
  onGatewayStatus: (callback) => {
    const handler = (_event, data) => callback(data);
    electron.ipcRenderer.on("gateway:status", handler);
    return () => {
      electron.ipcRenderer.removeListener("gateway:status", handler);
    };
  }
});
