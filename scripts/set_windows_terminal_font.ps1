param(
	[string]$FontFace = "JetBrains Mono"
)

$ErrorActionPreference = "Stop"

function Set-OrAddProperty {
	param(
		[Parameter(Mandatory = $true)] $Target,
		[Parameter(Mandatory = $true)][string]$Name,
		[Parameter(Mandatory = $true)] $Value
	)

	if ($Target.PSObject.Properties.Name -contains $Name) {
		$Target.$Name = $Value
		return
	}
	$Target | Add-Member -NotePropertyName $Name -NotePropertyValue $Value
}

$settingsCandidates = @(
	(Join-Path $env:LOCALAPPDATA "Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json"),
	(Join-Path $env:LOCALAPPDATA "Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState\settings.json")
)

$settingsPath = $settingsCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if ([string]::IsNullOrWhiteSpace($settingsPath)) {
	throw "Windows Terminal settings.json was not found."
}

$raw = Get-Content -Path $settingsPath -Raw
$settings = $raw | ConvertFrom-Json

if (-not $settings.profiles) {
	Set-OrAddProperty -Target $settings -Name "profiles" -Value ([pscustomobject]@{})
}
if (-not $settings.profiles.defaults) {
	Set-OrAddProperty -Target $settings.profiles -Name "defaults" -Value ([pscustomobject]@{})
}

Set-OrAddProperty -Target $settings.profiles.defaults -Name "font" -Value ([pscustomobject]@{
	face = $FontFace
})
# Backward compatibility for older schema keys.
Set-OrAddProperty -Target $settings.profiles.defaults -Name "fontFace" -Value $FontFace

$json = $settings | ConvertTo-Json -Depth 100
Set-Content -Path $settingsPath -Value $json -Encoding UTF8

Write-Host "Updated Windows Terminal font to '$FontFace' in: $settingsPath"
