<#
.SYNOPSIS
    Merges the latest upstream Syncthing changes into this fork.

.DESCRIPTION
    The fork keeps its own work as a small set of additive changes on top of
    upstream, so this is normally a clean fast merge. See
    custom/CUSTOMIZATIONS.md for the full list of files that diverge.

    Run with -DryRun first to see what would come in.

.PARAMETER Ref
    Upstream ref to merge. Defaults to upstream/main. Use a release tag such as
    v2.1.4 to track stable releases instead of the development branch.

.PARAMETER DryRun
    Fetch and report what would be merged, then stop without changing anything.

.EXAMPLE
    .\custom\scripts\sync-upstream.ps1 -DryRun
    .\custom\scripts\sync-upstream.ps1 -Ref v2.1.4
#>
[CmdletBinding()]
param(
    [string]$Ref = 'upstream/main',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
Push-Location $RepoRoot
try {
    # Make sure the upstream remote exists and can never be pushed to.
    $remotes = git remote
    if ($remotes -notcontains 'upstream') {
        Write-Host "Adding upstream remote..." -ForegroundColor Cyan
        git remote add upstream https://github.com/syncthing/syncthing.git
        git remote set-url --push upstream DISABLED_PUSH_TO_UPSTREAM
    }

    if (git status --porcelain) {
        throw "Working tree is not clean. Commit or stash your changes first."
    }

    Write-Host "Fetching upstream..." -ForegroundColor Cyan
    git fetch upstream --tags --prune

    $behind = (git rev-list --count "HEAD..$Ref").Trim()
    $ahead  = (git rev-list --count "$Ref..HEAD").Trim()
    Write-Host "This branch is $ahead commit(s) ahead of and $behind commit(s) behind $Ref." -ForegroundColor DarkGray

    if ($behind -eq '0') {
        Write-Host "Already up to date with $Ref." -ForegroundColor Green
        return
    }

    Write-Host "`nIncoming commits:" -ForegroundColor Cyan
    git log --oneline --no-decorate "HEAD..$Ref" | Select-Object -First 40

    # Warn when upstream touched a file the fork also modifies. These are the
    # only places a conflict can realistically come from.
    $ours = @('build.go')
    $touched = git diff --name-only "HEAD...$Ref"
    $overlap = $ours | Where-Object { $touched -contains $_ }
    if ($overlap) {
        Write-Host "`nHeads up - upstream changed file(s) this fork also patches:" -ForegroundColor Yellow
        $overlap | ForEach-Object { Write-Host "  $_" -ForegroundColor Yellow }
        Write-Host "See custom/CUSTOMIZATIONS.md for what the fork changed there." -ForegroundColor Yellow
    }

    if ($DryRun) {
        Write-Host "`nDry run - nothing merged." -ForegroundColor Green
        return
    }

    Write-Host "`nMerging $Ref..." -ForegroundColor Cyan
    git merge --no-edit $Ref
    if ($LASTEXITCODE -ne 0) {
        Write-Host @"

Merge stopped with conflicts. To resolve:
  git status                 # see the conflicted files
  git mergetool  (or edit)   # resolve them
  git commit                 # finish the merge
Or abandon the attempt with:
  git merge --abort
"@ -ForegroundColor Yellow
        exit 1
    }

    Write-Host "Merged cleanly. Verifying the build still works..." -ForegroundColor Cyan
    & (Join-Path $RepoRoot 'custom\build-windows.ps1')
    Write-Host "`nUpstream merged and the fork still builds. Push with: git push origin" -ForegroundColor Green
}
finally {
    Pop-Location
}
