<#
.SYNOPSIS
    Starts two throwaway Syncthing instances that share a folder, for testing.

.DESCRIPTION
    Most of what is worth testing in this fork -- desktop notifications, the
    device verification handshake, selective sync -- only happens when two
    devices talk to each other. Setting that up by hand takes twenty minutes
    and has several ways to go subtly wrong, so it lives here instead.

    Both instances run on their own ports, in their own home directories, well
    away from the real install on 8384. Nothing here touches it.

    Four things this gets right that a hand-rolled setup usually does not:

      * Discovery and relays are off and the peer address is static, so the two
        find each other instantly and nothing leaves the machine.

      * Only ONE side dials. Give both a static address for the other and they
        race, each tearing down the connection the other just made, and the
        pair flaps forever without a single useful error.

      * `encryptionPassword` is written as an empty string. Editing config.xml
        with PowerShell's [xml] type re-serialises `<encryptionPassword />` as
        an element containing whitespace, which Syncthing reads as a real
        password. The symptom is "Device sent cluster-config without the device
        info for the remote" and a connection that drops one second after every
        handshake. This cost an hour once; do not reintroduce it by round-
        tripping config.xml through [xml] after the folder exists.

      * `--logfile` is passed. Without it a `-H windowsgui` Syncthing started
        detached logs nowhere, and `syncthing.log` in the home directory stays
        zero bytes. `/rest/system/log` works too and is usually easier.

.PARAMETER Root
    Where the two homes and their data live. Defaults to a directory under TEMP.

.PARAMETER Binary
    The Syncthing binary to run. Defaults to the installed branded build.

.PARAMETER Fresh
    Delete any existing homes and start over.

.PARAMETER WithFolder
    Create a shared folder on A with sample files and offer it to B, so there
    is something to sync. Without this the pair connects but shares nothing,
    which is what you want when testing the folder-offer notification.

.PARAMETER Stop
    Stop both instances and exit. Leaves the homes in place.

.EXAMPLE
    .\custom\scripts\start-test-pair.ps1 -Fresh -WithFolder

.EXAMPLE
    .\custom\scripts\start-test-pair.ps1 -Stop
#>
[CmdletBinding()]
param(
    [string]$Root = (Join-Path $env:TEMP 'desuq-syncthing-testpair'),
    [string]$Binary = (Join-Path $env:LOCALAPPDATA 'Programs\desuq-syncthing\desuq-syncthing.exe'),
    [switch]$Fresh,
    [switch]$WithFolder,
    [switch]$Stop
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Instance A is the one to point the tray at; it is the "local" side in most
# tests. B plays the other person.
$Nodes = @(
    [pscustomobject]@{ Name = 'A'; Gui = 8390; Listen = 22010; Key = 'desuqtestkeyAAAAAAAAAAAAAAAAAAAA' },
    [pscustomobject]@{ Name = 'B'; Gui = 8391; Listen = 22011; Key = 'desuqtestkeyBBBBBBBBBBBBBBBBBBBB' }
)

function Home-For([string]$n) { Join-Path $Root "n$n" }
function Data-For([string]$n) { Join-Path $Root "data$n" }
function Headers([object]$node) { return @{ 'X-API-Key' = $node.Key } }
function BaseUrl([object]$node) { return "http://127.0.0.1:$($node.Gui)" }

function Api {
    param([object]$Node, [string]$Path, [string]$Method = 'Get', $Body)
    $args = @{
        Uri        = (BaseUrl $Node) + $Path
        Headers    = (Headers $Node)
        Method     = $Method
        TimeoutSec = 10
    }
    if ($null -ne $Body) {
        $args.ContentType = 'application/json'
        $args.Body = ($Body | ConvertTo-Json -Depth 10)
    }
    return Invoke-RestMethod @args
}

function Test-Up([object]$Node) {
    try { $null = Api -Node $Node -Path '/rest/system/ping'; return $true }
    catch { return $false }
}

function Stop-Pair {
    foreach ($n in $Nodes) {
        if (Test-Up $n) {
            try { $null = Api -Node $n -Path '/rest/system/shutdown' -Method Post } catch { }
            Write-Host "Stopped instance $($n.Name) (port $($n.Gui))"
        }
    }
    Start-Sleep -Seconds 3
}

if ($Stop) {
    Stop-Pair
    Write-Host 'Test pair stopped.' -ForegroundColor Green
    return
}

if (-not (Test-Path -LiteralPath $Binary)) {
    throw "Syncthing binary not found: $Binary`nBuild it first, or pass -Binary."
}

Stop-Pair

if ($Fresh -and (Test-Path -LiteralPath $Root)) {
    Write-Host "Removing $Root..."
    Remove-Item -LiteralPath $Root -Recurse -Force
}
$null = New-Item -ItemType Directory -Force -Path $Root

# --- generate ------------------------------------------------------------

$generated = @{}
foreach ($n in $Nodes) {
    $home2 = Home-For $n.Name
    $null = New-Item -ItemType Directory -Force -Path $home2, (Data-For $n.Name)
    if (Test-Path -LiteralPath (Join-Path $home2 'config.xml')) { continue }
    $generated[$n.Name] = $true

    Write-Host "Generating keys for instance $($n.Name)..."
    # -H windowsgui: the call operator neither waits nor sets $LASTEXITCODE.
    $p = Start-Process -FilePath $Binary -ArgumentList @('generate', "--home=$home2") `
        -Wait -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $Root "gen$($n.Name).out") `
        -RedirectStandardError (Join-Path $Root "gen$($n.Name).err")
    if ($p.ExitCode -ne 0) {
        throw "generate failed for $($n.Name) with exit code $($p.ExitCode). See $Root\gen$($n.Name).err"
    }
}

# --- configure -----------------------------------------------------------
#
# Done on the XML before first start, because these are the settings Syncthing
# reads at boot. Everything after this point goes through the REST API instead,
# so the [xml] round trip never sees a folder or device element (see the
# encryptionPassword note in the header).

function Set-Single([xml]$Doc, [string]$XPath, [string]$Value) {
    $nodes = @($Doc.SelectNodes($XPath))
    for ($i = 1; $i -lt $nodes.Count; $i++) { $null = $nodes[$i].ParentNode.RemoveChild($nodes[$i]) }
    if ($nodes.Count -gt 0) { $nodes[0].InnerText = $Value }
}

$ids = @{}
foreach ($n in $Nodes) {
    $cfg = Join-Path (Home-For $n.Name) 'config.xml'
    [xml]$x = Get-Content -LiteralPath $cfg -Raw
    $x.configuration.gui.address = "127.0.0.1:$($n.Gui)"
    $x.configuration.gui.apikey = $n.Key
    $x.configuration.gui.theme = 'violet'
    Set-Single $x '/configuration/options/listenAddress'         "tcp://127.0.0.1:$($n.Listen)"
    Set-Single $x '/configuration/options/globalAnnounceEnabled' 'false'
    Set-Single $x '/configuration/options/localAnnounceEnabled'  'false'
    Set-Single $x '/configuration/options/relaysEnabled'         'false'
    Set-Single $x '/configuration/options/startBrowser'          'false'
    # The default 60 s makes every test a waiting game.
    Set-Single $x '/configuration/options/reconnectionIntervalS' '10'
    # `generate` creates a default folder; drop it so tests start from nothing.
    # Only on the run that generated this config: without the guard, restarting
    # the pair without -Fresh deleted every folder the previous run had set up,
    # including the one -WithFolder makes, and left a pair that connects and
    # shares nothing while claiming to have started normally.
    if ($generated[$n.Name]) {
        foreach ($f in @($x.SelectNodes('/configuration/folder'))) { $null = $f.ParentNode.RemoveChild($f) }
    }
    $x.Save($cfg)

}

# --- start ---------------------------------------------------------------

foreach ($n in $Nodes) {
    Start-Process -FilePath $Binary -WindowStyle Hidden -ArgumentList @(
        'serve', "--home=$(Home-For $n.Name)", '--no-browser',
        "--logfile=$(Join-Path $Root "st$($n.Name).log")"
    )
}

Write-Host 'Waiting for both instances...' -NoNewline
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Seconds 2
    if ((Test-Up $Nodes[0]) -and (Test-Up $Nodes[1])) { break }
    Write-Host '.' -NoNewline
}
Write-Host ''
foreach ($n in $Nodes) {
    if (-not (Test-Up $n)) { throw "Instance $($n.Name) did not come up. See $Root\st$($n.Name).log" }
}

# Ask each instance who it is, rather than reading the first <device> out of
# its config.xml. After the first run that file holds two devices, sorted by
# ID, so "the first one" is whichever of the pair sorts lower -- and on a
# restart without -Fresh both instances were reported as, and paired against,
# the same device.
foreach ($n in $Nodes) {
    $ids[$n.Name] = (Api -Node $n -Path '/rest/system/status').myID
}
if ($ids['A'] -eq $ids['B']) { throw "Both instances report device ID $($ids['A']); the homes are not distinct." }

# --- pair them -----------------------------------------------------------
#
# Only B holds a static address for A, so only B dials. Both sides knowing how
# to reach the other makes them race and flap; see the header.

$A, $B = $Nodes
Api -Node $B -Path '/rest/config/devices' -Method Post -Body @{
    deviceID  = $ids['A']
    name      = 'Studio Workstation'
    addresses = @("tcp://127.0.0.1:$($A.Listen)")
} | Out-Null

Api -Node $A -Path '/rest/config/devices' -Method Post -Body @{
    deviceID  = $ids['B']
    name      = 'Yuki Laptop'
    addresses = @('dynamic')
} | Out-Null

Write-Host 'Waiting for the pair to connect...' -NoNewline
$connected = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 3
    $c = Api -Node $A -Path '/rest/system/connections'
    # @() matters: a single match comes back as a bare object, and under
    # StrictMode reading .Count on that is an error rather than 1.
    $live = @($c.connections.PSObject.Properties | Where-Object { $_.Value.connected })
    if ($live.Count -gt 0) { $connected = $true; break }
    Write-Host '.' -NoNewline
}
Write-Host ''
if (-not $connected) {
    Write-Warning "Not connected yet. Check: Invoke-RestMethod $(BaseUrl $A)/rest/system/log -Headers @{'X-API-Key'='$($A.Key)'}"
}

# --- optional shared folder ----------------------------------------------

if ($WithFolder) {
    $src = Join-Path (Data-For 'A') 'assets'
    $null = New-Item -ItemType Directory -Force -Path $src
    $rand = [Random]::new(20260823)
    foreach ($i in 1..6) {
        $bytes = New-Object byte[] (2MB)
        $rand.NextBytes($bytes)
        [IO.File]::WriteAllBytes((Join-Path $src "texture$i.png"), $bytes)
    }

    # encryptionPassword must be an empty string, not absent and not whitespace.
    Api -Node $A -Path '/rest/config/folders' -Method Post -Body @{
        id      = 'assets-test'
        label   = 'Project Assets'
        path    = $src
        type    = 'sendreceive'
        devices = @(
            @{ deviceID = $ids['A']; introducedBy = ''; encryptionPassword = '' },
            @{ deviceID = $ids['B']; introducedBy = ''; encryptionPassword = '' }
        )
    } | Out-Null
    Write-Host "Instance A shares 'Project Assets' (12 MB) with B; B has not accepted it yet."
}

# --- report --------------------------------------------------------------

Write-Host ''
Write-Host 'Test pair is up.' -ForegroundColor Green
foreach ($n in $Nodes) {
    Write-Host ("  {0}  {1}  key={2}" -f $n.Name, (BaseUrl $n), $n.Key)
    Write-Host ("     id={0}" -f $ids[$n.Name])
    Write-Host ("     home={0}" -f (Home-For $n.Name))
}
Write-Host ''
Write-Host 'Attach the tray to instance A:'
Write-Host ("  Start-Process .\custom\tray\desuq-syncthing-tray.exe -ArgumentList '--home={0}','--attach'" -f (Home-For 'A'))
Write-Host 'Serve the GUI from the working tree instead of the compiled blob:'
Write-Host '  $env:STGUIASSETS = "<repo>\gui"   (set it before starting, then re-run this script)'
Write-Host 'Logs:'
Write-Host ("  {0}\stA.log, {0}\stB.log  -- or /rest/system/log, which is usually easier" -f $Root)
Write-Host ''
Write-Host 'Stop with:  .\custom\scripts\start-test-pair.ps1 -Stop'
