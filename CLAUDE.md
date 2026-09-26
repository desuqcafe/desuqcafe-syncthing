# Working in this repository

This is **desuqcafe-syncthing**, a fork of [Syncthing](https://github.com/syncthing/syncthing)
packaged as a per-user Windows installer.

## How much to diverge from upstream

This is a fork, and it is becoming its own thing. **Divergence is expected and
is not a failure.** If the right design needs an upstream file edited, edit it;
do not contort a feature to avoid touching one.

What still matters is that the divergence is *deliberate and legible*, so a
future `git merge upstream/main` is work you can plan rather than a surprise:

- **Record every upstream-file edit** in the table in
  `custom/CUSTOMIZATIONS.md` — what it does, and how a conflict resolves. That
  table is the merge plan. Keeping it current is the whole discipline; the rest
  below is preference.
- **Prefer additive edits where they cost nothing.** A guarded branch beside
  upstream's logic conflicts far less often than a rewrite of it, and usually
  reads better anyway. Where being additive would mean a worse design, take the
  better design.
- **New files still cannot conflict at all**, so `custom/`, `gui/violet/` and
  `gui/default/syncthing/desuq/` remain the cheapest place to put things. A
  convenience, not a rule.
- **Use Syncthing's extension points when they are as good** as the
  alternative: `--home`, `STGUIASSETS`, build tags, the `ST_BRAND_*` env vars.
- **Still do not rename the Go module or its packages.** Not because merges are
  sacred, but because it rewrites every import in the tree for nothing a user
  can see. If that ever becomes worth doing it is a project of its own.
- **New dependencies still go in a nested module** — `custom/tray/` is one.
  A line in the root `go.mod`/`go.sum` is a conflict on every upstream
  dependency bump: a recurring tax for a one-off convenience.

Twenty upstream files carry fork edits today, 664 insertions against 210
deletions. Stripping the telemetry is what changed the character of that: it
is the first work that had to *delete* upstream behaviour rather than sit
beside it, because there is no additive way to remove a consent nag or a
settings control that no longer does anything. Several rows in
`CUSTOMIZATIONS.md` are marked **Medium** for that reason.

The main screen is the largest divergence so far and deliberately did *not*
follow that pattern. `<desuq-home>` is placed above upstream's first row and
that row is wrapped in a collapsed `<details>`, rather than being replaced —
so the biggest visual change the fork has made costs nine deleted lines in
`index.html`. Upstream's markup carries thirty-one actions, several of them
destructive, and the replacement should earn their deletion by covering the
cases first. Deleting it is a later, separate pass.

That pass has been costed — see `DEPLOYMENT-3D-TEAM.md` §18. Of the thirty-one,
seven were covered by the new screen, four are one click deeper, two are dead,
and eighteen had no entry point anywhere. **The three of those eighteen that
were capabilities rather than shortcuts now have one**: `restoreVersions.show`
is the History screen, `revertOverrideConfirmationModal('revert')` is *Undo my
changes here*, and `showFailed` is *N that would not sync* — all three on the
folder card, all three calling upstream's own handlers, because every modal is
`<ng-include>`d **outside** the region.

The remaining fifteen are shortcuts, and the answer on deleting the region is
still "not yet": `showListenerStatus` and `showDiscoveryStatus` have no second
door and no replacement planned, and after a deletion the log viewer is all
that is left.

`README.md` is the only row marked **High**:
it is a full rewrite, so every upstream README change conflicts. That was
chosen rather than accepted — the file has no behaviour and nothing reads it,
so a conflict costs one discarded diff, and "keep ours" is always the answer.
Everything else still resolves by keeping both sides.

## Layout

| Path | What |
| --- | --- |
| `custom/branding.ps1` | Single source of truth for all naming |
| `custom/build-windows.ps1` | Builds the branded binary + installer |
| `custom/installer/installer.iss` | Inno Setup script (per-user, no admin) |
| `custom/tray/` | Notification-area app, desktop notifications, the Explorer folder icons and their live hover text, and the one-shots behind Send To and the `.blend` right-click menu. **Its own Go module** |
| `custom/scripts/seed-config.ps1` | Writes first-run `config.xml` defaults |
| `custom/scripts/sync-upstream.ps1` | Merge upstream and verify the build |
| `custom/scripts/start-test-pair.ps1` | Two throwaway instances sharing a folder, for two-device testing |
| `custom/scripts/run-tests.ps1` | **Runs all twelve suites.** `-Quick` skips the two needing a binary or a live pair. What the build and CI both call |
| `custom/scripts/check-handshake-words.ps1` | Asserts the verification wordlists stay distinct. Run by the build even with `-SkipTests` |
| `custom/scripts/test-selective-render.js` | Drives the selective-sync picker through real Angular and a live instance. Needs jsdom and a running test pair |
| `custom/scripts/test-lanlimit-render.js` | Renders the LAN rate-limit note through real Angular. Needs jsdom; no instance required |
| `custom/scripts/test-wizard-render.js` | Drives the first-run setup guide through real Angular against canned REST. Needs jsdom; no instance required |
| `custom/scripts/test-home-render.js` | Drives the main screen the same way, and asserts the headline rules directly as a pure function. Needs jsdom; no instance required |
| `custom/scripts/test-history-render.js` | Drives the history screen. The restart guard and the run collapse are what it tries to break. Needs jsdom; no instance required |
| `custom/scripts/test-seed-naming.ps1` | Asserts a re-seed never takes a device name somebody chose |
| `custom/scripts/disable-inherited-ci.ps1` | Turn off upstream's workflows |
| `custom/FILE-BROWSER-OPTIONS.md` | Our own Explorer-like browser: seven options with pros and cons, and a recommendation. **Undecided; nothing built** |
| `custom/RELEASE-NOTES.md` | The body of the **next** release, rewritten each time. The release workflow reads it and appends the install section |
| `custom/CUSTOMIZATIONS.md` | Divergence register and merge guide |
| `custom/DEPLOYMENT-3D-TEAM.md` | Recommended config for the target users |
| `.github/workflows/desuq-test.yaml` | Runs every suite on push and PR. Reusable, so the release gates on it |
| `.github/workflows/desuq-release.yaml` | Our release pipeline. Its build job `needs:` the test job |

Everything else is upstream Syncthing, unmodified.

## Common commands

```powershell
.\custom\scripts\run-tests.ps1                 # all twelve suites; -Quick for the fast ten
.\custom\build-windows.ps1 -Installer          # build binary + installer (runs -Quick first)
.\custom\scripts\sync-upstream.ps1 -DryRun     # preview upstream changes
git tag v2.1.6-desuq.8; git push origin v2.1.6-desuq.8   # cut a release
# Tag shape is load-bearing: vX.Y.Z-desuq.N, where X.Y.Z is the upstream base
# and N keeps counting across bases. Never carry upstream's "-rc.N" into it:
# the tray's forkVersionRe (custom/tray/update.go) would not match, and the
# newer-peer toast would go quiet without an error.

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
- Stop the test pair before rebuilding if you pointed it at
  `custom\dist\desuq-syncthing.exe`. The build fails on `Move-Item -Force`
  with *"Cannot create a file when that file already exists"*, which is
  Windows' way of saying the destination is running, not that the flag was
  missed.
- `syncthing serve` exits **0** when an instance is already serving that home.
  Anything supervising it must check reachability first, or it respawn-loops.
- `lib/api/auto/gui.files.go` is gitignored and regenerated whenever `gui/`
  changes, so new GUI files cost no tracked diff.
- Fork-added GUI strings should use plain `{{ }}` interpolation.
  angular-translate renders `{%placeholders%}` literally for any string missing
  from `assets/lang`, which ours always are.
- **The per-theme asset overlay applies to every path, not just `assets/`.**
  `lib/api` serves `gui/<theme>/` over `gui/default/` and falls back per file,
  so a theme can own any single file. That is how the fork's `--v-*` palette
  works: `syncthing/desuq/tokens.css` exists once per theme and costs no
  upstream edit at all. **Its `<link>` must stay above `assets/css/theme.css`**
  in `index.html` — the violet theme defines the real palette there and relies
  on loading second. Move it and the violet UI turns light.
- **The test pair's binary and its GUI drift independently.** The pair defaults
  to the *installed* binary while `STGUIASSETS` serves `gui/` from the working
  tree, so it is easy to spend a session with a server older than the interface
  it is serving. The symptom is a fork REST endpoint 404ing while the GUI
  behaves as though it exists — `/rest/db/dirsizes` did exactly that. Build
  first and pass `-Binary custom\dist\desuq-syncthing.exe`.
- **Fork Awesome's family name is `ForkAwesome`, one word.** `"Fork Awesome"`
  with a space silently falls back and renders a tofu box, which is easy to
  miss in a `::before` on a disclosure triangle.

- **This build sends no telemetry, and that is a constant, not a setting.**
  `lib/build/desuq_telemetry.go`. Upstream has three reporters gated by two
  options, and the third -- the panic-log upload, `crashReportingEnabled` --
  is **on by default and never asks**. The config values are seeded off too,
  but only so `config.xml` does not claim otherwise. See `CUSTOMIZATIONS.md`.
- Auto-upgrade is compiled out (`-no-upgrade`). Syncthing verifies upgrades
  against upstream's signing key, which cannot validate our builds.
- Upstream's inherited GitHub workflows are disabled **via the GitHub API**, not
  by deleting files, so they cannot conflict on merge. That state lives in
  GitHub, not the repo.
- `.stignore` is never synced between devices — it is per-machine by design.
- **`/rest/db/browse?dirsonly=1` reports every directory as zero bytes.** It
  reaches a directories-only tree by *skipping every file*
  (`model.GlobalDirectoryTree`), and a directory's own `Size` is a filesystem
  stub. So there is no size information in that response at all, and anything
  downstream of it that talks about bytes is silently dead. The fork's
  `/rest/db/dirsizes` (`lib/api/api_dirsizes.go`) is the same tree with real
  totals, built server-side from the full tree and never serialised.
- **The tray refuses to start twice for one home.** A named mutex,
  `custom/tray/instance_windows.go`. Every shortcut now launches the tray, so
  a second one is a click away; without the guard you get two icons, two
  supervisors and two event subscriptions. A second launch with `-open` opens
  the GUI and exits.
- **`//go:uintptrescapes` is load-bearing in `notify_windows.go`.** Converting
  `unsafe.Pointer` to `uintptr` only keeps the referent alive when the
  conversion is syntactically inside a `syscall.Syscall` call. Passing one to
  an ordinary helper — `comCall` — leaves the local on the stack, where a
  stack growth relocates it before the syscall runs. Verified with
  `go build -gcflags=-m`.
- **`/rest/events/disk` is a *fixed-mask* endpoint and does not have the
  problem below.** One server-side buffer, `LocalChangeDetected |
  RemoteChangeDetected`, subscribed at daemon start
  (`lib/syncthing/syncthing.go:143`) and 1000 events deep, so `since=` works
  and there is no mask to mismatch. What it does have is a **restart**: the
  buffer is memory only, and IDs begin again at 1, so a stored `since` must be
  dropped the moment an ID goes backwards. **And polling will never show you
  that happen**: `Since()` in `lib/events` waits for its counter to pass the
  cursor before returning anything, so `since=4211` against a restarted daemon
  answers *empty*, every time, until 4211 more events exist. Ask for the
  newest with `since=0&limit=1&timeout=0` and compare. The History feed froze
  on exactly this until wave 15; `checkRestart` in `history.js` and
  `checkCollisions` in `custom/tray/claims.go` are the two readers. Two more things it will not tell
  you: **the first scan emits one event per existing file** (verified — six
  files, six events, one timestamp), which overruns 1000 on a real asset
  folder; and **`action` is only ever `modified` or `deleted`**
  (`lib/model/folder.go:1379`), so a file that has just been *created* reports
  as modified and nothing in the feed can say "added".
- **Marking a file ignored blanks its size in the local index.**
  `protocol.FileInfo.SetIgnored` → `setLocalFlags` → `setNoContent` sets
  `Size` to 0 and drops `Blocks`/`BlocksHash`. So an ignored file reports
  **zero bytes** locally, and anything comparing a local size to decide
  whether two copies agree will find every ignored file identical to every
  other. The *global* entry is authoritative and is not blanked — that is why
  `lib/api/api_reclaim.go` compares the global index against a real `Lstat`
  rather than reading local state. Fourth instance of the same class as
  `dirsonly=1` reporting every directory as zero bytes.
- **`/rest/db/completion`'s `remoteState` is `unknown`, not `notSharing`,
  for the commonest unaccepted share.** A peer's folder states are recorded
  only when their cluster config arrives -- at connect, and when *their*
  config changes. Share a new folder with somebody already connected and
  their last config predates it; a pending offer changes nothing on their
  side, so nothing is resent, and it stays `unknown` (the map's zero value)
  until they accept or reconnect. The main screen said "catching up -- 12 MiB
  to go" about exactly this. For a connected peer `unknown` means not
  accepted; for a disconnected one it means nothing, since the states are
  dropped on disconnect. `shareOf` in `home.js`.
- **A peer's completion reads 100% while they hold almost everything back.**
  An ignored file is not needed, so `/rest/db/completion` for a peer who took
  one file of six is complete. Their index says otherwise: ignored files
  arrive flagged `FlagLocalRemoteInvalid`, with `Size` blanked, so count them
  and take sizes from the global entry -- `/rest/db/peerheldback`
  (`lib/model/desuq_peerheldback.go`). Sixth instance of the local-state-lies
  class, and the first seen from the sending side.
- **Blender 5 compresses `.blend` files with zstd by default.** Every 5.x
  file on the development machine began `28 B5 2F FD`, so a thumbnail reader
  that skips compressed files skips nearly everything. The standard library's
  zstd decoder is `internal/zstd`; `lib/api/desuq_blendthumb.go` reaches it
  through `debug/elf` rather than add a root `go.mod` dependency. The 5.x
  header is also a different shape (`BLENDER17-01v0500`, 32-byte block
  headers with the length moved). A file saved from `--background` has no
  preview at all.
- **A peer's cluster config says who it shares each folder with, and upstream
  throws that away** once introductions and auto-accept have read it. Keeping
  it (`lib/model/desuq_hub.go`, one line in `ClusterConfig`) is the only way
  to see that two peers never sync with each other except through you.
- **Remote need excludes files the peer ignores.** Verified on a pair: a file
  B holds back never appears in A's `/rest/db/delivery` for B. So "does not
  have your latest" is not fooled by selective sync, unlike completion.
- **`.desuq-claims/` is a directory of the fork's own inside synced
  folders.** "I'm working on this", one file per device
  (`lib/api/api_claims.go`). Anything new that lists, counts, versions or
  ignores a folder's contents has to decide what to do about it -- see the
  table in `DEPLOYMENT-3D-TEAM.md` §25 for what every existing view does.
- **The page loads no Fancytree skin.** Upstream uses Fancytree only in table
  mode, so list mode -- the selective-sync picker -- rendered every row with a
  browser-default bullet and 40px indents until `selective.css` styled the
  `<ul>`/`<li>` itself. jsdom cannot see this; it was found in Chrome.
- **`/rest/folder/versions` has no paging and no filter.** It answers with
  `map[filename][]FileVersion` — every version of every file, in one
  document. Staggered versioning at thirty days keeps roughly fifty copies per
  file, so a five-thousand-file asset folder is a quarter of a million entries
  and tens of megabytes of JSON. The fork's `/rest/folder/history`
  (`lib/api/api_history.go`) is the same data summarised server-side, with the
  per-file list fetched only on expand. Restoring still uses upstream's
  `POST`, which was always the right shape.
- **`/rest/events` IDs are per subscription, not global.** Syncthing keeps one
  buffer per distinct `events=` mask and numbers each from 1. Bootstrapping
  "where is now" with one mask and then polling with another silently drops
  events. Ask with the same mask you intend to poll with.
- **`explorer.exe` exits 1 on success.** `lib/api/api_reveal_windows.go` starts
  it and never waits: it is a launcher that hands the request to the running
  desktop shell and leaves, and there is nothing useful in its status either
  way. Anything that checks the exit code reports every successful open as a
  failure. It is also why the route only ever passes a **directory** —
  `explorer.exe` given an executable runs it.
- **Anything served to an `<img>` cannot live under `/rest/`.** Every path
  there is behind the CSRF middleware, which admits a request only with a CSRF
  token header or an API key header, and an `<img src>` sends neither -- it is
  answered 403. The thumbnails in `lib/api/api_preview.go` are therefore
  mounted on the *outer* mux at `/preview/`, beside upstream's `/qr/`, which
  exists for exactly the same reason. Still behind the authentication
  middleware: a session cookie *is* sent by an `<img>`. This fails **only in a
  browser** -- the jsdom render tests stub `$http` and never reach the
  middleware, so all twelve suites passed while every thumbnail 403'd.
- **`/rest/stats/device` reports a never-connected device's `lastSeen` as the
  Unix epoch, not as a zero time.** `IsZero()` is false for it, so a brand new
  peer reads as fifty-six years of silence. `seenEver` in `custom/tray/stale.go`
  is the check; `stale_live_test.go` is what found it, and a fixture written by
  hand would have had the zero time in it.
- **The short device ID in a conflict copy's name is not the author of that
  copy.** `moveForConflict(name, file.ModifiedBy.String())` renames the *local*
  file aside and tags it with the *incoming* version's device -- the one that
  won. So the bytes are the losing edit and the name belongs to the winner.
  Verified on a pair: B won, and both sides ended up with
  `texture1.sync-conflict-...-V7OXBJ3.png` (B's ID) holding A's work. Take
  "who wrote this" from the index's `ModifiedBy` on each side, never from the
  name. Fifth instance of the local-state-lies class.
- **`custom/build-windows.ps1` needs PowerShell 7, not Windows PowerShell
  5.1.** Under 5.1 the generated `versioninfo.json` gets a UTF-8 BOM and the
  build dies at `goversioninfo` with *"could not parse the .json file: invalid
  character 'ï'"*, which names neither the file nor the cause.
- **Syncthing holds the folder root open**, so a test that wants to simulate a
  vanished folder has to pause the folder first. Deleting it anyway removes the
  *contents* and fails on the root -- which is, usefully, the exact shape of a
  drive that came back empty.
- **Syncthing's GUI certificate has no IP SAN.** `https-cert.pem` carries the
  device name as its common name and its only DNS SAN (`CN=desuq, DNS:desuq`),
  so a connection to `https://127.0.0.1:8384` can never pass hostname
  verification however the trust store is arranged. That is why
  `custom/tray/tlspin.go` pins: the certificate goes in a one-entry root pool
  *and* `ServerName` is read back off the certificate. Verified against a
  bare non-CA leaf, which is what Syncthing writes. GUI TLS is off by default
  and is not seeded on, so this path is rarely exercised -- which is how the
  tray carried `InsecureSkipVerify: true` for as long as it did.
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
  failures. See `DEPLOYMENT-3D-TEAM.md` section 10. **The tray initialises COM
  nowhere**, so any shell API added to it has to do its own
  `CoInitializeEx` under a `runtime.LockOSThread` -- the initialisation is
  per *thread*, and without the lock the runtime may move the goroutine
  between the two calls. `openURL` in `platform_windows.go` is the worked
  example.
- **Defender quarantines the tray as `Trojan:Win32/Bearfoos.A!ml`** on install
  -- an ML false positive, taking both shortcuts and start-at-sign-in with it,
  so the machine silently stops syncing at the next reboot. `DEPLOYMENT-3D-TEAM.md`
  section 20 has the audit of why it fires and what to do. Consequence for
  development: **a clean local build proves nothing about the installed copy**
  -- the same binary in `custom\dist\` was untouched while the one the
  installer wrote was eaten. Do not add LOLBin-shaped code to the tray;
  `rundll32 url.dll,FileProtocolHandler` was removed for exactly this reason --
  but **removing it did not clear the detection** (desuq.5 was quarantined
  identically to desuq.4), so do not treat source-level tidying as a fix.
- **Edit against delete is decided by which happened later.** Change later
  and the file silently comes back on the machine that deleted it -- no
  copy, no conflict, no event; delete later and the change survives only as
  a `sync-conflict` copy with no original beside it. Both reproduced on a
  pair by swapping the order. The tray remembers local deletions on disk
  (`custom/tray/resurrect.go`) because nothing in the index does.
  `DEPLOYMENT-3D-TEAM.md` §26.
- **A conflict copy keeps the mtime of the edit it preserved**, not the time
  of the conflict. Anything asking "is this conflict new" must read the stamp
  in the name (`sync-conflict-20260926-150211-`); by mtime, an offline edit
  followed by a tray restart was never announced.
- **The read-only attribute is synced.** Setting it on one machine makes a
  new version and every peer receives it -- including the person you meant
  to protect. That is why there is no "soft lock"; see §26 before retrying.
- **Switching a receive-only folder to Send & Receive publishes every local
  change it was holding, deletions included.** Verified on a pair. The main
  screen now says so where it reports local-only changes.
- **`DeviceConnected` fires once per connection, and Syncthing 2 opens
  several per peer.** `DeviceDisconnected` fires only when the last closes.
  Treat a second `DeviceConnected` without a disconnect between as nothing
  (`custom/tray/reconnect.go`).
- **`/rest/db/file`'s `availability` lists connected devices only.** An
  offline peer who has the file is indistinguishable from one who does not.
  `/rest/db/whohas` reads each peer's index instead.
- **The folder summary field is `needTotalItems`, not `needItems`.**
  `/rest/db/completion` says `needItems`; `/rest/db/status` and the
  `FolderSummary` event do not. The tray decoded the wrong one for as long as
  it existed, so a deletions-only pull was never "behind".
- **`<select>` with `<option ng-repeat>` does not re-select when the options
  arrive after the model is set.** The History folder picker sat on "All
  folders" while showing one folder. Use `ng-options`.
- The device-verification wordlists in `gui/default/syncthing/desuq/` are data,
  not prose: a word's *index* is its meaning. Re-ordering a list or inserting
  into the middle of one silently invalidates every verification anyone has
  already done. Append only, and re-run
  `custom/scripts/check-handshake-words.ps1` (the build does too).
