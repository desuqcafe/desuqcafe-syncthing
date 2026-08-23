<#
.SYNOPSIS
    Writes a first-run config.xml with defaults suited to the 3D team, before
    Syncthing is ever started.

.DESCRIPTION
    Out of the box Syncthing keeps no file history, reserves 1 % of the disk,
    and ignores nothing. For two non-technical modellers pushing .blend files
    around, those three defaults are the difference between "it just works" and
    a support call. See custom/DEPLOYMENT-3D-TEAM.md for the reasoning behind
    each value.

    This touches no upstream source. It shells out to Syncthing's own
    `generate` subcommand to create the config and device keys, then patches
    the resulting config.xml:

      defaults/folder/versioning  -> staggered, 30 day maximum age
      defaults/folder/minDiskFree -> 20 GB absolute (upstream: 1 %)
      defaults/ignores            -> the Blender + OS junk set
      gui/theme                   -> violet
      device/@name                -> the Windows user name (upstream: hostname)

    Only the defaults are touched. Existing folders and devices are left alone,
    so this is safe to run against a live installation.

    Seeding is recorded in a sentinel file in the data directory and skipped on
    later runs, so reinstalling or upgrading never overwrites choices the user
    has since made in the GUI. Pass -Force to re-apply anyway.

    Failures are logged and swallowed: cosmetic defaults are never a good
    reason to fail an installation. Run it by hand to see what went wrong.

.PARAMETER DataDir
    Syncthing's home directory, i.e. what gets passed to --home.

.PARAMETER Binary
    Path to desuq-syncthing.exe.

.PARAMETER DeviceName
    Name this device advertises to the others. Defaults to the Windows user
    name, falling back to the host name.

.PARAMETER Force
    Re-apply the defaults even if this data directory has been seeded before.
    Overwrites GUI changes to the seeded settings.

.EXAMPLE
    .\seed-config.ps1 -DataDir "$env:LOCALAPPDATA\desuqcafe-syncthing" -Binary "$env:LOCALAPPDATA\Programs\desuq-syncthing\desuq-syncthing.exe"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$DataDir,
    [Parameter(Mandatory)][string]$Binary,
    [string]$DeviceName,
    [switch]$Force
)

Set-StrictMode -Version Latest

# Bump when the seeded values below change, so an installer carrying newer
# defaults re-seeds a machine that was set up by an older one.
$SeedVersion = 1

# --- The defaults themselves ----------------------------------------------

# Staggered keeps a decreasing density of old versions, so a month of history
# on binary assets costs a fraction of a month of full copies. maxAge is in
# seconds; this is exactly what the GUI writes for "30 days".
$VersioningType   = 'staggered'
$VersioningMaxAge = 30 * 86400

# Absolute rather than upstream's 1 %, which is only 5 GB on a 500 GB drive --
# nothing at all when one asset drop can be tens of GB.
$MinDiskFreeValue = 20
$MinDiskFreeUnit  = 'GB'

$Theme = 'violet'

# Verified against a real folder: with these applied, a directory holding
# scene.blend, scene.blend1, scene.blend2, scene.blend@, texture.png,
# Thumbs.db and render.tmp indexes exactly scene.blend and texture.png.
# (?d) means "may be deleted if it is all that is left in a directory".
$IgnoreLines = @(
    '// Blender numbered backups and save-temp files'
    '(?d)*.blend[0-9]'
    '(?d)*.blend@'
    '// OS and editor junk'
    '(?d)Thumbs.db'
    '(?d)desktop.ini'
    '(?d).DS_Store'
    '(?d)*.tmp'
    '(?d)~*'
)

# --- Plumbing --------------------------------------------------------------

$script:LogPath = $null

function Write-Log {
    param([string]$Message, [string]$Colour = 'Gray')
    $line = '{0}  {1}' -f (Get-Date -Format 's'), $Message
    Write-Host $line -ForegroundColor $Colour
    if ($script:LogPath) {
        try { Add-Content -Path $script:LogPath -Value $line -Encoding utf8 } catch { }
    }
}

# encoding/xml is order-insensitive for struct fields, so appending a missing
# element is always safe.
function Get-OrAdd {
    param([System.Xml.XmlNode]$Parent, [string]$Name)
    $node = $Parent.SelectSingleNode($Name)
    if (-not $node) {
        $node = $Parent.OwnerDocument.CreateElement($Name)
        [void]$Parent.AppendChild($node)
    }
    return $node
}

function Set-Element {
    param([System.Xml.XmlNode]$Parent, [string]$Name, [string]$Value)
    (Get-OrAdd -Parent $Parent -Name $Name).InnerText = $Value
}

# XmlTextWriter in Indented mode rewrites a childless <x></x> as "<x>\n    </x>",
# which turns an empty element into one holding whitespace. Syncthing then
# reads e.g. versioning/fsPath as "   " rather than "", and puts .stversions
# somewhere surprising. Collapsing childless elements to <x /> avoids it, and
# Go's encoding/xml unmarshals <x /> and <x></x> identically.
function Set-EmptyElementsSelfClosing {
    param([System.Xml.XmlNode]$Node)
    foreach ($child in @($Node.ChildNodes)) {
        if ($child.NodeType -ne [System.Xml.XmlNodeType]::Element) { continue }
        if ($child.ChildNodes.Count -eq 0) {
            $child.IsEmpty = $true
        } else {
            Set-EmptyElementsSelfClosing -Node $child
        }
    }
}

# The branded binary is linked as a GUI-subsystem executable (-H windowsgui) so
# that double-clicking a shortcut does not flash a console window. A side
# effect is that PowerShell's call operator does not wait for it and never sets
# $LASTEXITCODE -- it returns the moment the process starts. Start-Process
# -Wait does the right thing, at the cost of having to redirect the output to
# files to see it.
function Invoke-Syncthing {
    param([string[]]$Arguments)

    $stdout = [System.IO.Path]::GetTempFileName()
    $stderr = [System.IO.Path]::GetTempFileName()
    try {
        $proc = Start-Process -FilePath $Binary -ArgumentList $Arguments `
            -NoNewWindow -Wait -PassThru `
            -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        $output = @()
        foreach ($f in @($stdout, $stderr)) {
            $text = Get-Content -LiteralPath $f -EA SilentlyContinue
            if ($text) { $output += $text }
        }
        return [pscustomobject]@{ ExitCode = $proc.ExitCode; Output = $output }
    } finally {
        Remove-Item -LiteralPath $stdout, $stderr -Force -EA SilentlyContinue
    }
}

# Is this a name Syncthing picked, or one a person chose?
#
# `generate` writes os.Hostname() (lib/config/config.go, prepareDeviceList),
# which on a stock Windows install is DESKTOP-A1B2C3 and tells nobody which
# machine they are looking at. That is the name worth replacing. Anything else
# is somebody's decision, and re-seeding -- with -Force, or after a
# $SeedVersion bump -- must not quietly take it away from them.
#
# The host-name test is the one that matters; the pattern is the backstop for a
# machine that has been renamed since Syncthing first ran, where the config
# still carries the old out-of-box name.
function Test-AutoGeneratedName {
    param([string]$Name)

    if ([string]::IsNullOrWhiteSpace($Name)) { return $true }
    $n = $Name.Trim()

    foreach ($candidate in @($env:COMPUTERNAME, (Get-HostNameOrEmpty))) {
        if ($candidate -and $n -ieq $candidate) { return $true }
    }

    # Windows' own out-of-box names: DESKTOP-A1B2C3, LAPTOP-..., and WIN-... on
    # Server. Deliberately anchored, so a deliberate "Desktop upstairs" is not
    # swept up with them.
    return [bool]($n -imatch '^(DESKTOP|LAPTOP|WIN)-[A-Z0-9]{5,15}$')
}

function Get-HostNameOrEmpty {
    try { return [System.Net.Dns]::GetHostName() } catch { return '' }
}

try {
    if (-not (Test-Path -LiteralPath $Binary)) {
        throw "binary not found: $Binary"
    }

    if (-not (Test-Path -LiteralPath $DataDir)) {
        New-Item -ItemType Directory -Path $DataDir -Force | Out-Null
    }
    $DataDir = (Resolve-Path -LiteralPath $DataDir).Path
    $script:LogPath = Join-Path $DataDir 'desuq-seed.log'

    $sentinel = Join-Path $DataDir '.desuq-seeded'
    if ((Test-Path -LiteralPath $sentinel) -and -not $Force) {
        $seen = ''
        try { $seen = (Get-Content -LiteralPath $sentinel -Raw).Trim() } catch { }
        if ($seen -eq "$SeedVersion") {
            Write-Log "Already seeded (version $SeedVersion); nothing to do."
            exit 0
        }
        Write-Log "Seeded at version '$seen', re-seeding at $SeedVersion."
    }

    # An explicitly passed -DeviceName is an instruction, not a default, so it
    # is applied whatever is already there.
    $deviceNameGiven = $PSBoundParameters.ContainsKey('DeviceName')
    if (-not $DeviceName) {
        $DeviceName = $env:USERNAME
        if (-not $DeviceName) { $DeviceName = Get-HostNameOrEmpty }
    }

    $cfgPath = Join-Path $DataDir 'config.xml'

    # `generate` creates config.xml plus cert.pem/key.pem, and refuses to
    # overwrite an existing key, so this is safe on an upgrade too.
    Write-Log "Generating config and device keys in $DataDir" 'Cyan'
    $gen = Invoke-Syncthing -Arguments @('generate', "--home=$DataDir")
    if ($gen.ExitCode -ne 0) {
        throw "generate failed ($($gen.ExitCode)): $($gen.Output -join '; ')"
    }
    foreach ($line in $gen.Output) { Write-Log "  generate: $line" 'DarkGray' }

    if (-not (Test-Path -LiteralPath $cfgPath)) {
        throw "generate did not produce $cfgPath"
    }

    # Identify this machine's own device entry, so we rename that one and not
    # a peer that has since been added.
    $myID = ''
    try {
        $id = Invoke-Syncthing -Arguments @('device-id', "--home=$DataDir")
        if ($id.ExitCode -eq 0 -and $id.Output.Count -gt 0) {
            $myID = ($id.Output | Select-Object -Last 1).ToString().Trim()
        }
    } catch { }

    $xml = New-Object System.Xml.XmlDocument
    $xml.PreserveWhitespace = $false
    $xml.Load($cfgPath)
    $root = $xml.DocumentElement

    # -- device name --------------------------------------------------------
    $devices = @($root.SelectNodes('device'))
    $me = $null
    if ($myID) {
        $me = $devices | Where-Object { $_.GetAttribute('id') -eq $myID } | Select-Object -First 1
    }
    if (-not $me -and $devices.Count -eq 1) { $me = $devices[0] }
    if ($me) {
        $existingName = $me.GetAttribute('name')
        if ($deviceNameGiven) {
            $me.SetAttribute('name', $DeviceName)
            Write-Log "Device name: $DeviceName (given on the command line)"
        } elseif (Test-AutoGeneratedName $existingName) {
            $me.SetAttribute('name', $DeviceName)
            if ($existingName) {
                Write-Log "Device name: $DeviceName (was '$existingName', which Syncthing picked)"
            } else {
                Write-Log "Device name: $DeviceName"
            }
        } else {
            # A re-seed over an install someone has been using. The name is
            # theirs; the rest of the defaults are still applied.
            Write-Log "Device name: keeping '$existingName'; it was not auto-generated."
        }
    } else {
        Write-Log 'Could not identify the local device; leaving its name alone.' 'Yellow'
    }

    # -- theme --------------------------------------------------------------
    Set-Element -Parent (Get-OrAdd -Parent $root -Name 'gui') -Name 'theme' -Value $Theme
    Write-Log "Theme: $Theme"

    # -- folder defaults ----------------------------------------------------
    $defaults = Get-OrAdd -Parent $root -Name 'defaults'
    $folder   = Get-OrAdd -Parent $defaults -Name 'folder'

    $minDiskFree = Get-OrAdd -Parent $folder -Name 'minDiskFree'
    $minDiskFree.SetAttribute('unit', $MinDiskFreeUnit)
    $minDiskFree.InnerText = "$MinDiskFreeValue"
    Write-Log "Minimum free disk space: $MinDiskFreeValue $MinDiskFreeUnit"

    $versioning = Get-OrAdd -Parent $folder -Name 'versioning'
    $versioning.SetAttribute('type', $VersioningType)
    # Replace the parameter list wholesale; a stale param from another
    # versioner would otherwise linger and confuse the GUI.
    foreach ($p in @($versioning.SelectNodes('param'))) { [void]$versioning.RemoveChild($p) }
    $param = $xml.CreateElement('param')
    $param.SetAttribute('key', 'maxAge')
    $param.SetAttribute('val', "$VersioningMaxAge")
    [void]$versioning.AppendChild($param)
    Set-Element -Parent $versioning -Name 'cleanupIntervalS' -Value '3600'
    Write-Log ("File versioning: {0}, maxAge {1}s ({2} days)" -f $VersioningType, $VersioningMaxAge, ($VersioningMaxAge / 86400))

    # -- default ignores ----------------------------------------------------
    $ignores = Get-OrAdd -Parent $defaults -Name 'ignores'
    foreach ($l in @($ignores.SelectNodes('line'))) { [void]$ignores.RemoveChild($l) }
    foreach ($pattern in $IgnoreLines) {
        $line = $xml.CreateElement('line')
        $line.InnerText = $pattern
        [void]$ignores.AppendChild($line)
    }
    Write-Log "Default ignore patterns: $($IgnoreLines.Count) lines"

    Set-EmptyElementsSelfClosing -Node $root

    # Write through a temp file so an interrupted run cannot leave a truncated
    # config behind.
    $tmp = "$cfgPath.desuq-tmp"
    $writer = New-Object System.Xml.XmlTextWriter($tmp, [System.Text.Encoding]::UTF8)
    try {
        $writer.Formatting = [System.Xml.Formatting]::Indented
        $writer.Indentation = 4
        $xml.Save($writer)
    } finally {
        $writer.Close()
    }
    Move-Item -LiteralPath $tmp -Destination $cfgPath -Force

    Set-Content -LiteralPath $sentinel -Value "$SeedVersion" -Encoding utf8 -NoNewline
    Write-Log "Seeded $cfgPath" 'Green'
    exit 0
}
catch {
    Write-Log "Seeding failed: $($_.Exception.Message)" 'Red'
    Write-Log 'Syncthing will start with upstream defaults.' 'Yellow'
    # Never fail the installation over cosmetic defaults.
    exit 0
}
