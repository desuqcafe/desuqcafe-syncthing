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
- **New dependencies go in a nested module, not the root `go.mod`.** Adding a
  line to upstream's `go.mod`/`go.sum` means a conflict on every dependency
  bump they make. `custom/tray/` is a separate module for exactly this reason.
- Prefer Syncthing's existing extension points over patching source:
  `--home`, `STGUIASSETS`, build tags, and the `ST_BRAND_*` env vars.
- Record any new upstream-file edit in the table in `custom/CUSTOMIZATIONS.md`.

Six upstream files are modified, none by more than a few dozen lines, and every
edit is additive. `custom/CUSTOMIZATIONS.md` has the table; keep it current.
Only one of the six — `syncthingController.js` — is edited *inside* an upstream
function rather than beside one, and that edit is a guarded early return.

## Layout

| Path | What |
| --- | --- |
| `custom/branding.ps1` | Single source of truth for all naming |
| `custom/build-windows.ps1` | Builds the branded binary + installer |
| `custom/installer/installer.iss` | Inno Setup script (per-user, no admin) |
| `custom/tray/` | Notification-area app, desktop notifications, and the Explorer folder icons. **Its own Go module** |
| `custom/scripts/seed-config.ps1` | Writes first-run `config.xml` defaults |
| `custom/scripts/sync-upstream.ps1` | Merge upstream and verify the build |
| `custom/scripts/start-test-pair.ps1` | Two throwaway instances sharing a folder, for two-device testing |
| `custom/scripts/check-handshake-words.ps1` | Asserts the verification wordlists stay distinct. Run by the build |
| `custom/scripts/test-selective-render.js` | Drives the selective-sync picker through real Angular and a live instance. Needs jsdom and a running test pair |
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

# Two-device testing. Most of what this fork adds only happens between two
# devices, so this is usually the first thing to run.
.\custom\scripts\start-test-pair.ps1 -Fresh -WithFolder   # A on 8390, B on 8391
.\custom\scripts\start-test-pair.ps1 -Stop
```

Requires Go (per `go.mod`) and Inno Setup. The build script provisions
`goversioninfo` itself.

To iterate on `gui/` without rebuilding the binary, point `STGUIASSETS` at the
repository's `gui` directory before starting Syncthing. It serves those files
in preference to the compiled blob, falling back per file, so a browser refresh
is the whole edit loop.

## Who this is for

A developer sharing large binary assets (`.blend` files, textures) with two
non-technical 3D modellers. Decisions should favour "works without touching a
command line" over configurability. See `custom/DEPLOYMENT-3D-TEAM.md`.

## Gotchas

- The branded binary is linked `-H windowsgui`. PowerShell's call operator
  neither waits for it nor sets `$LASTEXITCODE`; use `Start-Process -Wait
  -PassThru`. Same for the tray.
- `syncthing serve` exits **0** when an instance is already serving that home.
  Anything supervising it must check reachability first, or it respawn-loops.
- `lib/api/auto/gui.files.go` is gitignored and regenerated whenever `gui/`
  changes, so new GUI files cost no tracked diff.
- Fork-added GUI strings should use plain `{{ }}` interpolation.
  angular-translate renders `{%placeholders%}` literally for any string missing
  from `assets/lang`, which ours always are.

- Auto-upgrade is compiled out (`-no-upgrade`). Syncthing verifies upgrades
  against upstream's signing key, which cannot validate our builds.
- Upstream's inherited GitHub workflows are disabled **via the GitHub API**, not
  by deleting files, so they cannot conflict on merge. That state lives in
  GitHub, not the repo.
- `.stignore` is never synced between devices — it is per-machine by design.
- **`/rest/events` IDs are per subscription, not global.** Syncthing keeps one
  buffer per distinct `events=` mask and numbers each from 1. Bootstrapping
  "where is now" with one mask and then polling with another silently drops
  events. Ask with the same mask you intend to poll with.
- Windows suppresses toasts while anything is full screen (automatic Do Not
  Disturb). They land in the Action Centre instead, so a notifier that looks
  broken during testing may be working perfectly.
- `window.crypto.subtle` is **undefined** in the GUI whenever it is reached at
  anything but `127.0.0.1` over plain http — a LAN address is not a secure
  context. Anything cryptographic in `gui/` has to carry its own implementation.
- **Ignore patterns escape with `|` on Windows, not `\`.** `lib/ignore` swaps
  the escape character at init because backslash is the path separator, and a
  backslash-escaped pattern is silently rewritten into a different path rather
  than rejected. Ask `/rest/system/version` rather than assuming.
- `POST /rest/db/ignores` answers **200 and then reports the parse failure in
  the response body**. A folder whose ignore file will not parse refuses to
  scan or pull at all, so ignoring that field looks exactly like a folder that
  never syncs.
- An `#include` line in the *default* ignores deadlocks every newly accepted
  folder: the included file is inside the folder, which cannot sync until the
  include resolves. See `DEPLOYMENT-3D-TEAM.md` section 2.
- A `desktop.ini` does nothing unless the folder itself carries the read-only
  or system attribute, and `SHGetFileInfo` ignores it entirely when COM has not
  been initialised -- it returns the generic icon and no error. Both are silent
  failures. See `DEPLOYMENT-3D-TEAM.md` section 10.
- The device-verification wordlists in `gui/default/syncthing/desuq/` are data,
  not prose: a word's *index* is its meaning. Re-ordering a list or inserting
  into the middle of one silently invalidates every verification anyone has
  already done. Append only, and re-run
  `custom/scripts/check-handshake-words.ps1` (the build does too).
