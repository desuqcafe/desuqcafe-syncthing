<#
.SYNOPSIS
    Builds the desuqcafe Syncthing fork for Windows and (optionally) packages
    the installer.

.DESCRIPTION
    Wraps upstream's build.go rather than replacing it, so upstream build
    changes are picked up automatically. Branding is injected through the
    ST_BRAND_* environment variables (see custom/CUSTOMIZATIONS.md).

    The binary is always built with the "noupgrade" tag. Syncthing verifies
    upgrades against upstream's release signing key, which cannot validate this
    fork's builds, so in-app auto-upgrade is compiled out entirely. Updates are
    delivered by re-running the installer.

.PARAMETER Version
    Version string to stamp into the binary, e.g. "v2.1.4-desuq.1". Defaults to
    `git describe`.

.PARAMETER Installer
    Also build the Windows installer with Inno Setup.

.EXAMPLE
    .\custom\build-windows.ps1 -Installer
#>
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$Installer
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. "$PSScriptRoot\branding.ps1"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$DistDir  = Join-Path $PSScriptRoot 'dist'

function Find-Go {
    $cmd = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($candidate in @("$env:ProgramFiles\Go\bin\go.exe",
                             "${env:ProgramFiles(x86)}\Go\bin\go.exe",
                             "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
        if (Test-Path $candidate) { return $candidate }
    }
    throw "Go toolchain not found. Install it with:  winget install GoLang.Go"
}

function Find-ISCC {
    $cmd = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($candidate in @("${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
                             "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
                             "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")) {
        if (Test-Path $candidate) { return $candidate }
    }
    throw "Inno Setup not found. Install it with:  winget install JRSoftware.InnoSetup"
}

function Initialize-GoVersionInfo {
    param([string]$GoExe)
    # build.go shells out to `goversioninfo` to embed the Windows version
    # resource (product name, publisher, icon). Without it the branding is
    # silently skipped, so provision it on demand.
    $gopath = (& $GoExe env GOPATH).Trim()
    $gobin  = Join-Path $gopath 'bin'
    if ($env:PATH -notlike "*$gobin*") { $env:PATH = "$gobin;$env:PATH" }
    if (Get-Command goversioninfo.exe -ErrorAction SilentlyContinue) { return }
    Write-Host "Installing goversioninfo..." -ForegroundColor DarkGray
    & $GoExe install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
    if ($LASTEXITCODE -ne 0) { throw "failed to install goversioninfo" }
}

$go = Find-Go
Write-Host "Go:   $go" -ForegroundColor DarkGray
Initialize-GoVersionInfo -GoExe $go

# Resolve the version before building so the installer and binary agree.
if (-not $Version) {
    $Version = (& git -C $RepoRoot describe --tags --always --dirty --abbrev=8).Trim()
}
Write-Host "Version: $Version" -ForegroundColor DarkGray

# Inno Setup requires a numeric x.y.z version; derive one from the tag and fall
# back to 0.0.0 for untagged builds.
$numeric = '0.0.0'
if ($Version -match '(\d+)\.(\d+)\.(\d+)') { $numeric = "$($Matches[1]).$($Matches[2]).$($Matches[3])" }

# --- Build ----------------------------------------------------------------
Push-Location $RepoRoot
try {
    $env:CGO_ENABLED        = '0'
    $env:GOOS               = 'windows'
    $env:GOARCH             = 'amd64'
    $env:VERSION            = $Version

    # Build as a GUI-subsystem binary so launching from a shortcut does not pop
    # a console window. Output is still written to an attached terminal, so
    # running it from PowerShell for troubleshooting keeps working.
    $env:EXTRA_LDFLAGS      = '-H windowsgui'

    # Branding overrides consumed by build.go's shouldBuildSyso().
    $env:ST_BRAND_COMPANY     = $Brand.Company
    $env:ST_BRAND_PRODUCT     = $Brand.Product
    $env:ST_BRAND_DESCRIPTION = $Brand.Description
    $env:ST_BRAND_BINARY      = $Brand.Binary

    # Use a custom icon if one has been dropped in, else keep upstream's.
    $customIcon = Join-Path $PSScriptRoot 'branding\logo.ico'
    if (Test-Path $customIcon) {
        $env:ST_BRAND_ICON = (Resolve-Path $customIcon).Path -replace '\', '/'
        Write-Host "Icon: custom" -ForegroundColor DarkGray
    }

    Write-Host "Building $($Brand.Binary).exe (noupgrade)..." -ForegroundColor Cyan
    & $go run build.go -no-upgrade build
    if ($LASTEXITCODE -ne 0) { throw "build.go failed with exit code $LASTEXITCODE" }

    if (-not (Test-Path $DistDir)) { New-Item -ItemType Directory -Path $DistDir | Out-Null }
    $out = Join-Path $DistDir "$($Brand.Binary).exe"
    Move-Item -Force -Path (Join-Path $RepoRoot 'syncthing.exe') -Destination $out
    Write-Host "Binary: $out" -ForegroundColor Green
}
finally {
    Pop-Location
}

# --- Tray ------------------------------------------------------------------
# Built separately because custom/tray is its own Go module: it needs a systray
# dependency that has no business in upstream's go.mod. See custom/tray/go.mod.
$TrayDir = Join-Path $PSScriptRoot 'tray'
Push-Location $TrayDir
try {
    $env:CGO_ENABLED   = '0'
    $env:GOOS          = 'windows'
    $env:GOARCH        = 'amd64'
    $trayOut = Join-Path $DistDir "$($Brand.Binary)-tray.exe"

    Write-Host "Building $($Brand.Binary)-tray.exe..." -ForegroundColor Cyan
    # -H windowsgui for the same reason as the daemon: a shortcut must not pop
    # a console window at sign-in.
    & $go build -ldflags '-H windowsgui' -o $trayOut .
    if ($LASTEXITCODE -ne 0) { throw "tray build failed with exit code $LASTEXITCODE" }
    Write-Host "Tray:   $trayOut" -ForegroundColor Green
}
finally {
    Pop-Location
}

# --- Installer ------------------------------------------------------------
if ($Installer) {
    $iscc = Find-ISCC
    Write-Host "Packaging installer..." -ForegroundColor Cyan
    & $iscc `
        "/DMyAppVersion=$numeric" `
        "/DMyAppVersionFull=$Version" `
        "/DMyAppName=$($Brand.Product)" `
        "/DMyAppBinary=$($Brand.Binary)" `
        "/DMyAppPublisher=$($Brand.Company)" `
        "/DMyAppUrl=$($Brand.Url)" `
        "/DMyAppId=$($Brand.AppId)" `
        "/DMyDataDir=$($Brand.DataDirName)" `
        (Join-Path $PSScriptRoot 'installer\installer.iss')
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }
    Get-ChildItem (Join-Path $DistDir '*.exe') | ForEach-Object {
        Write-Host ("  {0}  ({1:N1} MB)" -f $_.Name, ($_.Length / 1MB)) -ForegroundColor Green
    }
}
