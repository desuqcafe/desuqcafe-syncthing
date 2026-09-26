<#
.SYNOPSIS
    Runs the Blender add-on inside a real, headless Blender against a fresh
    test pair. By hand: CI has no Blender, so this is not one of the suites.

.DESCRIPTION
    Starts the pair (-Fresh -WithFolder), accepts the share on B, then runs
    test-blender-live.py in Blender with DESUQ_HOME pointed at instance A.
    --factory-startup, so your own Blender preferences and add-ons are neither
    read nor changed. After Blender has quit it checks that the add-on took
    its mark off on the way out, which only a real exit can show.

.PARAMETER Blender
    blender.exe to use. Defaults to the newest under Program Files.

.PARAMETER Binary
    The Syncthing binary for the pair. Defaults to custom\dist.
#>
[CmdletBinding()]
param(
    [string]$Blender,
    [string]$Binary
)
$ErrorActionPreference = 'Stop'
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Repo = Resolve-Path (Join-Path $ScriptDir '..\..')
if (-not $Binary) { $Binary = Join-Path $Repo 'custom\dist\desuq-syncthing.exe' }

if (-not $Blender) {
    $Blender = Get-ChildItem "$env:ProgramFiles\Blender Foundation" -Directory -ErrorAction SilentlyContinue |
        Sort-Object { [version]($_.Name -replace '^Blender\s+', '') } -Descending |
        ForEach-Object { Join-Path $_.FullName 'blender.exe' } |
        Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $Blender -or -not (Test-Path $Blender)) { throw 'Blender not found; pass -Blender.' }
Write-Host "Blender: $Blender"

& (Join-Path $ScriptDir 'start-test-pair.ps1') -Fresh -WithFolder -Binary $Binary | Out-Null
$A = @{ url = 'http://127.0.0.1:8390'; h = @{ 'X-API-Key' = 'desuqtestkeyAAAAAAAAAAAAAAAAAAAA' } }
$B = @{ url = 'http://127.0.0.1:8391'; h = @{ 'X-API-Key' = 'desuqtestkeyBBBBBBBBBBBBBBBBBBBB' } }
$fa = (Invoke-RestMethod "$($A.url)/rest/config/folders" -Headers $A.h)[0]
$idA = (Invoke-RestMethod "$($A.url)/rest/system/status" -Headers $A.h).myID
$idB = (Invoke-RestMethod "$($B.url)/rest/system/status" -Headers $B.h).myID
$pathB = Join-Path (Split-Path $fa.path) 'B-assets'
$fb = @{ id = $fa.id; label = $fa.label; path = $pathB; type = 'sendreceive'
    devices = @(@{ deviceID = $idA }, @{ deviceID = $idB }) }
Invoke-RestMethod -Method Put "$($B.url)/rest/config/folders/$($fa.id)" -Headers $B.h `
    -ContentType 'application/json' -Body ($fb | ConvertTo-Json -Depth 5) | Out-Null
$deadline = (Get-Date).AddSeconds(120)
do {
    Start-Sleep 1
    $s = Invoke-RestMethod "$($B.url)/rest/db/status?folder=$($fa.id)" -Headers $B.h
} until (($s.globalFiles -gt 0 -and $s.needTotalItems -eq 0) -or (Get-Date) -gt $deadline)

$nA = Join-Path $env:TEMP 'desuq-syncthing-testpair\nA'
$env:DESUQ_HOME = $nA
$log = Join-Path $env:TEMP 'desuq-blender-live.log'
$p = Start-Process -FilePath $Blender -Wait -PassThru -NoNewWindow `
    -ArgumentList '--background', '--factory-startup', '--python', (Join-Path $ScriptDir 'test-blender-live.py') `
    -RedirectStandardOutput $log
Remove-Item Env:DESUQ_HOME
Get-Content $log | Where-Object { $_ -match '^(--|  ok|  FAIL|\[desuqcafe\]|FAILURES|Traceback|  File|\w+Error)' }

$last = Get-Content $log | Select-String '^FAILURES=(\d+)' | Select-Object -Last 1
if ($last) { $failed = [int]$last.Matches[0].Groups[1].Value }
else { $failed = 1; Write-Host '  FAIL the script did not finish (see above)' }

Start-Sleep 2
$claims = Invoke-RestMethod "$($A.url)/rest/folder/claims?folder=$($fa.id)" -Headers $A.h
$left = @($claims.claims | Where-Object { $_.mine -and $_.path -eq 'cabin.blend' })
if ($left.Count -eq 0) { Write-Host '  ok   quitting Blender took the automatic mark off' }
else { $failed++; Write-Host '  FAIL quitting Blender left the mark on' }

& (Join-Path $ScriptDir 'start-test-pair.ps1') -Stop | Out-Null
Write-Host ''
if ($failed) { Write-Host "$failed check(s) failed"; exit 1 }
Write-Host 'all live checks passed'
