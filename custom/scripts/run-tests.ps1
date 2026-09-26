<#
.SYNOPSIS
    Runs every test suite this fork has, and says which ones it skipped.

.DESCRIPTION
    There are thirteen suites in four languages, six of them need jsdom and one
    needs two live Syncthing instances, so until this existed the only way to
    run them all was to remember twelve command lines. Nothing did, which is why
    nothing ran them.

    What it runs, cheapest first, so a typo does not cost a two-minute wait:

      1. check-handshake-words.ps1     wordlists      no dependencies
      2. go test lib/build lib/ur cmd  telemetry      Go
      3. go test lib/api lib/versioner REST handlers  Go
      4. go test (custom/tray)         tray           Go, nested module
      5. test-handshake.js             handshake      Node
      6. test-handshake-render.js      handshake UI   Node + jsdom
      7. test-lanlimit-render.js       LAN note       Node + jsdom
      8. test-wizard-render.js         first run      Node + jsdom
      9. test-home-render.js           main screen    Node + jsdom
     10. test-history-render.js        history        Node + jsdom
     11. test-seed-naming.ps1          seeding        the built binary
     12. test-selective-render.js      picker         jsdom + a live test pair
     13. test-blender-addon.py         Blender add-on Python (no Blender needed)

    A missing prerequisite is reported as SKIP rather than as failure, and the
    exit code is non-zero only if something actually failed. But a run that
    skipped half the suites still says so where it cannot be missed: "all
    green" over four skips is how a suite quietly stops covering anything.

.PARAMETER Quick
    Skip the two suites that need a built binary or a live pair. What is left
    runs in well under a minute from a clean checkout.

.PARAMETER NoPair
    Do not start a test pair. Suite 12 runs only if one is already up.

.PARAMETER Binary
    The Syncthing binary to test against. Defaults to custom/dist.

.PARAMETER RequireAll
    Treat a skipped suite as a failure. For CI, where every prerequisite is
    installed on purpose and a skip therefore means the detection broke rather
    than that something is genuinely unavailable.

.PARAMETER InstallJsdom
    npm install jsdom into a cache directory if it is not already reachable.
    Off by default, because installing packages is not something a test run
    should do behind your back. CI passes it.

.EXAMPLE
    .\custom\scripts\run-tests.ps1
.EXAMPLE
    .\custom\scripts\run-tests.ps1 -Quick
.EXAMPLE
    .\custom\scripts\run-tests.ps1 -InstallJsdom     # what CI does
#>
[CmdletBinding()]
param(
    [switch]$Quick,
    [switch]$NoPair,
    [string]$Binary,
    [switch]$InstallJsdom,
    [switch]$RequireAll
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Continue'

$RepoRoot  = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$ScriptDir = $PSScriptRoot
if (-not $Binary) { $Binary = Join-Path $RepoRoot 'custom\dist\desuq-syncthing.exe' }

$results = [System.Collections.Generic.List[object]]::new()

function Record {
    param([string]$Name, [string]$Status, [string]$Note = '')
    $results.Add([pscustomobject]@{ Name = $Name; Status = $Status; Note = $Note })
    $colour = switch ($Status) { 'PASS' { 'Green' } 'FAIL' { 'Red' } default { 'DarkYellow' } }
    $suffix = if ($Note) { "  -- $Note" } else { '' }
    Write-Host ('  {0,-4} {1}{2}' -f $Status, $Name, $suffix) -ForegroundColor $colour
}

function Section([string]$Title) {
    Write-Host ''
    Write-Host "-- $Title" -ForegroundColor Cyan
}

# Go is routinely not on PATH on a Windows dev box.
function Find-Go {
    $cmd = Get-Command go.exe -EA SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($c in @("$env:ProgramFiles\Go\bin\go.exe",
                     "${env:ProgramFiles(x86)}\Go\bin\go.exe",
                     "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
        if (Test-Path -LiteralPath $c) { return $c }
    }
    return $null
}

function Find-Node {
    $cmd = Get-Command node.exe -EA SilentlyContinue
    if ($cmd) { return $cmd.Source }
    return $null
}

# jsdom is a 30 MB dependency tree used by six test scripts and by nothing
# that ships, so it is not vendored and not in any package.json. Find it
# wherever it already is before considering installing it.
#
# Returns the directory to put on NODE_PATH, or '' if node already resolves
# jsdom by itself, or $null if it is not available at all.
function Resolve-Jsdom {
    param([string]$NodeExe, [switch]$Install)

    $candidates = @()
    if ($env:NODE_PATH) { $candidates += @($env:NODE_PATH -split ';' | Where-Object { $_ }) }
    $cache = Join-Path $env:TEMP 'desuq-jsdom\node_modules'
    $candidates += $cache
    $candidates += (Join-Path $RepoRoot 'node_modules')

    # Test for jsdom's package.json, not its directory. The cache is in TEMP,
    # and Windows' temp cleanup deletes old files one at a time: it left a
    # jsdom\ holding only lib\, which passed a directory check, was never
    # reinstalled, and failed every render suite with "not installed".
    foreach ($c in $candidates) {
        if (Test-Path -LiteralPath (Join-Path $c 'jsdom\package.json')) { return $c }
    }

    & $NodeExe -e "require.resolve('jsdom')" 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { return '' }

    if (-not $Install) { return $null }

    Write-Host '  installing jsdom (once, cached in TEMP)...' -ForegroundColor DarkGray
    $dir = Split-Path -Parent $cache
    # A partial tree from an earlier install is worse than none: npm sees
    # jsdom listed and leaves the gutted copy where it is.
    if (Test-Path -LiteralPath $dir) { Remove-Item -Recurse -Force -LiteralPath $dir }
    $null = New-Item -ItemType Directory -Force -Path $dir
    Push-Location $dir
    try { $npmOut = & npm install --no-audit --no-fund --loglevel=error jsdom 2>&1 }
    finally { Pop-Location }

    if (Test-Path -LiteralPath (Join-Path $cache 'jsdom\package.json')) { return $cache }
    # Say why. Discarding this turned a failed install into six suites
    # reporting "jsdom not found" with nothing to go on.
    Write-Host '  npm install jsdom did not produce a usable copy:' -ForegroundColor Yellow
    $npmOut | Select-Object -Last 15 | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow }
    return $null
}

function Invoke-Suite {
    param([string]$Name, [scriptblock]$Body)

    # $LASTEXITCODE is only touched by a native process or by a script that
    # calls exit, and under StrictMode reading it before anything has set it
    # throws. Reset it, then trust $? for the case where neither happened.
    $global:LASTEXITCODE = 0
    $out = & $Body 2>&1
    $ok   = $?
    $code = $LASTEXITCODE
    if ($code -eq 0 -and -not $ok) { $code = 1 }

    if ($code -ne 0) {
        Record $Name 'FAIL' "exit $code"
        $out | Select-Object -Last 25 | ForEach-Object {
            Write-Host "       $_" -ForegroundColor DarkGray
        }
        return
    }

    # Every suite prints its own verdict on the last line. Echoing it is the
    # cheapest evidence that something actually ran. `go test` ends on
    # whichever package happened to sort last, including "[no test files]"
    # ones, so prefer a line that says something.
    $lines = @($out | ForEach-Object { "$_".Trim() } | Where-Object { $_ })
    $last  = $lines | Where-Object { $_ -notmatch '^\?\s' } | Select-Object -Last 1
    if (-not $last) { $last = $lines | Select-Object -Last 1 }
    Record $Name 'PASS' "$last"
}

# /rest/noauth/health is the one route that answers without an API key
# (lib/api/api.go), which is exactly what a readiness probe wants -- inferring
# "up" from the shape of a 403 works until the error text changes wording.
function Test-PairUp {
    foreach ($p in @(8390, 8391)) {
        try {
            $r = Invoke-RestMethod -Uri "http://127.0.0.1:$p/rest/noauth/health" -TimeoutSec 3
            if ($r.status -ne 'OK') { return $false }
        } catch { return $false }
    }
    return $true
}

Write-Host 'desuqcafe-syncthing test suites' -ForegroundColor White
Write-Host "  repo: $RepoRoot"

$go   = Find-Go
$node = Find-Node

# --- 1. wordlists ----------------------------------------------------------
Section 'wordlists'
Invoke-Suite 'check-handshake-words' { & (Join-Path $ScriptDir 'check-handshake-words.ps1') }

# --- 2 to 4. Go ------------------------------------------------------------
Section 'Go'
if (-not $go) {
    Record 'go test (telemetry)' 'SKIP' 'go not found'
    Record 'go test (api)'       'SKIP' 'go not found'
    Record 'go test (tray)'      'SKIP' 'go not found'
} else {
    Invoke-Suite 'go test (telemetry)' {
        Push-Location $RepoRoot
        try { & $go test ./lib/build/... ./lib/ur/... ./cmd/syncthing/... }
        finally { Pop-Location }
    }
    # The fork's three server-side handlers live here -- diskfree, dirsizes and
    # reveal -- and until this suite existed none of them were covered by
    # anything the build ran. Upstream's own api tests come along for the ride
    # and take about seven seconds. lib/versioner is here too: pinned versions
    # (desuq_pins.go) are guarded inside upstream's cleanup, and a merge that
    # moved that cleanup would otherwise delete pinned copies without failing
    # anything the build ran.
    Invoke-Suite 'go test (api)' {
        Push-Location $RepoRoot
        try { & $go test ./lib/api/... ./lib/versioner/... } finally { Pop-Location }
    }
    Invoke-Suite 'go test (tray)' {
        Push-Location (Join-Path $RepoRoot 'custom\tray')
        try { & $go test ./... } finally { Pop-Location }
    }
}

# --- 5 to 10. Node ---------------------------------------------------------
Section 'Node'
$jsdomPath = $null
if (-not $node) {
    foreach ($n in @('test-handshake', 'test-handshake-render', 'test-lanlimit-render',
                     'test-wizard-render', 'test-home-render', 'test-history-render')) {
        Record $n 'SKIP' 'node not found'
    }
} else {
    Invoke-Suite 'test-handshake' { & $node (Join-Path $ScriptDir 'test-handshake.js') }

    $jsdomPath = Resolve-Jsdom -NodeExe $node -Install:$InstallJsdom
    if ($null -eq $jsdomPath) {
        foreach ($n in @('test-handshake-render', 'test-lanlimit-render',
                         'test-wizard-render', 'test-home-render', 'test-history-render')) {
            Record $n 'SKIP' 'jsdom not found; pass -InstallJsdom or set NODE_PATH'
        }
    } else {
        if ($jsdomPath) { $env:NODE_PATH = $jsdomPath }
        Invoke-Suite 'test-handshake-render' { & $node (Join-Path $ScriptDir 'test-handshake-render.js') }
        Invoke-Suite 'test-lanlimit-render'  { & $node (Join-Path $ScriptDir 'test-lanlimit-render.js') }
        Invoke-Suite 'test-wizard-render'    { & $node (Join-Path $ScriptDir 'test-wizard-render.js') }
        Invoke-Suite 'test-home-render'      { & $node (Join-Path $ScriptDir 'test-home-render.js') }
        Invoke-Suite 'test-history-render'   { & $node (Join-Path $ScriptDir 'test-history-render.js') }
    }
}

# --- the Blender add-on's client, in plain Python ---------------------------
# custom/blender/desuq_syncthing/client.py imports no bpy, so what it decides
# is checked with any Python. The part only Blender can show -- the handlers
# on open, save and quit -- is test-blender-live.ps1, by hand: CI has no
# Blender.
Section 'Python'
$python = $null
$candidates = @('python.exe', 'py.exe', 'python3.exe' | ForEach-Object {
        (Get-Command $_ -EA SilentlyContinue).Source })
# Blender carries its own Python, and a machine with Blender and nothing
# else is exactly where the add-on is used.
$candidates += @(Get-ChildItem "$env:ProgramFiles\Blender Foundation\*\*\python\bin\python.exe" -EA SilentlyContinue |
        ForEach-Object FullName)
foreach ($c in $candidates) {
    if (-not $c) { continue }
    # Asked, not trusted by path: without Python installed, Windows puts a
    # python.exe stub on PATH that prints where to get it and exits 9009 --
    # and a real Store install lives at the same WindowsApps path.
    $v = & $c -c 'import sys; print(sys.version_info[0])' 2>$null
    if ($LASTEXITCODE -eq 0 -and "$v".Trim() -eq '3') { $python = $c; break }
}
if (-not $python) {
    Record 'test-blender-addon' 'SKIP' 'python not found'
} else {
    Invoke-Suite 'test-blender-addon' { & $python (Join-Path $ScriptDir 'test-blender-addon.py') }
}

# --- 10. seeding, against the real binary -----------------------------------
Section 'the built binary'
$haveBinary = Test-Path -LiteralPath $Binary
if ($Quick) {
    Record 'test-seed-naming' 'SKIP' '-Quick'
} elseif (-not $haveBinary) {
    Record 'test-seed-naming' 'SKIP' "no binary at $Binary; build first"
} else {
    Invoke-Suite 'test-seed-naming' { & (Join-Path $ScriptDir 'test-seed-naming.ps1') -Binary $Binary }
}

# --- 11. the picker, against two live instances ----------------------------
Section 'two live instances'
$pairScript  = Join-Path $ScriptDir 'start-test-pair.ps1'
$startedPair = $false

if ($Quick) {
    Record 'test-selective-render' 'SKIP' '-Quick'
} elseif (-not $node) {
    Record 'test-selective-render' 'SKIP' 'node not found'
} elseif (-not $haveBinary) {
    Record 'test-selective-render' 'SKIP' "no binary at $Binary; build first"
} elseif ($null -eq $jsdomPath) {
    Record 'test-selective-render' 'SKIP' 'jsdom not found'
} else {
    if ($jsdomPath) { $env:NODE_PATH = $jsdomPath }
    $ready = Test-PairUp
    if (-not $ready) {
        if ($NoPair) {
            Record 'test-selective-render' 'SKIP' 'no pair running and -NoPair given'
        } else {
            Write-Host '  starting a test pair...' -ForegroundColor DarkGray
            & $pairScript -Fresh -WithFolder -Binary $Binary 2>&1 | Out-Null
            $startedPair = $true
            for ($i = 0; $i -lt 10 -and -not $ready; $i++) {
                $ready = Test-PairUp
                if (-not $ready) { Start-Sleep -Seconds 2 }
            }
            if (-not $ready) { Record 'test-selective-render' 'SKIP' 'the pair did not come up' }
        }
    }
    if ($ready) {
        Invoke-Suite 'test-selective-render' { & $node (Join-Path $ScriptDir 'test-selective-render.js') }
    }
    if ($startedPair) { & $pairScript -Stop 2>&1 | Out-Null }
}

# --- summary ---------------------------------------------------------------
$pass = @($results | Where-Object { $_.Status -eq 'PASS' }).Count
$fail = @($results | Where-Object { $_.Status -eq 'FAIL' }).Count
$skip = @($results | Where-Object { $_.Status -eq 'SKIP' }).Count

Write-Host ''
Write-Host ('{0} passed, {1} failed, {2} skipped' -f $pass, $fail, $skip) -ForegroundColor White

# A run that skipped things is not a run that passed. Say so where it cannot be
# missed, so "the tests are green" never quietly means "the tests did not run".
if ($skip -gt 0) {
    Write-Host ''
    Write-Host "$skip suite(s) did not run:" -ForegroundColor Yellow
    $results | Where-Object { $_.Status -eq 'SKIP' } | ForEach-Object {
        Write-Host ('  {0}: {1}' -f $_.Name, $_.Note) -ForegroundColor Yellow
    }
}

if ($fail -gt 0) {
    Write-Host ''
    Write-Host 'FAILED:' -ForegroundColor Red
    $results | Where-Object { $_.Status -eq 'FAIL' } | ForEach-Object {
        Write-Host ('  {0}: {1}' -f $_.Name, $_.Note) -ForegroundColor Red
    }
    exit 1
}

if ($RequireAll -and $skip -gt 0) {
    Write-Host ''
    Write-Host ('-RequireAll: {0} suite(s) were skipped, which here counts as failure.' -f $skip) `
        -ForegroundColor Red
    exit 1
}

exit 0
