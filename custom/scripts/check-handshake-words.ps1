<#
.SYNOPSIS
    Verifies the device-verification wordlists are still safe to read aloud.

.DESCRIPTION
    The device handshake reduces a pair of device IDs to three words and a rank
    (gui/default/syncthing/desuq/). Its whole value is that two people reading
    the phrase to each other on a call will notice a mismatch. That breaks the
    moment two words in the same list sound alike: if "SLASH" and "CLASH" were
    both in it, a mismatched pair could sound matched, and the check would give
    false confidence rather than none -- the worst outcome available.

    So the lists have to hold three properties, and this asserts them:

      * exactly 256 entries per list, all A-Z uppercase.
        256 is not cosmetic. Each word encodes one byte of the digest, so a
        list of any other length would either waste entropy or index out of
        bounds.
      * no two words in a list share their first three letters.
      * no two words in a list are closer than a Levenshtein distance of 3.

    Levenshtein over spelling is a proxy for phonetic distance rather than the
    real thing, but it is a good one for this alphabet and it is mechanical,
    which hand-checking 32,640 pairs per list is not.

    Run it after touching handshakeWords.js. The Windows build runs it too, so
    a list that has drifted fails the build rather than shipping.

.EXAMPLE
    .\custom\scripts\check-handshake-words.ps1
#>
[CmdletBinding()]
param(
    [string]$WordsFile = (Join-Path $PSScriptRoot '..\..\gui\default\syncthing\desuq\handshakeWords.js')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$MinDistance = 3
$PrefixLength = 3
$Expected = 256

function Get-Levenshtein {
    param([string]$A, [string]$B)

    if ($A -eq $B) { return 0 }
    $n = $A.Length
    $m = $B.Length
    if ($n -eq 0) { return $m }
    if ($m -eq 0) { return $n }

    $prev = New-Object 'int[]' ($m + 1)
    $cur = New-Object 'int[]' ($m + 1)
    for ($j = 0; $j -le $m; $j++) { $prev[$j] = $j }

    for ($i = 1; $i -le $n; $i++) {
        $cur[0] = $i
        for ($j = 1; $j -le $m; $j++) {
            $cost = if ($A[$i - 1] -eq $B[$j - 1]) { 0 } else { 1 }
            $del = $prev[$j] + 1
            $ins = $cur[$j - 1] + 1
            $sub = $prev[$j - 1] + $cost
            $cur[$j] = [Math]::Min([Math]::Min($del, $ins), $sub)
        }
        $tmp = $prev; $prev = $cur; $cur = $tmp
    }
    return $prev[$m]
}

if (-not (Test-Path -LiteralPath $WordsFile)) {
    throw "Wordlist not found: $WordsFile"
}
$source = Get-Content -LiteralPath $WordsFile -Raw

# Pull each `name: [ 'A', 'B', ... ]` block out of the JS rather than trying to
# execute it. The file is generated data with a fixed shape, so a parse is
# enough and it keeps this script free of a JavaScript runtime.
$listPattern = "(?ms)^\s{8}(\w+):\s*\[(.*?)^\s{8}\]"
# NB: not $matches -- that is an automatic variable, and the -cnotmatch
# below overwrites it mid-loop.
$listMatches = [regex]::Matches($source, $listPattern)

if ($listMatches.Count -eq 0) {
    throw "No wordlists found in $WordsFile -- has its shape changed?"
}

$problems = @()

foreach ($m in $listMatches) {
    $name = $m.Groups[1].Value
    $words = [regex]::Matches($m.Groups[2].Value, "'([A-Za-z]+)'") |
        ForEach-Object { $_.Groups[1].Value }

    Write-Host "Checking '$name' ($($words.Count) words)..."

    if ($words.Count -ne $Expected) {
        $problems += "$name has $($words.Count) words; each list must hold exactly $Expected."
    }

    foreach ($w in $words) {
        if ($w -cnotmatch '^[A-Z]+$') {
            $problems += "$name contains '$w', which is not plain uppercase A-Z."
        }
    }

    $dupes = $words | Group-Object | Where-Object { $_.Count -gt 1 }
    foreach ($d in $dupes) {
        $problems += "$name repeats '$($d.Name)' $($d.Count) times."
    }

    $byPrefix = $words | Group-Object { $_.Substring(0, [Math]::Min($PrefixLength, $_.Length)) } |
        Where-Object { $_.Count -gt 1 }
    foreach ($g in $byPrefix) {
        $problems += "${name}: $($g.Group -join ', ') share the prefix '$($g.Name)'."
    }

    # O(n^2), but n is 256 and this runs once per build.
    for ($i = 0; $i -lt $words.Count; $i++) {
        for ($j = $i + 1; $j -lt $words.Count; $j++) {
            $d = Get-Levenshtein $words[$i] $words[$j]
            if ($d -lt $MinDistance) {
                $problems += "${name}: '$($words[$i])' and '$($words[$j])' are only $d edits apart (need $MinDistance)."
            }
        }
    }
}

if ($problems.Count -gt 0) {
    Write-Host ''
    Write-Host "Wordlist check FAILED with $($problems.Count) problem(s):" -ForegroundColor Red
    $problems | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    Write-Host ''
    Write-Host 'These lists get read aloud over the phone. Two words that sound alike'
    Write-Host 'turn a mismatch into an apparent match, which is worse than not'
    Write-Host 'checking at all. Replace the offending word rather than lowering the bar.'
    exit 1
}

Write-Host ''
Write-Host "Wordlists OK: $($listMatches.Count) lists, $Expected words each, minimum edit distance $MinDistance." -ForegroundColor Green
