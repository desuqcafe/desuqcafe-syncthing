# What this fork changes, and how to stay mergeable with upstream

This fork exists so we can add our own behaviour later. The overriding design
rule right now is: **keep the divergence from upstream Syncthing as small as
possible**, so `git merge upstream/main` stays boring.

Everything below is deliberate. Read this before adding a change of your own.

## The rule

> Add new files under `custom/`. Only edit an upstream file when there is no
> other way, and when you do, make the edit additive and keep upstream's
> behaviour as the default.

Anything in `custom/` and `.github/workflows/desuq-release.yaml` can never
conflict, because upstream has no such files.

## Files we modify from upstream

This is the complete list. Keep it that way, and keep this table current.

| File | Change | Conflict risk |
| --- | --- | --- |
| `build.go` | Added an `envOr()` helper and used it for the six Windows version-resource strings in `shouldBuildSyso()` (product name, publisher, description, internal/original filename, icon). | **Low.** 16 lines in one rarely-touched function. With the `ST_BRAND_*` variables unset the behaviour is byte-for-byte upstream's, so the change is safe to keep across merges. |
| `lib/api/api.go` | **One line**, registering `GET /rest/system/diskfree`. The handler itself is in a new file, `lib/api/api_diskfree.go`. | **Low.** One entry in a long, alphabetically-ordered, append-only route table. If it ever conflicts the resolution is "keep both sides". |
| `gui/default/index.html` | Two additive hunks: a `<script>` tag for the fork's directive, and a "Disk Space" row in the folder detail table. 11 lines. | **Low–medium.** The file is large and upstream does edit it, but both hunks are additive and nowhere near each other. |
| `gui/default/syncthing/folder/editFolderModalView.html` | Three lines showing free space under the Folder Path field. | **Low.** |

Everything else the fork adds lives in files upstream does not have, so it
cannot conflict at all:

| New file | What |
| --- | --- |
| `lib/api/api_diskfree.go` | The `/rest/system/diskfree` handler |
| `gui/default/syncthing/desuq/` | The fork's Angular directives |
| `gui/violet/` | The violet theme |
| `custom/` | Everything else |

In particular we have **not**:

- renamed the Go module (`github.com/syncthing/syncthing`) — renaming it would
  rewrite every import in the tree and make merges impossible;
- renamed packages, types or internal identifiers;
- added anything to the root `go.mod` or `go.sum`;
- changed the behaviour of any existing upstream code path. Every edit above
  is an addition; with the fork's new files removed, the four modified files
  would still behave exactly as upstream's do.

## How the branding is done without touching upstream

| What | How |
| --- | --- |
| Executable name (`desuq-syncthing.exe`) | The build script renames build.go's output. No source change. |
| Publisher / product shown in file properties and Add-Remove Programs | `ST_BRAND_*` environment variables read by the patched `build.go`. |
| Separate config and database (`%LOCALAPPDATA%\desuqcafe-syncthing`) | The shortcuts pass `--home=...`. Upstream already supports this flag; no source change. This is also why the fork can run alongside a stock Syncthing install. |
| GUI port | Not forced. Syncthing probes for a free port on first start, so it settles on 8384 or the next free port automatically. |
| Auto-upgrade | Compiled out with the `noupgrade` build tag (see below). No source change. |
| GUI logo / CSS *(not used yet)* | Syncthing serves `$STGUIASSETS/<theme>/<file>` in preference to its built-in copy and falls back per file, so dropping art into an assets directory rebrands the web UI **without editing `gui/`**. Wire it up by setting `STGUIASSETS` in the shortcuts. |
| First-run defaults (versioning, disk reserve, ignore patterns, theme, device name) | `custom/scripts/seed-config.ps1`, run by the installer before first launch. It calls Syncthing's own `generate` subcommand to create `config.xml` and the device keys, then patches the `<defaults>` block. No source change; see `DEPLOYMENT-3D-TEAM.md` for the values and the reasoning. |
| Violet web UI theme | A new directory, `gui/violet/`. `lib/api`'s static server discovers theme directories by itself and falls back to `gui/default` per file, so only `theme.css` had to be written. `lib/api/auto/gui.files.go` is generated at build time and **gitignored**, so compiling the theme in adds nothing to the diff. |
| Notification-area icon | `custom/tray/`, **a separate Go module** — see below. It talks to Syncthing only over the REST API. |
| Desktop notifications | Also `custom/tray/`. It subscribes to Syncthing's `/rest/events` long poll and raises Windows toasts through WinRT. No source change, and no new dependency: WinRT is reached through `combase.dll` with `syscall`, and toast clicks use protocol activation so nothing has to be registered with COM. |

## Why the tray is its own Go module

`custom/tray` has its own `go.mod` and `go.sum`. The alternative — adding
`fyne.io/systray` to the root `go.mod` — would put our lines in two files that
upstream edits on every dependency bump, guaranteeing a conflict on each merge,
for a program that is not part of Syncthing.

A nested module is invisible to the parent: `go build ./...` at the repository
root skips the directory entirely, and upstream's `go.mod` and `go.sum` stay
byte-for-byte theirs. `custom/build-windows.ps1` builds it as a second step.

The tray reads Syncthing's state through the REST API and its address and API
key out of `config.xml`. It deliberately does not import `lib/config` or
anything else from the tree above it, so an upstream change to those packages
cannot break it.

That isolation is also why the desktop notifications live here rather than in
`lib/`. They need only two things Syncthing already exposes — the event stream
and the fork's own `/rest/system/diskfree` — so putting them in the tray costs
**zero** additional divergence from upstream, while a notifier inside `lib/`
would mean patching the model or the API service. See
`DEPLOYMENT-3D-TEAM.md` section 8 for which events become toasts and why the
list is deliberately short.

All names live in one place: `custom/branding.ps1`.

## Why auto-upgrade is disabled

Syncthing verifies downloaded upgrades against the upstream project's release
signing key (`lib/upgrade`, `signature.Verify(SigningKey, ...)`). That key
cannot validate builds produced from this fork. Leaving the upgrade feature on
would therefore either fail, or — worse — replace our build with stock
Syncthing and silently drop our customisations.

So releases are built with `-no-upgrade`, which strips the feature entirely.
Users update by running a newer installer.

If we ever want real in-app updates for the fork, the work is: generate our own
signing key, replace `SigningKey` in `lib/upgrade`, sign each release, and point
`ReleasesURL` at our own metadata. That is a genuine feature, not a config
tweak, and it means guarding a private key.

## Merging upstream

```powershell
.\custom\scripts\sync-upstream.ps1 -DryRun   # see what is coming
.\custom\scripts\sync-upstream.ps1           # merge, then verify the build
```

The script refuses to run on a dirty tree, warns you if upstream touched
`build.go`, merges, and then rebuilds to prove the fork still compiles.

To track stable releases rather than upstream's development branch, pass a tag:

```powershell
.\custom\scripts\sync-upstream.ps1 -Ref v2.1.4
```

If `build.go` ever does conflict, the resolution is always the same: take
upstream's version of the function and re-apply the `envOr(...)` wrappers.

## Upstream's CI is disabled on this fork

The inherited workflows (`build-syncthing.yaml` and friends) are upstream's full
multi-platform release pipeline. On a fork they burn Actions minutes and fail on
missing secrets.

They are switched off **through the GitHub API rather than by deleting the
files**, precisely so they never conflict during a merge. Re-apply the setting
with:

```powershell
.\custom\scripts\disable-inherited-ci.ps1
```

Because that state lives in GitHub rather than in the repository, it must be
re-applied if the repository is ever recreated or cloned to a new remote.

Our own pipeline is `.github/workflows/desuq-release.yaml`.
