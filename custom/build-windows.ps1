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
    [switch]$Installer,
    [switch]$SkipTests
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
    # Pinned, not @latest. This tool emits a COFF object that is linked into
    # the shipped binary, and @latest opts out of the only protection that
    # matters here: GOSUMDB verifies a *given* version, so never fixing one
    # means every build machine -- including the CI runner that produces the
    # installer people are told to download -- links whatever was newest that
    # minute. Matches the version build-syncthing.yaml already pins.
    & $GoExe install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.0
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

# Inno Setup and the Windows version resource both want a purely numeric
# version. Derive one from the tag, falling back to 0.0.0.0 for untagged builds.
#
# The fourth component is the N in "-desuq.N", and it is not decoration. Two
# releases off the same upstream base -- v2.1.4-desuq.1 and v2.1.4-desuq.2 --
# have identical x.y.z, so a three-part number made them indistinguishable in
# file properties *and* produced two different installers with the same
# published filename. The first release of this fork and the second would both
# have been "desuq-syncthing-setup-2.1.4.exe".
$verBase = '0.0.0'
if ($Version -match '(\d+)\.(\d+)\.(\d+)') {
    $verBase = "$($Matches[1]).$($Matches[2]).$($Matches[3])"
}
$verRev = 0
if ($Version -match '-desuq\.(\d+)') { $verRev = [int]$Matches[1] }
$numeric = "$verBase.$verRev"

# The installer is named after the tag rather than the numeric version, because
# the person downloading it is looking for "the one I was told to get" and the
# tag is what they were told. Anything a file name cannot carry is dropped;
# in practice a tag has none of it.
$setupName = "$($Brand.Binary)-setup-" + ($Version -replace '[^A-Za-z0-9._-]', '')

# --- Checks ---------------------------------------------------------------

# Every suite that needs nothing but Go and Node runs before the build. Twenty
# seconds here is cheaper than shipping a binary whose telemetry guard or tray
# logic regressed. The two suites that need a built binary or two live
# instances are left to run-tests.ps1 and to CI: they cannot run yet, and they
# take minutes rather than seconds.
#
# -SkipTests is for a tight edit loop, and it is what CI passes -- the test
# workflow builds here and then runs every suite itself, so the quick set would
# otherwise run twice.
$runTests  = Join-Path $PSScriptRoot 'scripts/run-tests.ps1'
$wordCheck = Join-Path $PSScriptRoot 'scripts/check-handshake-words.ps1'

if (-not $SkipTests -and (Test-Path -LiteralPath $runTests)) {
    & $runTests -Quick
    if ($LASTEXITCODE -ne 0) {
        throw 'tests failed; re-run custom\scripts\run-tests.ps1 for the detail, or pass -SkipTests'
    }
} elseif (Test-Path -LiteralPath $wordCheck) {
    # Even with the suites skipped, this one still runs. The wordlists have to
    # stay phonetically distinct or the device handshake silently stops being
    # able to catch a mismatch, and re-ordering a list invalidates every
    # verification anyone has already done -- so it is a property of the thing
    # being compiled in, not just a test, and a build is the last point at
    # which a bad edit can be caught.
    Write-Host 'Checking device-verification wordlists...' -ForegroundColor DarkGray
    & $wordCheck
    if ($LASTEXITCODE -ne 0) { throw 'handshake wordlist check failed' }
}

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

    # Version resource, so the tray shows the same publisher and product in file
    # properties and Task Manager as the daemon does. build.go does this for the
    # daemon via goversioninfo; the tray is a separate module, so it gets the
    # same treatment here from the same branding block.
    $verMajor, $verMinor, $verPatch, $verBuild = $numeric -split '\.'
    $fixed = [ordered]@{
        Major = [int]$verMajor; Minor = [int]$verMinor
        Patch = [int]$verPatch; Build = [int]$verBuild
    }
    $versionInfo = [ordered]@{
        FixedFileInfo  = [ordered]@{
            FileVersion    = $fixed
            ProductVersion = $fixed
        }
        StringFileInfo = [ordered]@{
            CompanyName      = $Brand.Company
            FileDescription  = "$($Brand.Product) - notification area icon"
            FileVersion      = $Version
            InternalName     = "$($Brand.Binary)-tray"
            LegalCopyright   = $Brand.Company
            OriginalFilename = "$($Brand.Binary)-tray.exe"
            ProductName      = $Brand.Product
            ProductVersion   = $Version
        }
        IconPath       = if ($env:ST_BRAND_ICON) { $env:ST_BRAND_ICON } else { (Join-Path $RepoRoot 'assets\logo.ico') -replace '\\', '/' }
    }

    $viPath   = Join-Path $TrayDir 'versioninfo.json'
    $sysoPath = Join-Path $TrayDir 'resource.syso'
    $versionInfo | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $viPath -Encoding utf8

    try {
        & goversioninfo.exe -o $sysoPath -64=true $viPath
        if ($LASTEXITCODE -ne 0) { throw "goversioninfo failed with exit code $LASTEXITCODE" }

        Write-Host "Building $($Brand.Binary)-tray.exe..." -ForegroundColor Cyan
        # -H windowsgui for the same reason as the daemon: a shortcut must not
        # pop a console window at sign-in.
        & $go build -ldflags '-H windowsgui' -o $trayOut .
        if ($LASTEXITCODE -ne 0) { throw "tray build failed with exit code $LASTEXITCODE" }
    }
    finally {
        # Leaving these behind would bake a stale version into every later
        # `go build` run by hand in this directory.
        Remove-Item -LiteralPath $viPath, $sysoPath -Force -EA SilentlyContinue
    }
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
        "/DMyAppSetupName=$setupName" `
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
