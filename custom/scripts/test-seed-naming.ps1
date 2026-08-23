<#
.SYNOPSIS
    Asserts that re-seeding never takes a device name somebody chose.

.DESCRIPTION
    seed-config.ps1 used to set device/@name unconditionally. On a fresh
    install that is right -- it replaces Syncthing's DESKTOP-A1B2C3, which
    tells nobody which machine they are looking at. Over an existing install it
    was wrong, and it silently overwrote a name that had been set deliberately.

    The sentinel file normally stops a second seed from happening at all, so
    this only ever bit on -Force or after a $SeedVersion bump. That makes it
    exactly the kind of regression nobody notices until it has already happened
    to somebody, which is why it is asserted here rather than reasoned about.

    Runs the real script against the real binary in a throwaway directory.
    Nothing here touches the installed configuration.

.PARAMETER Binary
    The Syncthing binary to use. Defaults to the one custom/build-windows.ps1
    produces.
#>
[CmdletBinding()]
param(
    [string]$Binary = (Join-Path $PSScriptRoot '..\dist\desuq-syncthing.exe')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Seed = Join-Path $PSScriptRoot 'seed-config.ps1'
$Root = Join-Path $env:TEMP ('desuq-seed-naming-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))

if (-not (Test-Path -LiteralPath $Binary)) {
    throw "Syncthing binary not found: $Binary`nRun .\custom\build-windows.ps1 first, or pass -Binary."
}
$Binary = (Resolve-Path -LiteralPath $Binary).Path

$script:Failures = 0
function Check {
    param([string]$Name, [bool]$Ok, [string]$Detail)
    if ($Ok) { Write-Host "  ok   $Name" -ForegroundColor DarkGreen }
    else {
        Write-Host "  FAIL $Name   $Detail" -ForegroundColor Red
        $script:Failures++
    }
}

function Get-DeviceName([string]$dir) {
    [xml]$x = Get-Content -LiteralPath (Join-Path $dir 'config.xml') -Raw
    $id = (Get-DeviceId $dir)
    $me = @($x.configuration.device) | Where-Object { $_.id -eq $id } | Select-Object -First 1
    if (-not $me) { $me = @($x.configuration.device)[0] }
    return $me.name
}

function Get-DeviceId([string]$dir) {
    $out = Join-Path $Root 'id.txt'
    $p = Start-Process -FilePath $Binary -ArgumentList @('device-id', "--home=$dir") `
        -Wait -PassThru -WindowStyle Hidden -RedirectStandardOutput $out
    if ($p.ExitCode -ne 0) { throw "device-id failed" }
    return (Get-Content -LiteralPath $out -Raw).Trim()
}

function Set-DeviceName([string]$dir, [string]$name) {
    $cfg = Join-Path $dir 'config.xml'
    [xml]$x = Get-Content -LiteralPath $cfg -Raw
    $id = Get-DeviceId $dir
    foreach ($d in @($x.configuration.device)) {
        if ($d.id -eq $id) { $d.SetAttribute('name', $name) }
    }
    $x.Save($cfg)
}

function Invoke-Seed {
    # Hashtable splatting, not an array: an array splat passes its elements
    # positionally, so "-Force" arrives as a value rather than as a switch.
    param([string]$dir, [hashtable]$Extra = @{})
    $splat = @{ DataDir = $dir; Binary = $Binary }
    foreach ($k in $Extra.Keys) { $splat[$k] = $Extra[$k] }
    & $Seed @splat | Out-Null
}

$expected = $env:USERNAME
if (-not $expected) { $expected = [System.Net.Dns]::GetHostName() }

try {
    $null = New-Item -ItemType Directory -Force -Path $Root

    Write-Host "Seeded device naming, against $Binary"
    Write-Host ''

    # --- a fresh install ---------------------------------------------------
    Write-Host '-- a fresh install is named after the user, not the machine'
    $fresh = Join-Path $Root 'fresh'
    Invoke-Seed $fresh
    $got = Get-DeviceName $fresh
    Check 'the device is renamed on first seed' ($got -eq $expected) "got '$got', want '$expected'"

    # --- a name somebody chose --------------------------------------------
    Write-Host ''
    Write-Host '-- a name somebody chose survives a forced re-seed'
    Set-DeviceName $fresh "Yuki's Workstation"
    Invoke-Seed $fresh @{ Force = $true }
    $got = Get-DeviceName $fresh
    Check 'the chosen name is left alone' ($got -eq "Yuki's Workstation") "got '$got'"

    # And the rest of the seeding still happened, so this is not just an
    # early exit wearing a disguise.
    [xml]$x = Get-Content -LiteralPath (Join-Path $fresh 'config.xml') -Raw
    Check 'the other defaults were still applied' ($x.configuration.gui.theme -eq 'violet') `
        "theme is '$($x.configuration.gui.theme)'"

    # --- names Syncthing picked -------------------------------------------
    Write-Host ''
    Write-Host '-- names Syncthing picked are still replaced'

    Set-DeviceName $fresh ([System.Net.Dns]::GetHostName())
    Invoke-Seed $fresh @{ Force = $true }
    $got = Get-DeviceName $fresh
    Check 'the host name is recognised as the default and replaced' ($got -eq $expected) "got '$got'"

    Set-DeviceName $fresh 'DESKTOP-Z9Y8X7W'
    Invoke-Seed $fresh @{ Force = $true }
    $got = Get-DeviceName $fresh
    Check 'an out-of-box Windows name is replaced even after a machine rename' `
        ($got -eq $expected) "got '$got'"

    Set-DeviceName $fresh 'LAPTOP-4KJ21QA'
    Invoke-Seed $fresh @{ Force = $true }
    Check 'so is the LAPTOP- form' ((Get-DeviceName $fresh) -eq $expected) "got '$(Get-DeviceName $fresh)'"

    # The pattern is anchored, so something that merely starts the same way is
    # a real name and stays.
    Set-DeviceName $fresh 'Desktop upstairs'
    Invoke-Seed $fresh @{ Force = $true }
    Check 'but "Desktop upstairs" is a name, not a pattern' `
        ((Get-DeviceName $fresh) -eq 'Desktop upstairs') "got '$(Get-DeviceName $fresh)'"

    # --- an explicit instruction -------------------------------------------
    Write-Host ''
    Write-Host '-- an explicit -DeviceName is an instruction and wins'
    Set-DeviceName $fresh 'Render Box'
    Invoke-Seed $fresh @{ Force = $true; DeviceName = 'Render Box 2' }
    Check 'it overrides even a chosen name' ((Get-DeviceName $fresh) -eq 'Render Box 2') `
        "got '$(Get-DeviceName $fresh)'"

    # --- the sentinel still does its job -----------------------------------
    Write-Host ''
    Write-Host '-- and without -Force nothing is touched at all'
    Set-DeviceName $fresh 'Untouched'
    Invoke-Seed $fresh
    Check 'a repeat seed at the same version is a no-op' `
        ((Get-DeviceName $fresh) -eq 'Untouched') "got '$(Get-DeviceName $fresh)'"

    Write-Host ''
    if ($script:Failures -gt 0) {
        Write-Host "$($script:Failures) check(s) failed." -ForegroundColor Red
        exit 1
    }
    Write-Host 'All checks passed.' -ForegroundColor Green
} finally {
    Remove-Item -LiteralPath $Root -Recurse -Force -ErrorAction SilentlyContinue
}
