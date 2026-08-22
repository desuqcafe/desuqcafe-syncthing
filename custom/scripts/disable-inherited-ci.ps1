<#
.SYNOPSIS
    Disables the upstream Syncthing GitHub Actions workflows inherited by this
    fork, leaving the fork's own release workflow enabled.

.DESCRIPTION
    Upstream's workflows are its full multi-platform release pipeline. On a fork
    they consume Actions minutes and fail on secrets we do not have.

    They are disabled through the GitHub API instead of by deleting the workflow
    files, so that merging upstream changes never produces a conflict over them.

    This state lives in GitHub, not in the repository, so re-run this after
    recreating the repository. Requires the GitHub CLI, authenticated:
        gh auth status

.PARAMETER Repo
    owner/name of the fork. Defaults to the origin remote.
#>
[CmdletBinding()]
param(
    [string]$Repo
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    throw "GitHub CLI not found. Install it with:  winget install GitHub.cli"
}

if (-not $Repo) {
    # Derive from the origin remote rather than `gh repo view`, which on a fork
    # resolves to the upstream parent repository instead of ours.
    $url = (git remote get-url origin).Trim()
    if ($url -match '[:/]([^/:]+)/([^/]+?)(\.git)?$') {
        $Repo = "$($Matches[1])/$($Matches[2])"
    } else {
        throw "Could not determine the repository from origin remote '$url'. Pass -Repo owner/name."
    }
}
Write-Host "Repository: $Repo" -ForegroundColor DarkGray

# Everything except our own workflow.
$keep = @('desuq-release.yaml')

$workflows = gh api "repos/$Repo/actions/workflows" --paginate --jq '.workflows[] | "\(.id)\t\(.state)\t\(.path)"'
if (-not $workflows) {
    Write-Host "No workflows registered yet. Push a commit first, then re-run." -ForegroundColor Yellow
    return
}

foreach ($line in $workflows) {
    $parts = $line -split "`t"
    if ($parts.Count -lt 3) {
        Write-Host "  skipped unparseable entry: $line" -ForegroundColor DarkGray
        continue
    }
    $id, $state, $path = $parts
    $file = Split-Path $path -Leaf

    if ($keep -contains $file) {
        if ($state -ne 'active') {
            gh api -X PUT "repos/$Repo/actions/workflows/$id/enable" | Out-Null
            Write-Host "  enabled  $file" -ForegroundColor Green
        } else {
            Write-Host "  kept     $file" -ForegroundColor Green
        }
        continue
    }

    if ($state -eq 'active') {
        gh api -X PUT "repos/$Repo/actions/workflows/$id/disable" | Out-Null
        Write-Host "  disabled $file" -ForegroundColor Yellow
    } else {
        Write-Host "  already off  $file" -ForegroundColor DarkGray
    }
}

Write-Host "`nDone." -ForegroundColor Green
