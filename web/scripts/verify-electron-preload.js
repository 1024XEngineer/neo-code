import { spawnSync } from 'child_process'
import { existsSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const preloadPath = resolve(__dirname, '..', 'dist-electron', 'preload.cjs')

// 校验 preload 产物语法，避免损坏脚本进入 Electron 运行阶段。
function verifyElectronPreload() {
  if (!existsSync(preloadPath)) {
    console.error(`Electron preload not found: ${preloadPath}`)
    process.exit(1)
  }

  const result = spawnSync(process.execPath, ['--check', preloadPath], {
    stdio: 'inherit',
  })
  if (result.error) {
    console.error('Electron preload syntax check failed:', result.error.message)
    process.exit(1)
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1)
  }
}

verifyElectronPreload()
