# Working in this repository

This is **desuqcafe-syncthing**, a fork of [Syncthing](https://github.com/syncthing/syncthing)
packaged as a per-user Windows installer.

## The one rule that matters

Keep the divergence from upstream minimal, so `git merge upstream/main` stays
boring. Concretely:

- **Add new files under `custom/`.** Upstream has no such directory, so nothing
  there can ever conflict.
- **Only edit an upstream file as a last resort**, and when you do, make the
  change additive with upstream's behaviour as the default.
- **Never rename the Go module or any package.** The module is still
  `github.com/syncthing/syncthing`. Renaming it would rewrite every import in
  the tree and make merges impossible.
- Prefer Syncthing's existing extension points over patching source:
  `--home`, `STGUIASSETS`, build tags, and the `ST_BRAND_*` env vars.
- Record any new upstream-file edit in the table in `custom/CUSTOMIZATIONS.md`.

As of now exactly **one** upstream file is modified: `build.go` (16 lines).

## Layout

| Path | What |
| --- | --- |
| `custom/branding.ps1` | Single source of truth for all naming |
| `custom/build-windows.ps1` | Builds the branded binary + installer |
| `custom/installer/installer.iss` | Inno Setup script (per-user, no admin) |
| `custom/scripts/sync-upstream.ps1` | Merge upstream and verify the build |
| `custom/scripts/disable-inherited-ci.ps1` | Turn off upstream's workflows |
| `custom/CUSTOMIZATIONS.md` | Divergence register and merge guide |
| `custom/DEPLOYMENT-3D-TEAM.md` | Recommended config for the target users |
| `.github/workflows/desuq-release.yaml` | Our release pipeline |

Everything else is upstream Syncthing, unmodified.

## Common commands

```powershell
.\custom\build-windows.ps1 -Installer          # build binary + installer
.\custom\scripts\sync-upstream.ps1 -DryRun     # preview upstream changes
git tag v2.1.4-desuq.2; git push origin v2.1.4-desuq.2   # cut a release
```

Requires Go (per `go.mod`) and Inno Setup. The build script provisions
`goversioninfo` itself.

## Who this is for

A developer sharing large binary assets (`.blend` files, textures) with two
non-technical 3D modellers. Decisions should favour "works without touching a
command line" over configurability. See `custom/DEPLOYMENT-3D-TEAM.md`.

## Gotchas

- Auto-upgrade is compiled out (`-no-upgrade`). Syncthing verifies upgrades
  against upstream's signing key, which cannot validate our builds.
- Upstream's inherited GitHub workflows are disabled **via the GitHub API**, not
  by deleting files, so they cannot conflict on merge. That state lives in
  GitHub, not the repo.
- `.stignore` is never synced between devices — it is per-machine by design.
