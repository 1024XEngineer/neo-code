import { spawnSync } from 'child_process'
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'fs'
import { tmpdir } from 'os'
import { dirname, join, resolve } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rootDir = resolve(__dirname, '..')
const buildDir = join(rootDir, 'build')
const sourceIcon = join(buildDir, 'icon.png')
const outputIco = join(buildDir, 'icon.ico')
const outputIcns = join(buildDir, 'icon.icns')
const iconSizes = [16, 32, 48, 64, 128, 256, 512, 1024]

if (!existsSync(sourceIcon)) {
	throw new Error(`Source icon not found: ${sourceIcon}`)
}

const tempDir = mkdtempSync(join(tmpdir(), 'neocode-icons-'))

try {
	const resizedPngs = resizeImages(sourceIcon, tempDir, iconSizes)
	if (resizedPngs === null) {
		console.log(`Existing ${outputIco} and ${outputIcns} are up to date.`)
	} else {
		writeFileSync(outputIco, buildIco(resizedPngs))
		writeFileSync(outputIcns, buildIcns(resizedPngs))
		console.log(`Generated ${outputIco}`)
		console.log(`Generated ${outputIcns}`)
	}
} finally {
	rmSync(tempDir, { recursive: true, force: true })
}

/** resizeImages 根据当前系统选择可用的本地图像缩放工具。 */
function resizeImages(inputPath, outputDir, sizes) {
	if (process.platform === 'win32') {
		return resizeWithPowerShell(inputPath, outputDir, sizes)
	}
	if (process.platform === 'darwin' && commandExists('sips')) {
		return resizeWithSips(inputPath, outputDir, sizes)
	}
	if (commandExists('magick')) {
		return resizeWithMagick(inputPath, outputDir, sizes)
	}
	if (outputsAreFresh()) {
		console.warn('No local image resizer found; existing icon.ico and icon.icns are up to date.')
		return null
	}
	throw new Error('No local image resizer found. Install ImageMagick or run this script on Windows/macOS.')
}

/** resizeWithPowerShell 使用系统图像能力生成多尺寸 PNG，避免引入额外 npm 依赖。 */
function resizeWithPowerShell(inputPath, outputDir, sizes) {
	const script = `
Add-Type -AssemblyName System.Drawing
$source = [System.Drawing.Image]::FromFile('${escapePowerShell(inputPath)}')
try {
	$sizes = @(${sizes.join(',')})
	foreach ($size in $sizes) {
		$bitmap = New-Object System.Drawing.Bitmap $size, $size
		$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
		try {
			$graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
			$graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
			$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
			$graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
			$graphics.Clear([System.Drawing.Color]::Transparent)
			$graphics.DrawImage($source, 0, 0, $size, $size)
			$bitmap.Save((Join-Path '${escapePowerShell(outputDir)}' "$size.png"), [System.Drawing.Imaging.ImageFormat]::Png)
		} finally {
			$graphics.Dispose()
			$bitmap.Dispose()
		}
	}
} finally {
	$source.Dispose()
}
`
	const result = spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', script], {
		encoding: 'utf8',
		stdio: ['ignore', 'pipe', 'pipe'],
	})
	if (result.status !== 0) {
		throw new Error(`Icon resize failed:\n${result.stderr || result.stdout}`)
	}
	return new Map(sizes.map((size) => [size, readFileSync(join(outputDir, `${size}.png`))]))
}

/** resizeWithSips 使用 macOS 内置 sips 生成多尺寸 PNG。 */
function resizeWithSips(inputPath, outputDir, sizes) {
	for (const size of sizes) {
		const result = spawnSync('sips', ['-z', String(size), String(size), inputPath, '--out', join(outputDir, `${size}.png`)], {
			encoding: 'utf8',
			stdio: ['ignore', 'pipe', 'pipe'],
		})
		if (result.status !== 0) {
			throw new Error(`Icon resize failed:\n${result.stderr || result.stdout}`)
		}
	}
	return new Map(sizes.map((size) => [size, readFileSync(join(outputDir, `${size}.png`))]))
}

/** resizeWithMagick 使用 ImageMagick 作为 Linux 等环境的可选后备方案。 */
function resizeWithMagick(inputPath, outputDir, sizes) {
	for (const size of sizes) {
		const result = spawnSync('magick', [inputPath, '-resize', `${size}x${size}`, join(outputDir, `${size}.png`)], {
			encoding: 'utf8',
			stdio: ['ignore', 'pipe', 'pipe'],
		})
		if (result.status !== 0) {
			throw new Error(`Icon resize failed:\n${result.stderr || result.stdout}`)
		}
	}
	return new Map(sizes.map((size) => [size, readFileSync(join(outputDir, `${size}.png`))]))
}

/** buildIco 将多尺寸 PNG 封装为 Windows ICO 文件。 */
function buildIco(pngs) {
	const entries = iconSizes.filter((size) => size <= 256)
	const headerSize = 6 + entries.length * 16
	let offset = headerSize
	const header = Buffer.alloc(headerSize)

	header.writeUInt16LE(0, 0)
	header.writeUInt16LE(1, 2)
	header.writeUInt16LE(entries.length, 4)

	entries.forEach((size, index) => {
		const image = pngs.get(size)
		const entryOffset = 6 + index * 16
		header.writeUInt8(size === 256 ? 0 : size, entryOffset)
		header.writeUInt8(size === 256 ? 0 : size, entryOffset + 1)
		header.writeUInt8(0, entryOffset + 2)
		header.writeUInt8(0, entryOffset + 3)
		header.writeUInt16LE(1, entryOffset + 4)
		header.writeUInt16LE(32, entryOffset + 6)
		header.writeUInt32LE(image.length, entryOffset + 8)
		header.writeUInt32LE(offset, entryOffset + 12)
		offset += image.length
	})

	return Buffer.concat([header, ...entries.map((size) => pngs.get(size))])
}

/** buildIcns 将 PNG 数据封装为 macOS ICNS 文件，供 electron-builder 直接使用。 */
function buildIcns(pngs) {
	const chunks = [
		['icp4', 16],
		['icp5', 32],
		['icp6', 64],
		['ic07', 128],
		['ic08', 256],
		['ic09', 512],
		['ic10', 1024],
		['ic11', 32],
		['ic12', 64],
		['ic13', 256],
		['ic14', 512],
	]
	const body = chunks.map(([type, size]) => {
		const image = pngs.get(size)
		const header = Buffer.alloc(8)
		header.write(type, 0, 4, 'ascii')
		header.writeUInt32BE(image.length + 8, 4)
		return Buffer.concat([header, image])
	})
	const totalLength = 8 + body.reduce((sum, chunk) => sum + chunk.length, 0)
	const header = Buffer.alloc(8)
	header.write('icns', 0, 4, 'ascii')
	header.writeUInt32BE(totalLength, 4)
	return Buffer.concat([header, ...body])
}

/** escapePowerShell 转义路径中的单引号，保证脚本按字面值读取文件。 */
function escapePowerShell(value) {
	return value.replace(/'/g, "''")
}

/** commandExists 检查命令是否存在，避免直接执行时报出不清晰的错误。 */
function commandExists(command) {
	const probe = process.platform === 'win32'
		? spawnSync('where.exe', [command], { stdio: 'ignore' })
		: spawnSync('which', [command], { stdio: 'ignore' })
	return probe.status === 0
}

/** outputsAreFresh 判断已提交图标是否不旧于源图，可在缺少缩放工具时复用。 */
function outputsAreFresh() {
	if (!existsSync(outputIco) || !existsSync(outputIcns)) return false
	const sourceMtime = statSync(sourceIcon).mtimeMs
	return statSync(outputIco).mtimeMs >= sourceMtime && statSync(outputIcns).mtimeMs >= sourceMtime
}
