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

$Architecture = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToUpperInvariant()
switch ($Architecture) {
	"X64" { $ArchName = "x86_64" }
	"AMD64" { $ArchName = "x86_64" }
	"ARM64" { $ArchName = "arm64" }
	default { throw "Unsupported architecture: $Architecture" }
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

	Write-Host "Installed $BinaryName ($Flavor) from $LatestTag." -ForegroundColor Green
}
finally {
	if (Test-Path $TempDir) {
		Remove-Item -Path $TempDir -Recurse -Force
	}
}
