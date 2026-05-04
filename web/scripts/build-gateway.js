import { spawnSync } from 'child_process'
import { existsSync, mkdirSync } from 'fs'
import { join, resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import { platform, arch } from 'os'

const targetMap = {
  win32: { x64: 'windows/amd64', ia32: 'windows/386', arm64: 'windows/arm64' },
  darwin: { x64: 'darwin/amd64', arm64: 'darwin/arm64' },
  linux: { x64: 'linux/amd64', arm64: 'linux/arm64' },
}

const currentPlatform = platform()
const currentArch = arch()
const goosGoarch = targetMap[currentPlatform]?.[currentArch]

if (!goosGoarch) {
  console.error(`Unsupported platform/arch: ${currentPlatform}/${currentArch}`)
  process.exit(1)
}

const [goos, goarch] = goosGoarch.split('/')
const __dirname = dirname(fileURLToPath(import.meta.url))
const projectRoot = resolve(__dirname, '..', '..')
const outputDir = resolve(__dirname, '..', 'build')
const binaryName = goos === 'windows' ? 'neocode-gateway.exe' : 'neocode-gateway'
const outputPath = join(outputDir, binaryName)

if (!existsSync(outputDir)) {
  mkdirSync(outputDir, { recursive: true })
}

console.log(`Building neocode-gateway for ${goos}/${goarch}...`)

const result = spawnSync('go', ['build', '-a', '-o', outputPath, './cmd/neocode-gateway'], {
  cwd: projectRoot,
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
  stdio: 'inherit',
})

if (result.error) {
  console.error('Go build failed:', result.error.message)
  process.exit(1)
}
if (result.status !== 0) {
  console.error('Go build failed')
  process.exit(result.status ?? 1)
}

console.log(`Built: ${outputPath}`)
