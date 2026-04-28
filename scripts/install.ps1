param(
	[string]$Flavor = "full",
	[switch]$DryRun
)

$ErrorActionPreference = "Stop"

$Repo = "1024XEngineer/neo-code"
$Flavor = $Flavor.ToLowerInvariant()
if ($Flavor -notin @("full", "gateway")) {
	throw "Unsupported -Flavor value: $Flavor (expected full|gateway)"
}

switch ($Flavor) {
	"full" {
		$AssetPrefix = "neocode"
		$BinaryName = "neocode.exe"
	}
	"gateway" {
		$AssetPrefix = "neocode-gateway"
		$BinaryName = "neocode-gateway.exe"
	}
}

# 1. 识别物理架构（优先考虑 64 位重定向环境）
$RawArch = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrWhiteSpace($RawArch)) {
    $RawArch = $env:PROCESSOR_ARCHITECTURE
}

# 2. 跨平台补丁：如果环境变量拿不到（如 CI 环境），尝试 .NET 方法
if ([string]::IsNullOrWhiteSpace($RawArch)) {
    try {
        $Architecture = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToUpperInvariant()
    } catch {
        throw "Failed to determine system architecture. Cannot proceed with installation."
    }
} else {
    $Architecture = $RawArch.ToUpperInvariant()
}

# 3. 严格匹配资产列表
switch ($Architecture) {
    "X64"   { $ArchName = "x86_64" }
    "AMD64" { $ArchName = "x86_64" }
    "ARM64" { $ArchName = "arm64" }
    "X86"   { 
        # 32 位系统用户，明确告知不支持
        throw "NeoCode only supports 64-bit systems (x86_64/arm64). Your current system (x86) is not supported." 
    }
    default { 
        throw "Unsupported architecture: $Architecture. Please visit the releases page to check for available binaries." 
    }
}

if (![string]::IsNullOrWhiteSpace($env:NEOCODE_INSTALL_LATEST_TAG)) {
	$LatestTag = $env:NEOCODE_INSTALL_LATEST_TAG
}
else {
	Write-Host "Resolving latest release metadata..."
	$LatestTag = (Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest").tag_name
	if ([string]::IsNullOrWhiteSpace($LatestTag)) {
		throw "Failed to resolve latest release tag from GitHub API."
	}
}

$ZipFile = "${AssetPrefix}_Windows_${ArchName}.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$LatestTag/$ZipFile"
$ChecksumUrl = "https://github.com/$Repo/releases/download/$LatestTag/checksums.txt"

if ($DryRun) {
	Write-Output "flavor=$Flavor"
	Write-Output "asset=$ZipFile"
	Write-Output "download_url=$DownloadUrl"
	Write-Output "checksum_url=$ChecksumUrl"
	exit 0
}

$TempDir = Join-Path $env:TEMP "neocode_install_$([Guid]::NewGuid().ToString('N'))"
New-Item -Path $TempDir -ItemType Directory -Force | Out-Null
try {
	$ZipPath = Join-Path $TempDir $ZipFile
	$ChecksumPath = Join-Path $TempDir "checksums.txt"

	Write-Host "Downloading $ZipFile..."
	Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath
	Write-Host "Downloading checksums..."
	Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath

	$ChecksumLine = Get-Content -Path $ChecksumPath | Where-Object {
		($_ -match "^[0-9a-fA-F]{64}\s+\*?$([Regex]::Escape($ZipFile))$")
	} | Select-Object -First 1
	if ([string]::IsNullOrWhiteSpace($ChecksumLine)) {
		throw "Failed to find checksum entry for $ZipFile."
	}
	$ExpectedHash = (($ChecksumLine -split "\s+")[0]).ToLowerInvariant()
	$ActualHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($ActualHash -ne $ExpectedHash) {
		throw "Checksum verification failed for $ZipFile. Expected=$ExpectedHash Actual=$ActualHash"
	}

	Write-Host "Extracting archive..."
	Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

	$InstallDir = Join-Path $env:LOCALAPPDATA "NeoCode"
	if (!(Test-Path $InstallDir)) {
		New-Item -Path $InstallDir -ItemType Directory -Force | Out-Null
	}
	Copy-Item -Path (Join-Path $TempDir $BinaryName) -Destination $InstallDir -Force

	$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
	if ($UserPath -notmatch [Regex]::Escape($InstallDir)) {
		[Environment]::SetEnvironmentVariable("PATH", "$UserPath;$InstallDir", "User")
		Write-Host "Updated PATH. Re-open PowerShell/CMD to apply changes." -ForegroundColor Yellow
	}

	if ($Flavor -eq "full") {
		$NeoCodeExecutablePath = Join-Path $InstallDir "neocode.exe"
		Write-Host "Installing HTTP daemon autostart..."
		try {
			& $NeoCodeExecutablePath daemon install | Out-Null
		}
		catch {
			Write-Warning "Failed to install HTTP daemon autostart automatically."
			Write-Warning "Run '$NeoCodeExecutablePath daemon install' manually after installation."
		}
	}
	Write-Host "Installed $BinaryName ($Flavor) from $LatestTag." -ForegroundColor Green
}
finally {
	if (Test-Path $TempDir) {
		Remove-Item -Path $TempDir -Recurse -Force
	}
}
