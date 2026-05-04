import { existsSync, rmSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const outputDir = resolve(__dirname, '..', 'dist-electron')

// 清理 Electron 构建产物，避免损坏或残留文件被开发模式直接加载。
function cleanElectronOutput() {
  if (!existsSync(outputDir)) {
    console.log(`Electron output already clean: ${outputDir}`)
    return
  }
  rmSync(outputDir, { recursive: true, force: true })
  console.log(`Cleaned Electron output: ${outputDir}`)
}

cleanElectronOutput()
