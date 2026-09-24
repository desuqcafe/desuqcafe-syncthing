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
| `lib/api/api.go` (2) | **One guard**, 6 lines with the comment, in `postDBIgnores`: an ignore line containing CR or LF is rejected with 400. One element of that array is one line of `.stignore`, so an element carrying a newline silently becomes two rules — and the fork's picker builds those lines out of file names chosen by whoever offered the folder. A peer can otherwise write `!Textures` into a neighbour's ignore file to undo an exclusion, or `#include nope` to stop the folder syncing at all. | **Low.** Additive, immediately before the existing `SetIgnores` call, in a short handler upstream rarely touches. A conflict resolves by re-inserting the loop ahead of whatever the call becomes. Deliberately server-side rather than only in the picker, so it also covers upstream's own ignore textarea. |
| `lib/api/api.go` (3) | **One line**, registering `GET /rest/db/dirsizes`. The handler is in a new file, `lib/api/api_dirsizes.go`. It answers the question `db/browse?dirsonly=1` cannot: that endpoint reaches a directories-only tree by skipping every file, so every directory in it reports zero bytes, and the selective-sync picker's disk guard was inert on exactly the folders large enough to fill a disk. | **Low.** One entry in the same append-only route table as the `diskfree` line above it; "keep both sides". |
| `lib/api/api.go` (4) | **One line**, registering `POST /rest/system/reveal`. The handler is in a new file, `lib/api/api_reveal.go`. It opens a synced folder in Explorer, which is the one thing the fork's GUI wanted that a browser genuinely cannot do: a `file://` link from an `http://` page is blocked outright, so until now the folder path on the main screen was something to select and paste by hand. Takes a *folder ID* and an optional relative sub-path, never a path — the set of directories it will open is bounded by the config, and what it hands to `explorer.exe` is always a directory, because Explorer given an executable runs it. | **Low.** One entry in the same append-only route table as the three lines above it; "keep both sides". |
| `lib/api/api.go` (5) | **Two lines**, registering `GET /rest/db/reclaimable` and `POST /rest/db/reclaim`. The handlers are in a new file, `lib/api/api_reclaim.go`. Un-ticking in the selective-sync picker stops a folder being kept up to date; it never freed the disk, and the interface had merged the two into one promise. These are the second half: a dry run, and a delete that re-evaluates four rails server-side per file at the moment of deletion. | **Low.** Two entries in the same append-only route table as the four lines above; "keep both sides". |
| `lib/api/api.go` (6) | **One line**, registering `GET /rest/folder/history`. The handler is in a new file, `lib/api/api_history.go`. Upstream's `/rest/folder/versions` answers with every version of every file in one document — a quarter of a million entries for a five-thousand-file asset library at thirty days — which is untenable for a screen whose first page is twenty rows. This is the same data summarised server-side, with the per-file lists fetched on expand. Restoring still goes through upstream's `POST /rest/folder/versions`. | **Low.** Same table, same resolution. |
| `lib/api/api.go` (7) | **Four lines.** Three in the same append-only route table — `GET /rest/folder/conflicts`, `POST /rest/folder/conflict`, `POST /rest/folder/repair` — and one on the *outer* mux beside upstream's `/qr/`, registering `/preview/`. The handlers are in new files (`api_conflicts.go`, `api_repair.go`, `api_preview.go`). The odd one out is `/preview/`, and it has to be: thumbnails are `<img>` sources, an `<img>` cannot send a header, and everything under `/rest/` is behind the CSRF middleware, which admits only a CSRF or API-key header. Upstream's QR image is outside `/rest/` for exactly that reason and this sits beside it — still behind the authentication middleware, since a session cookie *is* sent by an `<img>`. | **Low.** Three entries in the same table as the six lines above; "keep both sides". The `/preview/` line sits next to `mux.HandleFunc("/qr/", ...)`, which upstream has not moved in years. |
| `lib/api/api.go` (8) | **One line**, registering `GET /rest/system/tray`. The handler is in a new file, `lib/api/api_tray.go`. It says whether the tray executable is still beside this binary, on an installed copy only. Defender quarantines the tray as a false positive (`DEPLOYMENT-3D-TEAM.md` §20) and the tray cannot report its own absence; this binary has never been flagged and keeps running when the tray is taken, so it is what is left to say so, and the main screen turns the answer into its loudest headline. | **Low.** Same append-only table as the lines above; "keep both sides". |
| `gui/default/index.html` | Twelve additive hunks, ~62 lines, plus one removal (the two `<ng-include>`s for upstream's usage-report modals, replaced by a comment saying why): four `<link>` and six `<script>` tags for the fork's GUI files, the `<desuq-selective-modal>` and `<desuq-first-run-wizard>` elements, a "Setup guide" entry in the Actions menu, a "Disk Space" row in the folder detail table, a "Verification" row in the device detail table, and a "Choose Files" button in the folder panel's footer (only on a folder shared with somebody — there is no remote index to pick from otherwise, and un-ticking a file that exists only here would tell Syncthing to stop looking after your own work). | **Low–medium.** The file is large and upstream does edit it, but every hunk is additive and they are far apart. |
| `gui/default/syncthing/folder/editFolderModalView.html` | Seven lines: three showing free space under the Folder Path field, four placing `<desuq-selective-option>` at the top of the Ignores tab. | **Low.** |
| `gui/default/syncthing/device/editDeviceModalView.html` | Ten lines: five placing the device verification card under the Device ID field, five placing `<desuq-lan-limit>` under the per-device rate limits. | **Low.** |
| `gui/default/syncthing/settings/settingsModalView.html` | Seven lines placing `<desuq-lan-limit>` between the rate fields and the "Limit Bandwidth in LAN" checkbox. | **Low.** |
| `gui/default/syncthing/core/syncthingController.js` | **One branch**, 13 lines with the comment, at the top of `saveFolder`'s new-folder handling. It hands the save to the selective-sync picker, and is guarded on both a flag only the fork's directive sets and a service only the fork publishes — so with `syncthing/desuq/` removed it is unreachable and the function is upstream's. | **Low–medium.** The only fork edit inside an upstream *function* rather than beside one. It sits between two comment-led blocks that have been stable for years, and it is a self-contained early return, so a conflict resolves by re-inserting it wherever the equivalent point ends up. |
| `gui/default/syncthing/core/syncthingController.js` (2) | **Three removals**, each replaced by a comment: the two blocks that raise the usage-reporting nag, and the two lines in `saveSettings` where choosing the release-candidate upgrade channel silently sets `urAccepted`. | **Medium.** Removals inside upstream functions. If upstream edits them the merge will conflict; the resolution is to delete their side again. |
| `gui/default/syncthing/core/syncthingController.js` (3) | **Three edits to the `localadditions` folder state.** It no longer returns `'success'` from `folderClass` (it returns `'warning'`), and its icon is no longer `fa-check`. A receive-only folder holding local additions offers one click of "Revert Local Changes", which deletes them; painting that panel green with a tick is what makes the button beneath it read as a tidy-up. Receive-only is what `DEPLOYMENT-3D-TEAM.md` recommends for both modellers, so this is the panel they see most. | **Low–medium.** Three one-line changes inside two long `switch`/`if` chains upstream does edit. A conflict resolves by re-applying the same three moves to whatever the chains have become. |
| `gui/default/syncthing/core/syncthingController.js` (4) | **One additive block**, ~18 lines with the comment, at the end of `revertOverrideConfirmationModal`: it puts the folder's label, its locally-changed item count and bytes, and whether the folder has a versioner, onto `revertOverrideParams` for the dialogue to name. | **Low.** Appended to the end of a short function, after upstream's `switch` and before the two lines that show the modal. |
| `gui/default/syncthing/folder/revertOverrideView.html` | **One additive `<div>`**, ~25 lines, between upstream's two paragraphs in the revert branch. Names the folder, says how many files and how big, and says whether they go to version history first or are gone for good. Upstream's own wording is untouched, and the block uses Bootstrap's `well`/`text-danger` rather than a fork stylesheet so the dark themes style it. | **Low.** Additive, inside one `ng-switch` branch. |
| `lib/versioner/` (new file only) | **No upstream file is edited.** `desuq_open.go` is a new file *inside* the package, which is how it can use the unexported `versionerFsFromFolderCfg` rather than reimplementing the archive path layout (a configured versions directory, a tilde in it, a relative one, a non-basic filesystem type). Getting a copy of that wrong fails silently — it looks like "no versions here" rather than an error. | **None.** A new file cannot conflict; the risk is that upstream renames the helper it calls, which is a compile error, not a silent one. |
| `lib/config/optionsconfiguration.go` | **Two struct tags.** `URAccepted` gains `default:"-1"` (upstream has none, i.e. 0, "not yet asked"); `CREnabled` loses `default:"true"`. | **Low.** Two lines in a long field list. A conflict resolves by re-applying the two tags to whatever upstream's line has become. |
| `lib/ur/usage_report.go` | **One guard**, 8 lines with the comment, at the top of `Serve`. Returns an inert service when `build.TelemetryEnabled` is false. | **Low.** Additive, first statement of the function. |
| `lib/ur/failurereporting.go` | **One guard**, 7 lines, at the top of `Serve`. Same shape; also means the handler never subscribes to `events.Failure`. | **Low.** Additive, first statement of the function. |
| `cmd/syncthing/monitor.go` | **One guard**, 7 lines, at the top of `maybeReportPanics`. This is the reporter upstream leaves *on*. | **Low.** Additive, first statement of the function. |
| `lib/syncthing/syncthing.go` | **One condition**, `if build.IsCandidate` becomes `if build.IsCandidate && build.TelemetryEnabled`, plus four comment lines. | **Low.** One token on one line. If it conflicts, re-add the conjunct. |
| `gui/default/syncthing/settings/settingsModalView.html` (2) | Upstream's "Anonymous Usage Reporting" `<select>` replaced by a static note saying the build sends none. | **Low–medium.** This one *replaces* rather than inserts. A conflict resolves by deleting upstream's control again. |
| `gui/default/index.html` (2) | **The main screen.** Four additive hunks and four label edits. The hunks: a `<link>` for `syncthing/desuq/tokens.css` — which **must stay above `assets/css/theme.css`**, because the violet theme defines the real palette there and relies on winning — a `<link>` for `home.css`, a `<script>` for `home.js`, and `<desuq-home>` immediately above upstream's first row, which is now wrapped in a `<details class="desuq-technical">`. Upstream's markup between those two wrapper lines is **untouched**: it carries thirty-one actions, several destructive (`revertOverrideConfirmationModal`, `restoreVersions`) and several diagnostic (`showFailed`, `showNeed`, `showLocalChanged`), and the new screen should earn their deletion by covering the cases first rather than removing them in the same change that introduces their replacement. **Costed since** (`DEPLOYMENT-3D-TEAM.md` §18): 7 covered, 4 one click deeper, 2 dead, **18 with no entry point at all** — and three of those eighteen are capabilities rather than shortcuts. Deleting the region moves this row to **High** and is a pass of its own; the shortlist that earns it is `restoreVersions.show`, `revertOverrideConfirmationModal('revert', …)` and `showFailed`. Each costs one button, because every modal is `<ng-include>`d *outside* the region. The label edits: `Show ID` → `Show device code`, `Identification` → `Device code` (twice), and the fork's own 194px `Verification` row reduced to a one-line link. Help menu: upstream's `Statistics` link to `data.syncthing.net` removed (a page built entirely from usage reports this build does not send, so it can only ever show somebody else's installations), and `Changelog`/`Bugs`/`Source Code` repointed at this fork, whose releases are the ones this binary is cut from. | **Low.** Everything structural is additive and the four label edits are one line each. The `<details>` wrapper is two lines at the top of upstream's row and one at the bottom; a conflict resolves by re-wrapping whatever the row has become. The one thing to preserve on merge is the `tokens.css` link's **position**, not just its presence. |
| `gui/default/syncthing/core/notifications.html` | **One removal**, replaced by a comment: upstream's `authenticationUserAndPassword` notification. It was the largest element on the screen of every fresh install and it was painted `panel-success` — bright green, the colour this fork reserves for "your files are safe" — for something that is neither a success nor a sync fact. The information now sits in Settings → GUI directly above the two fields that resolve it. Upstream's far stronger red "Danger!" panel, shown when the GUI is reachable from off this machine, is deliberately **untouched**: that is the case that actually warrants shouting. | **Low–medium.** A removal inside a file of independent `<notification>` blocks, so it conflicts only if upstream edits this one. The resolution is to delete their side again. |
| `gui/default/syncthing/core/shutdownDialogView.html` | **Rewritten** (8 lines). Upstream's was `status="success" closeable="no"` with the single line "Syncthing has been shut down." Green for having just stopped syncing your files; `closeable="no"` meant no backdrop, no Escape, no button and no link, with the server gone — a dead end you could only leave through the browser's chrome; and it never said how to start Syncthing again, which on this fork is the tray icon. Now `status="default" closeable="yes"`, and it says the files are where you left them, that closing the window is safe, and where to start it again. No action button, deliberately: every action it could offer needs the server that just stopped. | **Low–medium.** A whole-file rewrite, but of a file upstream touches very rarely, and "keep ours" is always the answer. |
| `gui/default/syncthing/settings/settingsModalView.html` (3) | **One removal and one addition.** Upstream's "Automatic upgrades" control is gone: this build is compiled `-no-upgrade`, so `upgradeInfo` is never populated and the only branch that could ever render read "Unavailable/Disabled by administrator or maintainer" — which is not even accurate, since nobody disabled it and it cannot work. Replaced by a plain statement that the build does not update itself, in the same shape as the telemetry note beside it. The addition is the GUI-authentication note evicted from the main screen, gated on `ng-if="!tmpGUI.user"`. | **Medium.** Same class as the telemetry row above; this file now carries two fork removals. |
| `gui/default/syncthing/device/editDeviceModalView.html` (2) | **Two label edits and one move.** `Device ID` → `Device code`, and the help sentence beneath the field rewritten to point at "Actions > Show device code". The move is the fix for a real bug: `<device-handshake>` had been placed *inside* upstream's `<div ng-if="editingDeviceNew()">`, so the verification card only ever rendered while **adding** somebody and never when opening an existing person's settings — the one thing it exists for, re-reading the phrase to each other on a call some time later, could not be done there at all. It is now a sibling of that block, directly under the Device code field, for both cases. | **Low.** The move is within a fork-added element; the anchor is upstream's closing `</div>` for the new-device block. |
| `gui/default/syncthing/device/idqrModalView.html` | **One attribute.** The modal heading `Device Identification - {{name}}` becomes `Device code — {{name}}`. Part of the same rename: the concept had four names across the interface, and the two people this fork is for have to say it out loud to each other over a phone. | **Low.** |
| `README.md` | **Rewritten, not appended to.** Upstream's landing page replaced by the fork's: what this is, an install a non-technical reader can follow, a table of what the fork adds linking into `DEPLOYMENT-3D-TEAM.md`, what it deliberately does not do, and where to take a bug. Upstream's goals, documentation, forum and security address are linked rather than inlined. | **High, and knowingly so** — see below. Every upstream README edit conflicts. The resolution is always "keep ours": nothing reads this file, so a stale merge costs one discarded diff. Read their side only for a link worth carrying over. |

## Why the README is a rewrite

This is the fork's highest-conflict file by some distance, and it is the one
place where taking the conflict is obviously right.

The choice was between rewriting and a fork header sitting above upstream's
content. The header conflicts far less, but it reads like a patch set on
somebody else's product, and it leaves the two people this fork exists for —
non-technical modellers who need "download, run, no administrator" — scrolling
past Syncthing's goal list and `go run build.go` to find out whether they are
in the right place. Both audiences the file has, the ten-second visitor and the
modeller, are served worse by every line of upstream's that stays.

Against that, the merge cost is close to nothing in kind even though it is
certain in frequency: `README.md` has no behaviour, nothing in the tree reads
it, and no test depends on it. A conflict here can only ever cost a diff to
look at and discard, which is the cheapest conflict there is.

What upstream's README carried that is worth keeping is carried as links, not
as text: the documentation site, the forum, `GOALS.md`, and **their** security
address, which the new file is explicit about being upstream's rather than
ours. The **MPLv2 notice and the attribution to the Syncthing Authors stay**,
because that is a licence obligation rather than a courtesy.

**`README-Docker.md` is deliberately left alone**, which was the other half of
the decision. Deleting it would be the tidy-looking move — a Windows-only
per-user installer has no use for Docker — but it buys nothing and costs
something. It would introduce a modify/delete conflict class the fork does not
have today, it would delete a document that is still perfectly accurate about
upstream's published image, and it would be incoherent while all seven
`Dockerfile*`s remain in the tree. So it stays, unlinked from anywhere except
one line in the new README saying whose image it documents.

Everything else the fork adds lives in files upstream does not have, so it
cannot conflict at all:

| New file | What |
| --- | --- |
| `lib/api/api_diskfree.go` | The `/rest/system/diskfree` handler |
| `lib/api/api_dirsizes.go` | The `/rest/db/dirsizes` handler: the directory tree with real byte and file totals |
| `lib/api/api_reveal.go` | The `/rest/system/reveal` handler: resolves a folder ID to a directory and proves it is inside that folder |
| `lib/api/api_reveal_windows.go` | Hands that directory to `explorer.exe`. Started and never waited for — explorer exits **1 on success** |
| `lib/api/api_reveal_other.go` | Answers 501 off Windows, deliberately: `xdg-open` is a launcher for whatever the desktop has registered |
| `lib/api/api_reveal_test.go` | The containment check. It is the only thing standing between "open this" and "run that" |
| `lib/api/api_reclaim.go` | The reclaim handlers, and `reclaimVerdict` — the four rails, pure, so the one thing in this fork that deletes a person's files can be proved without a model or a disk |
| `lib/api/api_reclaim_test.go` | Tries to make `reclaimVerdict` say yes when it should say no. Every ambiguous case expects "keep" |
| `lib/api/api_history.go` | The `/rest/folder/history` handler: the archive summarised, with the big part never serialised |
| `lib/api/api_history_test.go` | The totals. The browser never sees the full archive, so it cannot check the arithmetic |
| `lib/api/api_conflicts.go` | The conflict list and the two resolutions. Both are a rename plus an archive, never a delete — so either choice is undone from the History screen |
| `lib/api/api_conflicts_test.go` | `conflictParse`, pure. It decides which file gets overwritten, so the awkward names are spelled out rather than generated |
| `lib/api/api_repair.go` | `POST /rest/folder/repair`, and `repairVerdict` — the rail that refuses to recreate an empty directory for a folder whose index still holds files |
| `lib/api/api_repair_test.go` | The four cases. Two allowed, two refused; the difference is whether a drive being unplugged becomes a deletion everybody receives |
| `lib/api/api_preview.go` | Thumbnails, decoded and box-sampled server-side. PNG/JPEG/GIF only, which also keeps it from being a "read any file" route |
| `lib/api/api_tray.go` | `GET /rest/system/tray`: is the tray still on disk. Only an installed copy, recognised by the `seed-config.ps1` the installer ships beside it, ever answers `expected`, so `custom\dist\` and a bare build are never alarmed about |
| `lib/api/api_tray_test.go` | Installed with and without the tray, a dev build, a bare build, off Windows, and a rebranded binary name |
| `lib/api/api_preview_test.go` | Aspect ratio, transparency, format, and that it refuses a file that only claims to be an image |
| `lib/versioner/desuq_open.go` | Opens one archived copy for reading, so a version can be *shown* without being restored |
| `custom/tray/stale.go` | "Kai has not synced for four days", and the three sentences it chooses between depending on what the evidence supports |
| `custom/tray/stale_test.go` | Those rules, including the Unix-epoch case a live instance actually returns |
| `custom/tray/stale_live_test.go` | The two REST shapes behind it, against a real Syncthing. It is what found the epoch |
| `custom/tray/pausefor.go` | A pause with an end, and the hold that lifts it |
| `custom/tray/pausefor_test.go` | That cancel really cancels, and that re-arming replaces rather than stacks |
| `gui/default/syncthing/desuq/` | The fork's Angular directives, its wordlists and its CSS |
| `custom/tray/update.go` | The peer-version comparison behind the "an update is available" toast |
| `custom/scripts/test-wizard-render.js` | Drives the first-run wizard through real Angular |
| `custom/scripts/test-history-render.js` | Drives the history screen the same way. The restart guard and the run collapse are what it tries to break |
| `custom/scripts/test-home-render.js` | Drives the main screen through real Angular, and asserts the headline rules directly |
| `gui/default/syncthing/desuq/tokens.css` | The `--v-*` palette, light. Every `syncthing/desuq/` stylesheet paints with these and nothing else |
| `gui/dark/syncthing/desuq/tokens.css` | The same palette for Syncthing's dark theme |
| `gui/black/syncthing/desuq/tokens.css` | The same palette for Syncthing's black theme |
| `gui/violet/` | The violet theme |
| `custom/` | Everything else |
| `lib/build/desuq_telemetry.go` | The `TelemetryEnabled` constant all three reporters consult |
| `lib/ur/desuq_telemetry_test.go` | Asserts nothing is posted, by watching the wire |
| `cmd/syncthing/desuq_telemetry_test.go` | Asserts no panic log is uploaded |
| `.github/workflows/desuq-test.yaml` | Runs every suite; reusable, so the release gates on it |

Three of those are the same path under three theme directories, which is worth
explaining because it turns what looked like an unavoidable upstream edit into
no edit at all. `lib/api`'s static server overlays `gui/<theme>/` on top of
`gui/default/` and falls back **per file**, and it does that for every path,
not just `assets/`. So a theme gets its own copy of the fork's palette purely
by owning a copy of `syncthing/desuq/tokens.css`; `light` has none, so it gets
the default light one, which is what it wants.

Before this, only `gui/violet` defined the `--v-*` custom properties — `dark`,
`black`, `light` and `default` defined none — so under any theme but violet all
four of the fork's stylesheets fell through to the hardcoded light values in
their own `var()` calls. Nobody had seen it, because every test this fork has
is jsdom and jsdom does not paint. The visible result was a first-run wizard
whose heading and close button were invisible on a dark page.

Verified empirically against a running instance rather than assumed: a probe
file placed at `gui/dark/syncthing/desuq/` was served under the dark theme and
fell back to the `gui/default` copy under violet and light.

Where the fork stands today: **609 inserted lines against 207 deleted**, across
seventeen files. The README is 152 of those insertions and 105 of the
deletions; take it out and the rest of the fork is 457 against 102.

The main-screen wave added the three newest files — `notifications.html`,
`shutdownDialogView.html` and `idqrModalView.html` — and is the largest single
change to `index.html` so far at 149 insertions. Only **nine** of that file's
deletions are fork work in total, which is the point: the new screen is a
`<desuq-home>` element and a `<details>` wrapper placed around upstream's
markup rather than in place of it, so almost none of what it replaces on screen
is actually gone from the file.

The audit pass added 28 of those insertions and 4 of the deletions, and no
fourteenth file: the ignore-line guard went into `lib/api/api.go`, which
already carried the `diskfree` route; the folder panel's "Choose Files" button
lost its paused guard and the two `<disk-free>` call sites gained a
`min-disk-free` binding, all in files already listed above.

Up to the telemetry work almost every edit was inserted *beside* upstream's
code rather than in place of it, which is why merges had been boring. Stripping
the telemetry is the first change that had to delete things — upstream's
consent nag, its usage-reporting dropdown, and the two lines where picking the
release-candidate upgrade channel quietly opts you in. There was no additive
way to remove a control, and a switch left on screen wired to nothing would
have been worse than a merge conflict.

So before a merge there are two things to read: the four rows marked
**Medium**, which are removals inside upstream functions and resolve by
deleting their side again, and the README, which is **High** and resolves by
keeping ours every time. Everything else still resolves by keeping both sides.

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
| First-run setup guide | `gui/default/syncthing/desuq/firstRunWizard.js` and its template and stylesheet, plus four additive lines in `index.html`. Four steps -- name, code, verify, sync -- over a fresh install that otherwise has no next action on it at all. It reads its own state over **REST rather than off `syncthingController`'s scope**, which is why the merge cost is those four lines and why `test-wizard-render.js` can drive it without standing up the controller. The one thing passed in is upstream's own pending-folder accept, so step 4 hands an offer to `addFolderAndShare` rather than re-implementing its defaults. See `DEPLOYMENT-3D-TEAM.md` section 16. |
| Knowing an update exists, without contacting anybody | `custom/tray/update.go`. Syncthing already tells every device it connects to what version it is running, and serves it back at `/rest/system/connections` -- upstream's own GUI shows it. The tray compares itself to its peers and toasts when one is ahead. **No polling, no releases API, no signing key**, and the one external URL in the tray is only ever handed to a browser by somebody clicking the toast. See `DEPLOYMENT-3D-TEAM.md` section 17. |
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

## Running the tests

```powershell
.\custom\scripts\run-tests.ps1            # all eight suites
.\custom\scripts\run-tests.ps1 -Quick     # the six that need no binary and no pair
```

Nine suites in three languages, four of them needing jsdom and one needing two
live Syncthing instances. Until `run-tests.ps1` existed the only way to run
them all was to remember nine command lines, so nothing did.

A missing prerequisite -- no Go, no jsdom, no built binary -- is reported as
SKIP rather than as failure, and the exit code stays 0. But the summary says
loudly what did not run, because "all green" over four skips is how a suite
quietly stops covering anything. `-RequireAll` turns a skip into a failure, and
is what CI passes: there, everything is installed on purpose, so a skip means
the detection broke.

`build-windows.ps1` runs the quick set before compiling; `-SkipTests` opts out
for a tight edit loop, and still runs the wordlist check, which is a property
of the data being compiled in rather than a test of it.

## CI runs them, and a release cannot skip them

`.github/workflows/desuq-test.yaml` runs every suite on push and pull request.
It is also a **reusable** workflow, so `desuq-release.yaml` calls it as a job
its build `needs:` -- a tagged release cannot be cut from a tree whose tests
are red, and the suites are defined once rather than twice.

Windows-only, and not by oversight: half the suites are PowerShell, the tray
and the Explorer folder icons are Win32, and the artifact is a Windows
installer. A Linux runner would run about a third of it and give a green tick
that meant less than nothing.

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
