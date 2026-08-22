# Deploying to a 3D team

Notes for the target deployment: one developer sharing `.blend` files and
textures with two non-technical 3D modellers.

Everything below was verified against the code in this tree and against a
running instance, not taken from documentation.

---

## 1. Disk space: the check is weaker than it looks

Syncthing has a `minDiskFree` setting per folder, default **1 %**. It is enforced
in `lib/model/folder_sendrecv.go` at two points, both **per file**:

```go
// Verify we have space to handle the file before we start creating temp files
if err := f.CheckAvailableSpace(uint64(fi.Size)); err != nil {
```

What that means in practice:

- Syncthing checks *"is there room for this one file, plus the reserve?"*
- It **never** checks *"can this disk hold the folder I just agreed to sync?"*

So a modeller can accept a 400 GB share onto a 250 GB drive. Syncthing will
happily fill the disk down to the reserve, failing file by file at the end, with
the folder sitting in a permanent "Out of Sync" state. Nothing warns them up
front.

The GUI has an input for `minDiskFree` but **never displays actual free space**,
and there is no REST endpoint reporting disk usage. There is no pre-flight
capacity check anywhere in the codebase.

**What the fork does about it**

The reserve is now seeded at **20 GB absolute** rather than 1 % (see §4 and the
table at the end). 1 % of a 500 GB drive is 5 GB, which is nothing when a single
asset drop can be tens of GB, and an absolute figure behaves predictably across
differently-sized drives.

The GUI now also *shows* the space, which it never did before:

- `GET /rest/system/diskfree?path=…` reports free and total bytes for a path
  (`lib/api/api_diskfree.go`). For a folder path that does not exist yet it
  measures the nearest existing ancestor, which is the same filesystem the
  folder will end up on.
- The folder editor shows "481 GiB free of 1.82 TiB" under the Folder Path
  field, updating as the path is typed.
- The folder panel has a **Disk Space** row, and when what is still to be
  pulled will not fit it turns red: *"Not enough space: needs 357 GiB more than
  the 481 GiB free here"*.

**A true accept-time check turns out to be impossible**, and it is worth
recording why, because it is not obvious. The suggestion used to be "compare
the offering device's advertised folder size against `fs.Usage()`". But the
offering device never advertises a size. The pending-folder record is
`ObservedFolder` in `internal/db/observed.go`:

```go
type ObservedFolder struct {
	Time             time.Time
	Label            string
	ReceiveEncrypted bool
	RemoteEncrypted  bool
}
```

Label and two flags. No size, no file count. And the size cannot be learned by
waiting either: the GUI adds a newly accepted folder **paused**, and
`generateClusterConfig` announces a paused folder with
`StopReason: FolderStopReasonPaused`, so the remote sends no index and
`globalBytes` stays 0 until the folder actually starts.

So the earliest moment the size is knowable is *just after* the folder starts
and its index arrives — before any file data has been pulled. That is where the
warning fires. It is not "before you click accept", but it is still long before
the disk fills, and it names the exact shortfall instead of leaving the folder
to wedge at "Out of Sync" with a full drive.

## 2. Selective sync: it exists, but it is ignore patterns

There is no per-file "tick what you want" UI like Dropbox. The mechanisms are:

| Mechanism | What it does |
| --- | --- |
| **Ignore patterns** (`.stignore`) | Exclude paths by glob. This *is* selective sync. |
| **Folder type: Receive Only** | Take changes, never send them. Good for a modeller who should not push edits back. |
| **Folder type: Send Only** | Push changes, never accept them. Good for the developer's authoritative source. |
| **Separate folders** | The bluntest and often best tool — split "characters", "environments", "textures" so people subscribe to what they need. |

Two things worth knowing:

**`.stignore` is never synced.** `lib/fs/filesystem.go` hard-excludes it:

```go
var internals = []string{".stfolder", ".stignore", ".stversions"}
```

Each machine has its own. You cannot centrally push ignore rules — *except* via
the `#include` directive: commit a `team.stignore` file inside the synced folder
(that file *does* sync), and have each device's `.stignore` contain:

```
#include team.stignore
```

Now you maintain the rules in one place and everyone picks them up.

**Ignores are set at accept time.** When someone adds or accepts a folder, the
GUI adds it **paused**, opens the Ignore Patterns tab pre-filled with the
configured defaults, and only starts syncing once they save. That is a genuinely
good flow — it means a modeller can deselect the 200 GB of source textures
*before* anything downloads. It is easy to miss if nobody tells them it is there.

## 3. Default ignore patterns are supported — use them

`Defaults.Ignores` (`lib/config/config.go`) applies to newly added folders,
exposed at `/rest/config/defaults/ignores` and editable in the GUI under
*Actions → Advanced → Defaults*.

A tested starting set for Blender work:

```
// Blender numbered backups and save-temp files
(?d)*.blend[0-9]
(?d)*.blend@
// OS and editor junk
(?d)Thumbs.db
(?d)desktop.ini
(?d).DS_Store
(?d)*.tmp
(?d)~*
```

Verified: with these applied to a folder containing `scene.blend`,
`scene.blend1`, `scene.blend2`, `scene.blend@`, `texture.png`, `Thumbs.db` and
`render.tmp`, Syncthing indexed exactly `scene.blend` and `texture.png`.

`(?d)` means "these files may be deleted if they are all that is left in a
directory", which is what you want for junk.

Blender's `.blend1`/`.blend2` backups are otherwise pure waste: they are large,
change on every save, and are recoverable locally. Excluding them is usually the
single biggest bandwidth win.

## 4. File versioning is OFF by default — turn it on

`FolderConfiguration.Versioning` has no default, so new folders keep **no
history**. For binary assets that no one can merge, this is the riskiest default
in the product: an overwrite or deletion propagates to everyone in seconds, and
the previous version is simply gone.

Available strategies live in `lib/versioner/`: `trashcan`, `simple`, `staggered`,
`external`. **Staggered** suits this team — it keeps decreasing density of old
versions up to a maximum age, so a month of history costs far less than a month
of full copies.

Set it in *Actions → Advanced → Defaults → Folder* so every new folder inherits
it.

## 5. Conflicts on binary files

Two modellers saving the same `.blend` produces, in
`lib/model/folder_sendrecv.go`:

```
scene.sync-conflict-20260822-143000-ABCD123.blend
```

Nothing is lost, but nothing is merged either — someone has to look at both and
decide. `maxConflicts` defaults to 10 per file.

There is no locking or check-out mechanism in Syncthing, and adding one would be
a substantial feature. For a team of three the practical answer is social
(own your own directories, or use Send Only / Receive Only so only one machine
is authoritative) rather than technical.

Worth telling the modellers explicitly: **a `sync-conflict` file is not an
error, it is a rescued copy.** Otherwise they will delete them.

## 6. Partial writes are handled correctly

Worth stating because it is a common worry: receivers never see half-written
files. Downloads land in `.syncthing.<name>.tmp` (`lib/fs/tempname.go`) and are
renamed into place only when complete. A modeller will not open a half-synced
`.blend`.

The sending side is fine too — if Blender is still writing when a scan fires,
Syncthing sees the file change again and rescans. `fsWatcherDelayS` defaults to
10 s, which absorbs most of it.

## 7. Nothing showed that Syncthing was running — now the tray does

Syncthing has no notification-area icon and no service mode. Started from the
sign-in shortcut it ran completely invisibly, which for this team is the worst
possible failure mode: if it stops, or somebody closes it, nothing says so and
the files simply stop arriving. People notice days later.

The sign-in shortcut now starts `desuq-syncthing-tray.exe`, which starts
Syncthing, keeps it running, and shows one of five states:

| Icon | Meaning |
| --- | --- |
| Violet, tick | Up to date |
| Blue, arrows | Syncing or scanning, with the amount left in the tooltip |
| Grey, pause bars | Paused |
| Red, exclamation | A folder has an error — open the GUI to see it |
| Dark, dash | Syncthing is not running |

Right-click gives **Open**, **Pause Syncing** and **Quit**. Pause uses the same
endpoint as the GUI's *Pause All* button: it pauses every device, so nothing
transfers, while local scanning carries on.

Worth telling the modellers: **the icon is the app**. Quitting from that menu
stops syncing until they sign in again or start it from the Start Menu.

## 8. Nothing reached the user unless the GUI was open - now it does

Syncthing knows a great deal that nobody ever sees. It has a full event stream
at `/rest/events` - devices connecting, folders erroring, sync finishing,
someone asking to share a folder - and **no notification of any kind**. Unless
the web GUI happens to be open in a browser tab, every one of those passes in
silence.

For this team that is the same failure as having no tray icon, one level up.
The icon says "something is wrong"; it does not say so loudly enough to notice,
and nobody is looking at it.

The tray now consumes that event stream and raises real Windows toasts. Every
toast is click-through: clicking it opens the web GUI at the page that can act
on the thing being reported.

| Toast | Raised when | Held back by |
| --- | --- | --- |
| A new device wants to connect | An unconfigured device tries to connect | One per device, until it is dealt with |
| A new folder has been offered | A known device offers a folder | One per folder *and* offering device |
| Sync complete | A folder that was behind reaches zero | 10 s settle, then one toast for all folders that finished; 2 min per folder |
| Problem with *folder* | A folder enters the error state | One per folder per 30 min, reset when it recovers |
| Disk nearly / completely full | Free space falls under twice the folder's reserve | One per **drive** per 6 h, reset when space recovers |

**The restraint is the feature.** `LocalChangeDetected` fires once per file and
`FolderSummary` every few seconds per folder; a naive "toast on interesting
event" makes the first sync of a texture library unusable and teaches the user
to dismiss everything, which is worse than no notifications at all. So the tray
subscribes to a deliberately narrow event mask - the per-file events are not in
it - and every alert has a cooldown. "Sync complete" in particular fires only on
the transition from *behind* to *in sync*, never on an idle folder rescanning
and finding nothing.

Two things learned building it, both non-obvious:

- **Event IDs are per subscription, not global.** Syncthing keeps one buffer per
  distinct event mask and numbers each from 1. Asking `/rest/events` for the
  latest ID *without* an `events=` filter and then polling *with* one compares
  two unrelated counters, and silently drops everything below the other
  subscription's number. The bootstrap query must carry the same mask as the
  polls that follow.
- **Windows suppresses toasts in full screen.** Playing a full-screen video or
  game turns on Do Not Disturb automatically, and toasts go to the Action Centre
  instead of the screen. That is correct behaviour, but it looks exactly like a
  broken notifier when testing.

The toasts are raised by calling WinRT through `combase.dll` directly
(`custom/tray/notify_windows.go`). The alternatives were both worse: the
`Shell_NotifyIcon` balloon route needs the tray icon's `NOTIFYICONDATA`, which
`fyne.io/systray` keeps private, and shelling out to PowerShell spawns a 30 MB
process per toast and fails silently under Constrained Language Mode. Clicks
use `activationType="protocol"`, so opening the GUI needs **no COM activator
and no registration** beyond a per-user registry key naming the app.

Notifications can be turned off per machine with `--quiet` on the tray, and
appear in Windows' own notification settings under *desuqcafe Syncthing*.

## 9. Adding a device is mutual, but it is not authentication

Both sides have to add each other before anything syncs, and that mutuality is
routinely mistaken for proof of identity. It is not. It proves the two
installations agreed on an ID. It says nothing about **whose** ID it is.

In practice the ID is pasted into a chat window and trusted. Anyone able to
edit that message can substitute their own ID; both people add the attacker,
both see "Connected", and nothing anywhere looks wrong. Syncthing's own
documentation says the ID must be exchanged over a trusted channel. Nothing in
the product helps you check that you did.

**What the fork does about it**

The device editor and the device panel now show a short authentication string
for the pair -- the same idea ZRTP and Signal use, dressed as a collectible
card with a finishing-move name:

```
《 CRIMSON TALISMAN NOCTURNE 》Rank CLXXVI
```

The card carries those same four bytes three separate ways: the words, the
rank, and a sigil drawn from them. That redundancy is deliberate. Someone who
would skim past "CLXXVI" will still notice that their partner has a
nine-pointed star where this screen shows six, and the rarity tier gives a
one-glance check ("mine's gold") before a single character has been read.

The two device IDs are sorted, joined and hashed with SHA-256. Three bytes of
the digest index three wordlists of 256; a fourth gives the rank. Sorting is
what makes both machines agree regardless of who is adding whom, and 256³ × 256
is 2^32 possible phrases. Swap either ID and the phrase changes.

**The part that is easy to get wrong.** The strength is entirely in the channel
you compare over. Reading the phrase to each other on a voice call works
because you recognise the voice. Sending it through the same chat that carried
the device ID does **not** — whoever tampered with the ID can tamper with the
phrase. The card says so on screen, because a ritual people perform incorrectly
is worse than no ritual: it manufactures confidence without earning it.

Some deliberate decisions:

- **The wordlists are checked mechanically, not by eye.** No two words in a
  list share their first three letters, and none are within a Levenshtein
  distance of 3. If "SLASH" and "CLASH" were both in the strike list, a
  *mismatched* pair could sound matched down a phone line — the one failure
  this mechanism exists to prevent. `custom/scripts/check-handshake-words.ps1`
  asserts it and the Windows build runs it, so a careless edit fails the build.
- **The rank is shown as a Roman numeral and as a number** — "Rank CLXXVI
  (176 of 256)". The numeral is the aesthetic; the digits are what someone
  actually reads down a phone, because CCXLIII and CCXLIV are not
  distinguishable by ear.
- **SHA-256 is implemented in the page rather than taken from
  `window.crypto.subtle`.** SubtleCrypto only exists in a secure context. That
  covers `http://127.0.0.1`, but the GUI is routinely opened at
  `http://192.168.x.x` from another machine, where it is `undefined` — so the
  feature would have silently died on exactly the setup most likely to need it.
- **Confirmations are stored in `localStorage`, not in the config.** Writing
  them to the config would sync a claim about identity between machines, which
  is precisely the thing that cannot be trusted over the wire. A confirmation
  is a note to self and stays on the machine that made it. It is also voided
  automatically if either device ID changes, so re-pasting a different ID
  cannot inherit a tick it never earned.
- **Confirming means picking the right card out of three.** The failure mode of
  every "compare these codes" dialogue ever shipped is that people click yes
  without reading; a checkbox saying "it matched" gets ticked by reflex. So
  confirming deals three cards -- the real one and two decoys -- and asks which
  one the other person is describing. It adds no entropy and is not meant to:
  it is an attention check on a step whose entire value is that a human really
  looked. The decoys are derived from the digest rather than at random, so the
  same pair always deals the same spread; a hand that reshuffled on every
  redraw would suggest the phrase itself was unstable. No decoy shares any of
  the four positions with the real card, so someone comparing only the rank
  cannot pick a decoy and be told they were right.
- **It does not block saving.** The card informs; it does not gate the button.
  Gating would mean editing upstream's save path, and would trap anyone whose
  browser failed to load the directive.

**What it does not do.** It does not defend against someone who can grind
device IDs: 2^32 is enough to make a live substitution fail, not enough to
resist an attacker generating keys until one collides with a target phrase.
For a three-person studio that is the right trade. It is worth knowing before
anyone points this at a larger deployment.

## 10. What is verified, and what is not

Verified 2026-08-23 against **two instances on separate ports sharing a real
folder**, not just single-device:

- Seeding produces the same defaults on a second machine, and a newly accepted
  folder inherits staggered/30d and the 20 GB reserve from them.
- The ignore set holds across the wire: the sender had 21 files, the receiver
  got 16 — `.blend1`, `.blend2`, `.blend@`, `.tmp` and `Thumbs.db` were never
  transferred at all, not transferred-then-hidden.
- The tray reports `Syncing Project Assets` / `157.5 MB remaining` counting
  down, and `1 of 1 devices connected`.
- The installer was run for real over an existing install: it preserved the
  device key, folders and devices, seeded the defaults, pointed the sign-in
  shortcut at the tray, and on a second run stopped both processes and left
  `config.xml` byte-identical.
- The device verification card was rendered through real Angular and asserted
  on: the phrase, both its forms, the on-screen warning, and -- the one that
  matters -- that a stored confirmation is voided the moment either device ID
  changes. The in-page SHA-256 matches Node's `crypto` byte for byte, and
  across 20,000 synthetic pairs every one of the 256 values in each of the
  four positions is reachable with no collisions, so the space really is 2^32.
- **All five notifications fired end to end against those two instances**, and
  were confirmed on screen. In order: an unknown device dialling in raised one
  toast despite Syncthing emitting the underlying event ten times; the folder
  offer named the offering device and the folder label; the completion toast
  reported `12.6 MB received`, the real transferred figure; the disk warning
  came through the fork's own `/rest/system/diskfree`; and removing a
  `.stfolder` marker produced the folder-error toast.

**Not verified:** uninstall. It shares `StopRunningInstance` with the upgrade
path, which is exercised, but the `DelTree` prompt has never been run.

## 11. Things that surprised us, worth knowing before changing anything

- **`limitBandwidthInLan` defaults to `false`.** Rate limits are silently
  ignored on LAN and loopback until it is switched on. If a limit "does not
  work", this is why.
- **`/rest/db/browse` returns the *global* tree**, not the local one — it lists
  files the remote has that were never pulled. That is the hook any real
  "tick what you want" selective-sync UI would hang off (see §2).
- **A pending folder carries no size** (§1), so nothing can be decided about
  capacity until the folder actually starts.
- **`.stfolder` is created hidden**, so a synced folder has *no* visible marker
  in Explorer. Nothing in Syncthing writes a folder icon or overlay.

## Recommended configuration

Applied per machine, under *Actions → Advanced → Defaults*:

| Setting | Default | Suggested | Why |
| --- | --- | --- | --- |
| Folder → Min Disk Free | 1 % | 20 GB (absolute) | Predictable across drive sizes; 1 % of a small SSD is nothing |
| Folder → File Versioning | none | Staggered, 30 days | Binary assets cannot be merged; deletion is otherwise permanent |
| Default Ignores | empty | the block in §3 | Kills the `.blend1` churn |
| Folder type (modellers) | Send & Receive | Receive Only where they only consume | Prevents accidental upstream overwrites |

These are per-device settings rather than build-time ones, so **the installer
seeds them into `config.xml` before Syncthing first starts**
(`custom/scripts/seed-config.ps1`). A fresh install therefore already has
staggered versioning, the 20 GB reserve and the Blender ignore set in place,
and nobody has to find *Actions → Advanced → Defaults*.

What is seeded, and what is deliberately not:

| Seeded | Not seeded |
| --- | --- |
| `defaults/folder/versioning` — staggered, 30 days | Existing folders and devices — only the *defaults* are written |
| `defaults/folder/minDiskFree` — 20 GB | Folder type (Receive Only) — that is a per-folder, per-person choice |
| `defaults/ignores` — the §3 block | The GUI password — set it per machine if the LAN is not trusted |
| `gui/theme` — `violet` | |
| `device/@name` — the Windows user name, rather than upstream's host name, because `DESKTOP-A1B2C3` tells nobody which machine they are looking at | |

Re-running the installer does **not** re-seed: a sentinel file in the data
directory records that it has been done, so an upgrade never overwrites
settings someone has since changed in the GUI. To force it after changing the
recommended values, bump `$SeedVersion` in the script, or run it by hand:

```powershell
& "$env:LOCALAPPDATA\Programs\desuq-syncthing\seed-config.ps1" `
    -DataDir "$env:LOCALAPPDATA\desuqcafe-syncthing" `
    -Binary  "$env:LOCALAPPDATA\Programs\desuq-syncthing\desuq-syncthing.exe" `
    -Force
```
