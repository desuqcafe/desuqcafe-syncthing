# What this fork changes, and what that costs at merge time

This is a fork, and it is becoming its own product rather than a patch set on
somebody else's. **Divergence is expected.** Where the right design needs an
upstream file edited, it gets edited.

What this file is for is making that divergence legible: every change from
upstream, what it does, and how a conflict resolves if upstream ever touches
the same lines. It is the merge plan. Read it before adding a change of your
own, and add yours to it.

## Where a change costs nothing

New files cannot conflict, because upstream has no such paths: `custom/`,
`gui/violet/`, `gui/default/syncthing/desuq/`, `lib/api/api_diskfree.go` and
`.github/workflows/desuq-release.yaml`. That is a convenience worth taking when
it is free, and worth ignoring when the alternative is a worse design.

Two constraints are still worth honouring, for reasons that are not about
merge tidiness:

- **The Go module keeps its name.** Renaming
  `github.com/syncthing/syncthing` rewrites every import in the tree and
  changes nothing a user can see.
- **New dependencies go in a nested module.** A line in the root `go.mod` or
  `go.sum` conflicts on every upstream dependency bump — a recurring tax, for
  a one-off convenience. `custom/tray/` is a separate module for this reason.

## Files we modify from upstream

Keep this table current. It is the only place the merge cost is written down.

| File | Change | Conflict risk |
| --- | --- | --- |
| `build.go` | Added an `envOr()` helper and used it for the six Windows version-resource strings in `shouldBuildSyso()` (product name, publisher, description, internal/original filename, icon). | **Low.** 16 lines in one rarely-touched function. With the `ST_BRAND_*` variables unset the behaviour is byte-for-byte upstream's, so the change is safe to keep across merges. |
| `lib/api/api.go` | **One line**, registering `GET /rest/system/diskfree`. The handler itself is in a new file, `lib/api/api_diskfree.go`. | **Low.** One entry in a long, alphabetically-ordered, append-only route table. If it ever conflicts the resolution is "keep both sides". |
| `gui/default/index.html` | Eight additive hunks, ~41 lines, plus one removal (the two `<ng-include>`s for upstream's usage-report modals, replaced by a comment saying why): three `<link>` and five `<script>` tags for the fork's GUI files, the `<desuq-selective-modal>` element, a "Disk Space" row in the folder detail table, a "Verification" row in the device detail table, and a "Choose Files" button in the folder panel's footer. | **Low–medium.** The file is large and upstream does edit it, but every hunk is additive and they are far apart. |
| `gui/default/syncthing/folder/editFolderModalView.html` | Seven lines: three showing free space under the Folder Path field, four placing `<desuq-selective-option>` at the top of the Ignores tab. | **Low.** |
| `gui/default/syncthing/device/editDeviceModalView.html` | Ten lines: five placing the device verification card under the Device ID field, five placing `<desuq-lan-limit>` under the per-device rate limits. | **Low.** |
| `gui/default/syncthing/settings/settingsModalView.html` | Seven lines placing `<desuq-lan-limit>` between the rate fields and the "Limit Bandwidth in LAN" checkbox. | **Low.** |
| `gui/default/syncthing/core/syncthingController.js` | **One branch**, 13 lines with the comment, at the top of `saveFolder`'s new-folder handling. It hands the save to the selective-sync picker, and is guarded on both a flag only the fork's directive sets and a service only the fork publishes — so with `syncthing/desuq/` removed it is unreachable and the function is upstream's. | **Low–medium.** The only fork edit inside an upstream *function* rather than beside one. It sits between two comment-led blocks that have been stable for years, and it is a self-contained early return, so a conflict resolves by re-inserting it wherever the equivalent point ends up. |
| `gui/default/syncthing/core/syncthingController.js` (2) | **Three removals**, each replaced by a comment: the two blocks that raise the usage-reporting nag, and the two lines in `saveSettings` where choosing the release-candidate upgrade channel silently sets `urAccepted`. | **Medium.** Removals inside upstream functions. If upstream edits them the merge will conflict; the resolution is to delete their side again. |
| `lib/config/optionsconfiguration.go` | **Two struct tags.** `URAccepted` gains `default:"-1"` (upstream has none, i.e. 0, "not yet asked"); `CREnabled` loses `default:"true"`. | **Low.** Two lines in a long field list. A conflict resolves by re-applying the two tags to whatever upstream's line has become. |
| `lib/ur/usage_report.go` | **One guard**, 8 lines with the comment, at the top of `Serve`. Returns an inert service when `build.TelemetryEnabled` is false. | **Low.** Additive, first statement of the function. |
| `lib/ur/failurereporting.go` | **One guard**, 7 lines, at the top of `Serve`. Same shape; also means the handler never subscribes to `events.Failure`. | **Low.** Additive, first statement of the function. |
| `cmd/syncthing/monitor.go` | **One guard**, 7 lines, at the top of `maybeReportPanics`. This is the reporter upstream leaves *on*. | **Low.** Additive, first statement of the function. |
| `lib/syncthing/syncthing.go` | **One condition**, `if build.IsCandidate` becomes `if build.IsCandidate && build.TelemetryEnabled`, plus four comment lines. | **Low.** One token on one line. If it conflicts, re-add the conjunct. |
| `gui/default/syncthing/settings/settingsModalView.html` (2) | Upstream's "Anonymous Usage Reporting" `<select>` replaced by a static note saying the build sends none. | **Low–medium.** This one *replaces* rather than inserts. A conflict resolves by deleting upstream's control again. |

Everything else the fork adds lives in files upstream does not have, so it
cannot conflict at all:

| New file | What |
| --- | --- |
| `lib/api/api_diskfree.go` | The `/rest/system/diskfree` handler |
| `gui/default/syncthing/desuq/` | The fork's Angular directives, its wordlists and its CSS |
| `gui/violet/` | The violet theme |
| `custom/` | Everything else |

Where the fork stands today: **159 inserted lines against 51 deleted**, across
those twelve files.

Up to the telemetry work almost every edit was inserted *beside* upstream's
code rather than in place of it, which is why merges had been boring. Stripping
the telemetry is the first change that had to delete things — upstream's
consent nag, its usage-reporting dropdown, and the two lines where picking the
release-candidate upgrade channel quietly opts you in. There was no additive
way to remove a control, and a switch left on screen wired to nothing would
have been worse than a merge conflict.

So the four rows marked **Medium** above are the ones to read before a merge.
Everything else still resolves by keeping both sides.

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
| Verified device handshake | `gui/default/syncthing/desuq/`, plus five lines in the device modal and one row in the device panel. Entirely client-side: the phrase is a SHA-256 of the two device IDs, computed in the browser, so there is **no new REST route and no server code at all**. See `DEPLOYMENT-3D-TEAM.md` section 9. |
| Selective sync file picker | `gui/default/syncthing/desuq/selectiveSync.js` and friends, plus the four upstream hunks above. Also **no server code**: it is built entirely out of `/rest/db/browse`, which already serves the *global* tree, and `/rest/db/ignores`. The fancytree it renders in is one upstream already ships for the version restorer. See `DEPLOYMENT-3D-TEAM.md` section 2. |
| A rate limit that says whether it applies | `gui/default/syncthing/desuq/lanLimitDirective.js`, plus a line in each of the two dialogues with rate fields. Reads `options.limitBandwidthInLan` and, where there is a device, `isLocal` from `/rest/system/connections`. No server code. The **default is left as upstream's**; see `DEPLOYMENT-3D-TEAM.md` section 11 for why changing it would be wrong. |
| No telemetry of any kind | `lib/build/desuq_telemetry.go` — a `const TelemetryEnabled = false` that all three of upstream's reporters consult, plus the GUI removals above and two seeded config values. See the section below. |
| Synced folders visible in Explorer | `custom/tray/foldericon*.go`. A `desktop.ini` per folder, written by the tray -- per user, no administrator, no COM, no registration, and **no upstream change of any kind**. Explicitly *not* an overlay-icon shell extension: those need a registered in-process COM server and compete for about fifteen global slots Dropbox and OneDrive already fill. See `DEPLOYMENT-3D-TEAM.md` section 10. |

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

The Explorer folder icons live here for the same reason. They need the folder
list and the ignore patterns, both of which the REST API already serves, and
they write files on the machine the tray is running on -- so putting them in
the tray costs no divergence at all, where a shell integration inside `lib/`
would mean a Windows-only dependency in a cross-platform tree.

That isolation is also why the desktop notifications live here rather than in
`lib/`. They need only two things Syncthing already exposes — the event stream
and the fork's own `/rest/system/diskfree` — so putting them in the tray costs
**zero** additional divergence from upstream, while a notifier inside `lib/`
would mean patching the model or the API service. See
`DEPLOYMENT-3D-TEAM.md` section 8 for which events become toasts and why the
list is deliberately short.

All names live in one place: `custom/branding.ps1`.

## Why the telemetry is compiled out rather than switched off

Upstream has **three** reporters, gated by **two** options, and the survey most
people do finds only the first two:

| Reporter | Where | Posts to | Gate | Default |
| --- | --- | --- | --- | --- |
| Usage report | `lib/ur/usage_report.go` | `Options.URURL`, `data.syncthing.net` | `URAccepted >= 2` | off (0, "not yet asked") |
| Failure reports | `lib/ur/failurereporting.go` | `Options.CRURL` + `/failure` | `URAccepted > 0` | off |
| **Panic-log upload** | `cmd/syncthing/crash_reporting.go`, called from `monitor.go` | `Options.CRURL`, `crash.syncthing.net` | **`CREnabled`** | **on** |

The third one is the interesting one. It is gated on a *different* option,
`CREnabled`, which upstream defaults to `true`, and unlike the other two it
never asks. A stock build that crashes uploads its panic log — which contains
goroutine stacks and the tail of the log — without the user having agreed to
anything. `lib/ur`'s and `cmd/syncthing`'s tests demonstrate this: forcing
`TelemetryEnabled` to `true` and re-running them shows the crash server
contacted and the panic log renamed to `.reported.log`.

Setting the options is therefore not enough on its own, for three reasons:

1. It would only fix a config *we* wrote. `--home` pointed somewhere new, a
   hand-run `generate`, a config restored from a backup: each starts over.
2. `CREnabled` and `URAccepted` are two separate switches, and the seed script
   would have to keep tracking whatever upstream adds next.
3. The GUI offered a dropdown to turn usage reporting back on, and picking the
   release-candidate upgrade channel turned it on without saying so.

So the guarantee lives in a constant instead. Every reporter consults
`build.TelemetryEnabled` before doing anything, the GUI no longer offers a
control, and the options are *also* defaulted and seeded off — but only so
that `config.xml` does not claim something the binary will not do. That part
is cosmetic; the constant is what holds.

A constant rather than a build tag on purpose: a tag someone forgets to pass
is a silent regression, and there is no build of this fork that should ever
want telemetry.

What is deliberately **not** removed: `/rest/svc/report` and the report-building
code itself. Building a report is local and harmless, it is what upstream's
"preview" showed, and deleting `lib/ur` outright would mean touching
`lib/model`, `lib/api`, `lib/syncthing` and the generated mocks for no change
in what leaves the machine.

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
