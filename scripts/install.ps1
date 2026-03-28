$ErrorActionPreference = "Stop"

# 配置仓库信息
$Repo = "pionxe/neo-code"
$ProjectName = "neo-code"
$BinaryName = "neocode.exe"

Write-Host "🚀 开始安装 $BinaryName..." -ForegroundColor Cyan

# 1. 获取系统架构
$Arch = $env:PROCESSOR_ARCHITECTURE
if ($Arch -eq "AMD64") {
    $ArchName = "x86_64"
} elseif ($Arch -eq "ARM64") {
    $ArchName = "arm64"
} else {
    Write-Error "❌ 不支持的系统架构: $Arch"
    exit
}

# 2. 从 GitHub API 获取最新 Release 版本号
Write-Host "🔍 正在获取最新版本信息..."
$ApiUrl = "https://api.github.com/repos/$Repo/releases/latest"
try {
    $LatestRelease = Invoke-RestMethod -Uri $ApiUrl
    $LatestTag = $LatestRelease.tag_name
} catch {
    Write-Error "❌ 无法获取最新版本，请检查网络或 GitHub 访问权限。"
    exit
}
Write-Host "📦 发现最新版本: $LatestTag"

# 3. 拼接下载链接
$ZipFile = "${ProjectName}_Windows_${ArchName}.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$LatestTag/$ZipFile"

# 4. 下载并解压到临时目录
$TempDir = Join-Path $env:TEMP "neocode_install"
if (Test-Path $TempDir) { Remove-Item -Recurse -Force $TempDir }
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
$ZipPath = Join-Path $TempDir $ZipFile

Write-Host "⬇️  正在下载压缩包..."
Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath

Write-Host "📦 正在解压..."
Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

# 5. 部署到用户目录
$InstallDir = Join-Path $env:LOCALAPPDATA "NeoCode"
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}
Write-Host "⚙️  正在将可执行文件部署到 $InstallDir..."
Copy-Item -Path (Join-Path $TempDir $BinaryName) -Destination $InstallDir -Force

# 6. 配置环境变量 PATH
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notmatch [regex]::Escape($InstallDir)) {
    Write-Host "🔧 正在配置环境变量..."
    $NewPath = "$UserPath;$InstallDir"
    [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
    Write-Host "⚠️  环境变量已更新！安装完成后，请重启终端(PowerShell/CMD)以使命令生效。" -ForegroundColor Yellow
}

# 7. 清理临时文件
Remove-Item -Recurse -Force $TempDir

Write-Host "✅ 安装成功！" -ForegroundColor Green