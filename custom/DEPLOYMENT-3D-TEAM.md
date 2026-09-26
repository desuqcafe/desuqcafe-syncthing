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

## 2. Selective sync existed, but only as ignore patterns — now there is a picker

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

**The order this is done in matters, and getting it wrong wedges the folder.**
The included file lives inside the folder. The folder will not start until the
include resolves, and the include cannot resolve until the folder has synced,
so a device that is given the `#include` line *before* its first sync deadlocks
on itself: Syncthing logs `failed to load include file team.stignore: file not
found` and sits in an error state indefinitely, syncing nothing, forever.

There is no way to make the line itself safe. `lib/ignore` has no tolerant
form of `#include`, and adding one would be the wrong fix anyway — a rule set
that silently loses the 200 GB it was holding back is a worse failure than one
that stops and says so.

So the safe recipe is an ordering:

1. The sender commits `team.stignore` **into the folder**, and lets it sync.
2. Each receiver accepts the share and lets it sync **once**, with whatever
   patterns it already has (the seeded set from §3, or a picker selection).
   Confirm `team.stignore` is actually on disk.
3. *Then* add `#include team.stignore` to that folder's own patterns, in
   *Edit Folder → Ignore Patterns*.

Two traps that follow from the same cause:

- **Never put the `#include` in *Actions → Advanced → Defaults*.** There it
  deadlocks every folder anyone accepts from then on, not just one.
  `seed-config.ps1` refuses to write one into the seeded defaults for exactly
  this reason, rather than trusting a warning in a document.
- **Removing and re-accepting a folder re-arms the trap**, because the new
  copy starts empty again. The `#include` has to come off before the folder is
  removed, or be added back only after the re-accept has synced.

For three people, weigh this against just seeding the rules per machine (§3),
which has no ordering to get wrong. The `#include` buys central maintenance;
whether that is worth an indefinite wedge on a mis-ordered rollout is a real
question, not a rhetorical one.

**Ignores are set at accept time.** When someone adds or accepts a folder, the
GUI adds it **paused**, opens the Ignore Patterns tab pre-filled with the
configured defaults, and only starts syncing once they save. That is a genuinely
good flow — it means a modeller can deselect the 200 GB of source textures
*before* anything downloads. It is easy to miss if nobody tells them it is there.

But what it opens is a textarea, and what it wants is globs. For the people
this fork is for, "deselect the source textures" and "write
`/Textures/Source`" are not the same instruction, and the second one does not
get done.

### What the fork does about it

The Ignores tab of the folder editor now offers **Choose what to sync**, and
the folder panel has a **Choose Files** button that reopens the same picker on
a folder that already exists. Ticking the option at accept time runs this
sequence:

1. the folder is added **paused**;
2. `*` is written as its only ignore line, so it will hold everything back;
3. it is started. The index arrives; no file data does;
4. the picker shows the whole remote tree with everything ticked, the weight of
   the selection against the free space on the target drive, and per-row sizes;
5. what is left unticked is written back as ignore patterns, and the rest pulls.

Step 2 is the part that makes the rest safe. Between accepting a share and
choosing what to take from it, **not one byte of content is downloaded** —
verified with 18 files known and 0 files local, with an empty directory on
disk, while the entire tree was browsable.

The hook that makes this possible at all is that `/rest/db/browse` calls
`GlobalDirectoryTree`, which walks the *global* index rather than the local
disk. It therefore lists files that were never pulled, which is exactly the
list a picker needs and is not obtainable any other way.

Some deliberate decisions:

- **It writes the paths you did *not* tick, not the ones you did.** A file
  added remotely inside a directory you kept then arrives on its own, which is
  what ticking the directory meant. The walk emits the minimal set: a wholly
  unticked node is one line and its children need none, and only partially
  ticked directories are descended into — which is also why no `!` re-include
  lines are ever needed.
- **The picker owns a marked block and nothing else.** Everything outside
  `//// desuqcafe selective sync` … `end` is preserved verbatim through every
  rewrite, so the seeded Blender set from §3 and any `#include` line survive.
  Managed lines that no longer match anything in the tree are kept rather than
  dropped, under a comment saying so: silently discarding a rule is how
  something that was holding back 200 GB quietly stops.
- **Backing out means "sync everything".** Closing the picker by any route on
  a freshly accepted share clears the `*`. The alternative is a folder that
  reports itself perfectly up to date while syncing nothing, forever, which is
  a far worse failure than downloading more than you meant to.
- **Past 20,000 files it shows directories only.** Fetching and rendering a
  six-figure texture library file by file is not something a browser will do
  well. The count comes from `/rest/db/status` before the tree is fetched, so
  the oversized fetch never happens. Files sitting outside any directory are
  kept either way in that mode, and the picker says so.
- **Names are escaped, with the platform's escape character.** This one is a
  trap. `asset[1].png` is a glob for `asset1.png`, which in a render output
  directory is very likely to be the file next to it. And `lib/ignore` picks
  its escape character at init — backslash normally, but **pipe on Windows**,
  because backslash is the path separator there. Writing `asset\[1\].png` on
  Windows does not escape anything: the parser runs `ToSlash` over the line
  first, so it becomes `asset/[1/].png` and matches nothing at all. An explicit
  `#escape=` line would settle it, but the parser rejects one that appears
  after any pattern and ours would have to follow the user's own lines, so the
  picker asks `/rest/system/version` instead.
- **A parse error is reported rather than swallowed.** `POST /rest/db/ignores`
  answers 200 and *then* reports in the body that Syncthing cannot read what it
  just stored. A folder in that state refuses to scan or pull at all, so
  letting it pass looks exactly like a folder that simply never syncs.

**On the `#include` deadlock**, see the ordering above. The picker surfaces the
error rather than appearing to succeed — that is what the last bullet is
about — but it cannot fix it, and neither can anything else: the only remedy
is not to write the line until the file is there.

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
| An update is available | A device you sync with is running a newer build of this fork | One per version per 24 h (§17) |

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

## 10. A synced folder looked like any other folder — now it does not

There was no sign at all. `.stfolder` is created hidden, nothing writes a
folder icon, and Syncthing has no shell integration, so in Explorer a synced
folder and an ordinary one are the same yellow rectangle. Somebody moves one,
or works in a copy of it, and finds out days later.

Every synced folder now carries the fork's icon: a violet folder with a sync
ring on it, sitting among the yellow ones. The tray writes it, reconciles every
two minutes so a folder added later gets one too, and re-tries a folder whose
drive was not plugged in.

**No shell extension.** The overlay-icon badge Dropbox and OneDrive put on the
corner of a file was deliberately not built. Overlays require a registered
in-process COM server, and they come out of a global pool of roughly fifteen
slots that those two products already crowd — ours would very likely never be
drawn, and we would have put a DLL into every Explorer process to achieve that.
`desktop.ini` needs no administrator, no COM, no registration, and leaves
nothing behind but a file Explorer ignores once the icon it names is gone.

Four things this had to get right:

- **The marker must be ignored before it is written.** `desktop.ini` lands
  inside the synced folder. Written first and excluded second, it would sync to
  everyone carrying a path that is meaningless on their machine, and in a
  **receive-only** folder it would appear as an unexpected local addition for
  someone to worry about. So the tray checks the folder's ignore patterns
  first, appends `(?d)desktop.ini` if nothing there covers it, and only then
  writes. An explicit `!desktop.ini` is respected and the folder is left alone.
  (§3's seeded set already contains the rule, so on a fresh install there is
  usually nothing to add.)
- **The folder needs an attribute or Explorer never looks.** A `desktop.ini` in
  a plain folder is inert; the folder must be marked read-only or system.
  Read-only is the one to use — system would hide the folder from anyone who
  has not turned off *hide protected operating system files*, which for a
  folder whose whole purpose is to be visible would be perfectly backwards.
  Read-only on a *directory* does not stop anything being written inside it; it
  is the flag that means "customised".
- **UTF-16LE with a BOM.** The shell reads `desktop.ini` through
  `GetPrivateProfileString`, which only treats a file as Unicode if it starts
  with the byte-order mark. Without it the icon path is read in the system code
  page, and any user whose name is not spelled in ASCII gets a path to a file
  that does not exist.
- **Overwriting one is not the same as writing one.** `CreateFile` refuses to
  truncate a hidden or system file unless those attributes are passed in, so
  the second run has to clear them first. This fails only on the *second* run,
  which is exactly the sort of thing a single-pass test says nothing about.

To undo it — every marker file, and the read-only attribute with it:

```powershell
& "$env:LOCALAPPDATA\Programs\desuq-syncthing\desuq-syncthing-tray.exe" `
    --home "$env:LOCALAPPDATA\desuqcafe-syncthing" --clear-folder-icons
```

That reads the folder list out of `config.xml` rather than asking Syncthing, so
it works with everything stopped. `--no-folder-icons` on the tray turns the
whole thing off for a machine. Note that the **uninstaller does not run it**:
after removal the marker files stay, pointing at an icon that is gone, and
Explorer silently falls back to the ordinary folder icon. Harmless, but it is
litter, and worth clearing by hand before uninstalling if it matters.

## 11. Rate limits do nothing on the local network, and nothing said so

`limitBandwidthInLan` defaults to `false`
(`lib/config/optionsconfiguration.go`), and while it is off the limiter skips
every connection it considers local:

```go
// lib/connections/limiter.go
func (w waiterHolder) unlimited() bool {
	if w.isLAN && !w.limitsLAN.Load() {
		return true
	}
```

`isLAN` is `c.IsLocal()` from `lib/connections/service.go`. So somebody sets a
limit, tests it against the machine on the next desk -- or against a second
instance on loopback, which is also "local" -- sees no effect at all, and
concludes rate limiting is broken.

Measured, 2 MB over loopback with `maxSendKbps` at 64 KiB/s:

| `limitBandwidthInLan` | Time |
| --- | --- |
| `false` (the default) | **1.5 s** |
| `true` | **25.1 s** |

25 seconds rather than the 32 the arithmetic suggests, because the first
512 KiB comes out of the limiter's burst allowance
(`limiterBurstSize = 4 * 128 << 10`) before any throttling begins.

**The default is deliberately left alone.** Throttling LAN transfers is the
wrong thing for a studio moving tens of gigabytes of textures between machines
in one room, and for anyone whose colleagues are remote the setting changes
nothing at all — those connections are not local, so the limit already
applies. The problem was never the default. It was that the default was
invisible: the checkbox is three fields below the rate inputs in *Settings →
Connections*, and in *Edit Device → Advanced* it is not on the screen at all.

So the fork surfaces it instead. Under both pairs of rate fields, and only once
a limit is actually set, there is now a note saying whether that limit applies:

> **Not applied on the local network.** Only traffic that leaves this network
> is limited. Change that with "Limit Bandwidth in LAN" in Actions → Settings →
> Connections.
> *This device is connected over the local network right now, so nothing is
> being limited.*

That last sentence is not a guess. `/rest/system/connections` reports `isLocal`
per connection, from the same `IsLocal()` the limiter consults, so in the
device editor the note reports what is happening to that device rather than
what might happen to some device. With the checkbox ticked it flips to
confirming — "Applies to every connection, including devices on this network" —
rather than vanishing, because a note that disappears when you fix something
teaches nobody what they fixed.

## 12. Syncthing phoned home on a crash, and never asked

Upstream's anonymous usage reporting is opt-in behind a modal, and off until
somebody clicks yes. That is the part everybody knows about, and it was not the
problem.

There are **three** reporters, and they are gated by **two** options:

| Reporter | Posts to | Gated on | Upstream default |
| --- | --- | --- | --- |
| Usage report — folder counts, sizes, platform, settings | `data.syncthing.net` | `urAccepted >= 2` | off |
| Failure reports — non-fatal internal errors | `crash.syncthing.net/failure` | `urAccepted > 0` | off |
| **Panic-log upload** | `crash.syncthing.net` | **`crashReportingEnabled`** | **on** |

The third is on a different switch, and nothing ever asks about it. On a stock
build, a crash uploads the panic log — goroutine stacks and the tail of the
run's output, which for us means folder names and paths — to a third party the
user has never heard of. That is not a hypothetical: forcing the fork's kill
switch back on and re-running `go test ./cmd/syncthing/` shows the upload
happening and the log renamed to `.reported.log` afterwards.

### What the fork does about it

`lib/build/desuq_telemetry.go` holds one constant:

```go
const TelemetryEnabled = false
```

All three reporters check it before doing anything. The usage-report and
failure-report services start and immediately go inert; the panic uploader
returns before it even reads the config. Nothing subscribes to the failure
event stream, so nothing is buffered either.

With that in place the GUI's controls would be switches wired to nothing, so
they are gone too:

- the consent modal is not included in the page at all, and neither of the two
  places that raised it survives — including the one that set a cookie on
  first visit and started nagging four hours later, on every config load,
  until answered;
- *Actions → Settings → Connections* no longer has an **Anonymous Usage
  Reporting** dropdown. In its place is a line saying the build sends none;
- choosing "Stable releases and release candidates" under Automatic upgrades
  no longer sets `urAccepted` as a side effect. Upstream does, which means
  picking an upgrade channel opts you into usage reporting without saying so.

`urAccepted` is seeded to `-1` and `crashReportingEnabled` to `false` anyway,
and the struct defaults changed to match, so a config generated with no seeding
at all is still correct. **That part is cosmetic.** It stops `config.xml`
reading as though the question were still open; it is not what stops the
traffic.

`build.IsCandidate` force-enables usage reporting upstream. It is
`strings.Contains(Version, "-rc.")` and our tags are `v2.1.4-desuq.N`, so it
was never going to fire — but that is a naming convention, and the guard there
now depends on the constant instead.

### What is left, and why

Global discovery and relays are still on. They are a much larger third-party
surface than the reporting was, and turning them off would break the thing the
fork exists to do; see section 14 for the reasoning and what to do instead.

`/rest/svc/report` still builds a report when asked. Building one is local and
harmless — it is what upstream's "Preview" link showed — and it is never
posted. Deleting `lib/ur` outright would mean touching `lib/model`, `lib/api`,
`lib/syncthing` and the generated mocks, for no change in what leaves the
machine.

## 13. What is verified, and what is not

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
- **All five of the original notifications fired end to end against those two
  instances**, and
  were confirmed on screen. In order: an unknown device dialling in raised one
  toast despite Syncthing emitting the underlying event ten times; the folder
  offer named the offering device and the folder label; the completion toast
  reported `12.6 MB received`, the real transferred figure; the disk warning
  came through the fork's own `/rest/system/diskfree`; and removing a
  `.stfolder` marker produced the folder-error toast.

- The selective-sync picker was driven end to end against those two instances
  through real Angular and the real fancytree, with the assertions reading the
  filesystem rather than the API: 50 checks in
  `custom/scripts/test-selective-render.js`, which builds its own fixture tree
  on the sending instance so that it is repeatable. The ones that matter are that
  nothing at all was on disk while the tree was being browsed; that
  `Textures/Source` was **never created**, rather than created and hidden; that
  `asset[1].png` could be excluded without taking `asset1.png` with it; that
  the seeded ignore block survived two rewrites; that dismissing a fresh accept
  left the folder syncing all 18 files rather than stranded on `*`; and that an
  unreadable ignore file was reported instead of passing for success.

- **The main screen was finally looked at in a browser** (2026-08-24), which
  until then nothing in this fork had been: every GUI test here is jsdom, and
  jsdom does not paint. Both instances of the pair were opened side by side
  under the violet theme and render correctly, and *Open folder* was clicked
  for real — Explorer opened at the folder. Its refusals were checked directly
  rather than inferred: no folder 400, unknown folder 404, a `../../../Windows`
  sub-path 400, a NUL byte 400. The first-run wizard has **still** never been
  painted anywhere, and remains the largest unverified surface.

- The Explorer folder icons were verified through the shell itself rather than
  by checking our own output: `SHGetFileInfo`, the API Explorer uses, resolves
  a marked folder to the fork's `folder.ico` and gives it its own system
  image-list slot (286) where a plain folder gets the generic one (3). The
  claim that matters was checked live too -- on a **receive-only** folder with
  no default ignores at all, the tray added the rule itself, wrote the marker,
  and afterwards `receiveOnlyTotalItems` was 0, `globalFiles` was unchanged on
  both sides, and no `desktop.ini` ever reached the sending device.
- `--clear-folder-icons` was run from the shipped binary with Syncthing
  stopped: marker gone, read-only attribute cleared, icon removed, exit 0.

- The LAN rate-limit note was rendered through real Angular and asserted on,
  16 checks in `custom/scripts/test-lanlimit-render.js`: that it stays silent
  until a limit is actually set, that it points at the checkbox below it in
  Settings and at the other dialogue in the device editor, that it reports a
  device's live connection but claims nothing about one that has dropped, and
  that ticking the box turns it into a confirmation rather than making it
  vanish. The claim itself was measured against the two instances -- the table
  in section 11 is that measurement, not arithmetic.

- Seeded device naming was run against the real script and binary, 8 checks in
  `custom/scripts/test-seed-naming.ps1`: a fresh install is renamed, a forced
  re-seed over "Yuki's Workstation" keeps it *and still applies the other
  defaults*, the host name and the `DESKTOP-`/`LAPTOP-` forms are still
  replaced, "Desktop upstairs" is not, and an explicit `-DeviceName` wins.

- The telemetry strip was verified **by watching the wire, both ways**. Three
  Go tests -- two in `lib/ur`, one in `cmd/syncthing` -- stand an
  `httptest` server up as the usage-report and crash-report endpoint, turn every
  telemetry option all the way *on* in the config, run the reporter, and assert
  the server was contacted zero times. The panic test adds an independent
  witness: an uploaded log gets renamed to `.reported.log`, so the original
  still sitting there says the upload was never attempted rather than merely
  having failed.

  Each was then checked for the failure that matters -- that it is not passing
  vacuously. Flipping `TelemetryEnabled` to `true` and re-running makes all
  three fail, and *how* they fail is the finding: `usage report server was
  contacted 1 times`, `failure handler subscribed to the config 1 times`, and
  the crash server contacted with the panic log renamed. Upstream's build
  really does upload a panic log unasked; this is that behaviour, observed.

  Two further checks in `test-seed-naming.ps1` assert the seeded `config.xml`
  says `urAccepted -1` and `crashReportingEnabled false`, and the GUI removals
  were confirmed against the running instance rather than the working tree:
  the served `index.html` includes neither usage-report modal, the served
  settings view has no `urVersion` control, and the served controller contains
  no `showModal('#ur')`.

- The Explorer icon cache is now poked with `SHChangeNotify(SHCNE_UPDATEDIR,
  SHCNF_PATHW | SHCNF_FLUSHNOWAIT, ...)` after a marker is written or removed,
  so a folder already open in Explorer repaints instead of keeping its plain
  icon until the cache next happens to be rebuilt. Two things about this are
  asserted and one is not. Asserted: that the export resolves, because a
  misspelled `LazyProc` name is a panic in the tray on a user's machine rather
  than a build error; and that the shell is told **only when something actually
  changed**, which is the real risk, since the tray reconciles every folder
  every two minutes. **Not asserted: the repaint itself.** `SHGetFileInfo`
  answers out of a per-process cache that is not the one a folder view draws
  from, and it returns the marked answer whether or not the notification was
  sent -- a test built on it passes with the call removed, which is worse than
  no test. Confirming the repaint needs a person with a folder open.

- **Uninstall was finally run, for real, against a live install.** It was the
  one thing in this document that had never been exercised. Both directories
  were copied aside first; in the event nothing had to be restored from the
  copy, because the data directory is not touched unless you say so.

  The run: `unins000.exe /VERYSILENT`, which takes the *default* answer to the
  data-deletion prompt, and the default is deliberately No. Exit 0. The
  program directory was removed entirely, the data directory kept, and
  `key.pem` and `cert.pem` came out byte-for-byte identical to the copy taken
  beforehand -- so the device keeps its identity and the other two people's
  device lists do not change. Nothing was left behind: no Start Menu entry, no
  sign-in shortcut, no desktop shortcut, no Add/Remove Programs row, no
  processes.

  The folder-icon clearing was exercised rather than assumed. A throwaway
  folder was added to the real `config.xml` and marked the way the tray marks
  one, and after the uninstall its `desktop.ini` was gone and its read-only
  attribute cleared -- so the markers really do come off while the tray binary
  and `config.xml` are both still on disk, which is why the call sits at
  `usUninstall` rather than `usPostUninstall`. Reinstalling put everything
  back, and the tray came up healthy on the same three processes.

  Still not exercised: **answering Yes** to the data-deletion prompt. That
  branch is one `DelTree` call, and testing it means destroying a real device
  identity to watch a directory be deleted.

- `folder.ico` went from **365 KB to 50 KB**, and the tray binary with it,
  because its 128 and 256 pixel entries are now PNG rather than DIB. A DIB
  entry is uncompressed, so those two were 90% of the file. The tray's own
  status icons stay DIB at every size deliberately -- systray hands their path
  to `LoadImage()`, which is fussier than Explorer, and at 16-48 pixels the
  compression would buy nothing. That it still *works* is the same
  `SHGetFileInfo` assertion as before, unchanged and still resolving the
  marked folder to its own image-list slot; a second test asserts the two
  large entries are still PNG, because regenerating with that branch dropped
  would look like nothing at all except a much larger binary.

- The first-run guide was driven through real Angular against canned REST
  responses: 34 checks in `custom/scripts/test-wizard-render.js`. The ones that
  matter are that it does **not** open over a machine that already has a device
  or a folder; that closing it remembers the step without marking it finished,
  while *Done* stops it opening itself again for good; that each step's tick
  follows the real state rather than what was clicked; and that the Copy button
  works through `execCommand` with no `navigator.clipboard` present, which is
  the path that runs at any LAN address. The wording of the sentence the whole
  handshake depends on -- read it on a call, not in the chat that carried the
  code -- is asserted rather than left to survive an edit.

- The update notice was run for real, on **two instances built from two
  different tags**: a pair on `v2.1.4-desuq.2` and `v2.1.4-desuq.3`, with the
  tray attached to the older one. `/rest/system/connections` on the older
  machine reported the newer peer's `clientVersion` verbatim, and bouncing the
  connection produced exactly the intended toast --

  > **An update is available**
  > Yuki Laptop is running v2.1.4-desuq.3 and you have v2.1.4-desuq.2. Click to
  > download the new installer.

  -- and then a second reconnect produced **no** second toast, which is the
  cooldown doing its job. The rule that carries the feature -- that a peer
  running **stock** Syncthing can never trigger it, at any version -- has its
  own test case, because getting it wrong produces a permanent, unactionable
  notice pointing at a release that does not exist.

**Not verified:** the `DelTree` branch of uninstall -- the one that runs when
somebody answers *Yes* to "also delete your configuration and database". Every
other path through the uninstaller has now been run against a live install.

**Not verified:** anything about layout or paint. Every GUI check in this
document -- the picker, the verification card, the LAN note, the first-run
guide, the telemetry removals -- was made through real Angular, real fancytree
and the real REST API under jsdom, or by reading what the server served. jsdom
does not lay out or paint, so what is asserted is structure and behaviour, not
appearance. No browser has rendered any of it.

## 14. Global discovery and relays: a bigger surface, and a different question

With the telemetry gone these are the only things left that contact a third
party, and they are a *much* larger surface than usage reporting ever was —
contacted every 30 minutes, forever, by every device. It is a fair question
whether they should go the same way. **The recommendation is no**, and the
reasoning is worth writing down because it is not "third parties are fine".

### What each one is, and what it actually discloses

| | What it does | Who sees what |
| --- | --- | --- |
| **Local discovery** | Broadcast/multicast on the LAN, port 21027 | Nobody outside the LAN. Nothing to argue about. |
| **Global discovery** | Announces this device's listen addresses to `discovery-announce-v4/v6.syncthing.net` every 30 min; looks peers up at `discovery-lookup.syncthing.net` | The announce body is literally `{"addresses": [...]}`. The device ID comes from the client certificate on the POST. So: device ID, public IP, listen addresses. **No folder names, no file names, no sizes, no counts.** |
| **Relays** | If two devices cannot reach each other directly, a volunteer relay from `relays.syncthing.net` forwards bytes between them | The relay carries **ciphertext**. `lib/connections/relay_dial.go` layers `tls.Client`/`tls.Server` with the device's own certificate *on top of* the relay session, so the operator sees which two device IDs are talking, when, and how much — not what. |
| **STUN** | `_stun._udp.syncthing.net` SRV lookup, then STUN to learn this device's public address for NAT traversal | Source IP, same as any STUN. |

Upstream deliberately splits announce and lookup across *different* hostnames
with `?nolookup` and `?noannounce`, so neither server sees both halves. That is
a real design decision in the user's favour and worth crediting.

### Why this is not the same call as the telemetry

The telemetry was pure outflow: data *about* the user, of no use to the user,
in exchange for nothing. Removing it cost nothing and there was no failure mode
to weigh.

These are load-bearing. Turn global discovery off and a device with no static
address becomes unreachable from outside its own LAN. Turn relays off and two
devices behind NATs that will not punch simply never connect.

And the failure mode is the bad kind: **silent, and indistinguishable from the
peer being switched off.** A modeller working from home sees "Disconnected"
with no explanation, and nothing on either screen can tell them the difference
between "cannot find the other machine" and "the other machine is off". For a
fork whose entire premise is that non-technical users should not have to
diagnose anything, trading a small metadata disclosure for that is a bad deal.

### What to do instead, in increasing order of effort

1. **Give every device a static address where it has one.** In *Edit Device →
   Addresses*, replace `dynamic` with `tcp://192.168.1.50:22000` for the
   machines in the studio. Discovery becomes a fallback rather than the path,
   and the in-studio case stops depending on any third party at all. This costs
   one field per device and is worth doing regardless.
2. **Turn off global discovery and relays only on machines that never leave the
   LAN**, and only once step 1 is done for them. A desktop that is always in
   the studio does not need either. Leave both on for laptops.
3. **Self-host, if it genuinely matters.** This is the honest answer to "I want
   it gone" applied to discovery, and it is more achievable than it sounds:
   both servers are in this tree already, `cmd/stdiscosrv` and
   `cmd/strelaysrv`. One small VPS runs both. Then set
   `globalAnnounceServers` and the relay address to your own, and no device
   contacts `syncthing.net` for anything, with **no loss of function**. That
   is a weekend of work and a running cost, not a config change, so it is a
   decision rather than a default — but it is the option that actually exists.

What is **not** recommended is switching them off across the board and finding
out later. If you do it, do step 1 first, and expect to be the person who
explains why a laptop stopped syncing at home.

## 15. Things that surprised us, worth knowing before changing anything

- **`limitBandwidthInLan` defaults to `false`.** Rate limits are silently
  ignored on LAN and loopback until it is switched on. If a limit "does not
  work", this is why. The GUI now says so next to the rate fields; see below
  for why the default was left where it is.
- **`/rest/db/browse` returns the *global* tree**, not the local one — it lists
  files the remote has that were never pulled. That is the hook the file picker
  hangs off (see §2).
- **The ignore-pattern escape character is `|` on Windows, not `\`.**
  `lib/ignore` swaps it at init because backslash is the path separator.
  A backslash-escaped pattern on Windows is silently rewritten into a different
  path and matches nothing (§2).
- **An `#include` in the default ignores deadlocks every new folder** (§2).
- **A pending folder carries no size** (§1), so nothing can be decided about
  capacity until the folder actually starts.
- **`.stfolder` is created hidden**, so a synced folder has *no* visible marker
  in Explorer of its own. Nothing in Syncthing writes a folder icon or overlay;
  the fork does, see section 10.
- **A `desktop.ini` is inert unless the folder carries the read-only or system
  attribute.** Writing the file and nothing else changes nothing at all, with
  no error anywhere (section 10).
- **SHGetFileInfo silently ignores `desktop.ini` when COM is not initialised.**
  It still succeeds and still returns an icon -- the generic one out of
  `imageres.dll`. Anything checking this mechanism from outside Explorer has to
  call `CoInitializeEx` first or it will conclude the feature is broken while
  Explorer shows the icon perfectly well. It also caches per path, so probing a
  folder *before* customising it poisons the answer you get afterwards.

## 16. The first ten minutes were an empty screen

Everything above improves a screen somebody has already found. This is about
the screen they land on, and it was the worst one in the product.

A modeller who has just run the installer gets: an empty folder list with an
*Add Folder* button, a device panel showing only their own machine, and nothing
else. There is no next action anywhere on it. Worse, the one thing they
actually need in order to make progress -- their device code, to send to
whoever is sharing files with them -- is behind a menu called **Actions**,
under an entry called **Show ID**, in a dialogue that also offers to share it
by SMS.

That is where the install ends and the phone call to the developer starts, and
it happens to every new machine.

**What the fork does about it**

A four-step guide, opened automatically the first time and reachable
afterwards from *Actions → Setup guide*:

| | Step | Done when |
| --- | --- | --- |
| 1 | **Name this machine** | always; it is prefilled with the seeded name |
| 2 | **Send your code** -- the device ID large, with Copy and the QR upstream already serves at `/qr/` | somebody adds this device, or asks to connect to it |
| 3 | **Check it is really them** -- the verification card from §9, with the "read it on a call" warning | a card has been confirmed |
| 4 | **Choose what to sync** -- what a folder offer looks like, and the button that hands it to the picker from §2 | a folder exists |

The steps tick themselves off against the actual state of the world rather than
against what the person has clicked, so reopening it a week later shows what is
genuinely still outstanding.

Some deliberate decisions:

- **It never traps anybody.** Three of the four steps cannot be completed by
  the person in front of the screen: they complete when somebody else adds
  them, calls them, or shares a folder. Step 2 in particular will routinely sit
  unfinished until tomorrow. So the guide closes on Escape, on the backdrop and
  on an explicit *Finish this later*, remembers the step it was on, and reopens
  there. A wizard that demands a third party's attention before it will release
  the screen is worse than no wizard: the tab gets closed and never reopened.
- **It does not open over a working setup.** If the machine already has a
  device or a folder, the person found their own way here and a modal on top of
  it is an interruption. Only *Actions → Setup guide* opens it then.
- **Only "Done" on the last step stops it coming back by itself.** Closing is
  not the same as finishing, and neither is reaching step 4.
- **It reads its state over REST, not off `syncthingController`'s scope.** That
  keeps the merge cost to four additive lines in `index.html`, and it is what
  lets `custom/scripts/test-wizard-render.js` drive the whole thing through
  real Angular without standing up the controller.
- **Step 4 does not re-implement accepting a folder.** It calls upstream's own
  `addFolderAndShare`, passed in as a binding, so the path and folder-type
  defaults stay upstream's and the picker keeps working exactly as it does from
  the offer bar.
- **The Copy button carries the `execCommand` fallback.** `navigator.clipboard`
  is undefined outside a secure context, and the GUI is routinely opened at
  `http://192.168.x.x` from the machine next door -- the same trap that made
  the handshake carry its own SHA-256 (§9). That fallback is not legacy
  support here; it is the path that runs for half the users.

**What it does not do.** It does not gate anything, it does not configure
anything beyond the device name, and it cannot make the other person add you.
It is a signpost on a screen that had none.

## 17. Nothing ever said a new version existed

In-app auto-upgrade is compiled out of this fork, and has to be: Syncthing
verifies downloads against upstream's release signing key, which cannot
validate our builds. Leaving it on would either fail or quietly replace this
build with stock Syncthing.

Installing a newer version over an older one works, and is verified -- the Inno
`AppId` is fixed, so it is recognised as an upgrade; the installer stops the
tray and the daemon first so nothing is locked; `key.pem`, `cert.pem` and
`config.xml` survive; and the seed script no-ops unless its `$SeedVersion` has
been bumped. Even the *Start automatically when I sign in* choice is restored,
because Inno records the selected tasks in the uninstall key.

The gap was never the mechanism. It was that **nothing anywhere told anybody a
newer version existed**, so an install ran the version it was given forever,
until a human was handed a new installer and watched running it.

**What the fork does about it**

The tray compares itself to the machines it is already talking to.

Syncthing's Hello message carries the client version, `lib/model` puts it in
`ConnectionStats`, and `/rest/system/connections` has served it all along --
upstream's own GUI shows it in the device detail table. So when a connected
device is running a newer build of this fork, the tray says so and the toast
opens the releases page.

The obvious alternative was polling GitHub's releases API. It was rejected:
the README's central claim is that this build contacts nobody, and a daily
request to `api.github.com` carrying the user's IP would be a footnote on that
claim forever -- for information that was already on the wire.

Some deliberate decisions:

- **A peer on stock Syncthing can never trigger it.** Upstream's release train
  runs ahead of whatever base version this fork sits on, so a modeller
  connected to somebody on plain Syncthing 2.3.0 would otherwise be told
  forever to update to something that does not exist. Only versions carrying
  this fork's own `-desuq.N` suffix are compared at all.
- **A build made between tags is never nagged.** That is the developer's own
  machine, they know what they are running, and `git describe`'s suffix sorts
  oddly against the tags by design.
- **One toast per version, with a day's cooldown.** Every reconnect re-checks;
  a version that was newer an hour ago is not more true for being repeated.
  A *different*, newer version still gets through inside the cooldown.
- **Nothing is fetched.** The releases URL is the only external address in the
  tray and it is only ever handed to a browser by somebody clicking the toast.

**What it does not do.** It cannot fire before a peer connects, so a machine
sitting alone stays quiet -- which for this team is right, because a machine
with no peers has nothing to be out of date *for*. It also fires in the order
this actually happens: the person who cuts the releases upgrades first, and the
other two find out from their own machines rather than from a message they have
to be online to read.

The real alternative, if this ever stops being enough, is the one previously
ruled out of scope: our own signing key, signing each release, and replacing
`SigningKey` in `lib/upgrade`. That is a feature with a private key to guard,
not a config change.

## 18. The screen you land on was built for somebody else

Upstream's main screen is a table of every folder and every device, with
thirty-one distinct actions on it — override, revert, delete encrypted items,
restore versions, rescan, pause all, four separate "which files" listings, and
the connection diagnostics. It is a good screen for somebody administering a
sync network. It is the wrong screen for somebody who wants to know whether
their afternoon's work is safe.

The three questions this team actually has are: **is everything here, who am I
sharing with, and is anything wrong.** The default view now answers those and
nothing else.

- **One sentence at the top**, and it is the answer rather than a status code:
  *"Everything is here"*, or what is wrong, or what is still arriving.
- **Folders as cards** — how many files, how big, what is still coming, who has
  it, and how far each of them has got. A part-filled ring is somebody catching
  up; a dashed one is somebody who never accepted the share, which looks
  identical to 0 % if you only read the number and is the one of the two that
  never fixes itself.
- **People as cards** — online or not, what they actually hold, and whether you
  have verified them (§9).
- **Open folder** on every folder card. The path used to be text you selected
  and pasted into an Explorer window: a browser cannot open a file manager,
  because a `file://` link from an `http://` page is blocked outright. It takes
  a folder *ID* rather than a path, and what reaches `explorer.exe` is always a
  directory — Explorer handed an executable runs it.

### The bug class this kept producing

Four separate bugs during the build had one cause: **a sentence derived from
local state alone, presented as the shared reality.** In each case the local
figures were correct and the sentence was false.

| The screen said | What was true |
| --- | --- |
| "Everything is here, in step with Yuki" | Yuki had never accepted the folder |
| "Up to date" | The peer was behind; only `/rest/db/completion` knows |
| "1 file · has the same files as you" | 22 files were held back by the picker, so nothing was "needed" and the folder read as idle |

The rule that came out of it, worth applying to any copy added here: **ask what
the sentence claims about the other machine, and whether you actually asked
that machine.** Per-device truth lives in
`/rest/db/completion?folder=&device=`, and `remoteState` is the only field that
separates "behind" from "never accepted".

### Upstream's screen is still there

It is collapsed under **Technical details**, not deleted, and that is
deliberate rather than unfinished. Of its thirty-one actions, the audit found
seven covered by the new screen, four one click deeper, two dead — and eighteen
with no entry point anywhere. Three of those eighteen were capabilities rather
than shortcuts, and **all three now have a home**:

| Was homeless | Now |
| --- | --- |
| `restoreVersions.show` — restoring an earlier version of a file, which matters especially here because §4's staggered versioning is seeded **on** for every folder | **History → Older versions** (§19), reached from every folder card |
| `revertOverrideConfirmationModal('revert', …)` — the only way out of a wedged receive-only folder, the mode recommended for both modellers in the table below | **Undo my changes here**, on the folder card, shown only for a receive-only folder that actually has local changes |
| `showFailed` — *which* files failed, as opposed to how many | **N that would not sync**, on the folder card, shown only when the count is non-zero |

All three call upstream's own handlers. Every modal is `<ng-include>`d
*outside* the collapsed region, so these are entry points rather than
reimplementations — which is what made each of them cost one button.

That does not finish the job. Fifteen shortcuts still have no second door, and
two of them are the ones to be careful with: `showListenerStatus` and
`showDiscoveryStatus` have no replacement planned, and after the region goes
the log viewer is all that is left. Deleting upstream's markup remains a pass
of its own; it is now a smaller one.

## 19. Two promises the interface was only half keeping

Both of these were the same shape of mistake: a sentence that was true, next to
a sentence that should have been there and was not.

### "Choose files" was never about disk space

Un-ticking something in the picker writes an exclusion into the folder's
managed ignore block. Syncthing then stops keeping that thing up to date. It
does **not** delete what is already on the disk, and for a while the interface
said neither one thing nor the other — a modeller who un-ticked three gigabytes
of reference scans to make room watched their free space not move, with nothing
anywhere explaining why.

Those are two separate actions and the fork now offers both. Un-ticking stops
updates. **Free it up**, on the folder card, deletes the local copies — and it
is offered whenever there is something to reclaim, not only in the moment after
a pick, because the bytes outlive the moment.

It is the only thing this build does that deletes your files, so what it
refuses to touch is the interesting part. Four rails, all re-checked
server-side per file at the instant of deletion rather than trusted from the
browser:

1. The file matches the ignore patterns **currently loaded** for the folder,
   re-read at that moment. A pattern the screen thought was in force but is not
   can never reach a live file.
2. The global index still has it, undeleted.
3. A device **connected right now** has the current version. This is the rail
   that makes it reversible: ticking the item again pulls the file back. "Kai
   had it last week" is not good enough, and the confirmation names who it is
   relying on for exactly that reason.
4. The copy on disk matches that version in size and modification time. If you
   edited it offline and then un-ticked it, your edit is not their file and is
   not deleted.

Anything failing a rail is **named with its reason**, never silently skipped:
*"Deleted 3,178 files, 3.0 GB freed. 2 files were kept — RefPhotos/notes.txt,
nobody else connected has this copy."*

The residual risk is worth stating plainly, because no rail removes it: if
Kai later deletes the folder too and you never re-ticked it, it is gone from
both machines. That is inherent in reclaiming space. What the rails guarantee
is narrower and more useful — you can never lose something that existed *only*
on your disk.

**One implementation note that is easy to get wrong.** Rail 4 stats the disk
rather than reading the local index, because *marking a file ignored blanks its
size in the index*: `protocol.FileInfo.SetIgnored` → `setLocalFlags` →
`setNoContent` sets `Size` to 0 and drops the block hashes. An ignored file
therefore reports zero bytes locally, and comparing against that number would
have made every ignored file look identical to every other. The global entry is
authoritative and is not blanked. This is the same class of trap as
`/rest/db/browse?dirsonly=1` reporting every directory as zero bytes — local
state that looks like an answer and is not — and it is the fourth instance this
fork has hit.

### Thirty days of history nobody could read

§4 seeds staggered versioning **on**, thirty days, for every folder this build
creates. That is right for two people overwriting each other's `.blend` files.
But until now the only door to the archive it writes was inside upstream's
collapsed region, so the fork was diligently keeping old copies that nobody
could reach — versioning as a disk-space leak with good intentions.

**History**, on every folder card, has two tabs.

**Recent changes** is who touched what, lately. Read the empty state carefully:
it says *"nothing has changed since this computer started"*, and it means it.
That feed comes from `/rest/events/disk`, which is a memory-only buffer that
begins again at every restart. Three things it cannot do, which the wording is
built around:

- It never says a file was **added**. `action` is only ever `modified` or
  `deleted` (`lib/model/folder.go`), so a file that was just created arrives as
  modified and nothing in the feed can tell them apart.
- It is not a complete record, and says so in its own footer. The first scan of
  a folder emits one event per existing file, which on a real asset folder
  overruns the thousand-deep buffer by itself.
- It shows nothing for **ignored** files at all — `emitDiskChangeEvents` skips
  invalid entries — so a folder that has been through the picker is quieter
  here than it is on disk.

**Older versions** is the archive, and it is the one that survives a restart.
Files are listed newest-first with a count of copies kept and what they cost on
disk; expanding one lists its copies with a **Restore** beside each. A file
that has been deleted outright is tagged, and its button says **Put it back**,
because undeleting is not the same act as rolling back and one word for both
hides that.

Restoring is safer than it sounds and the screen says so: Syncthing archives
the current file *before* overwriting it, so the thing you were about to lose
becomes the newest entry in that same list. An accidental restore is undone by
restoring again.

**Why there is a fork endpoint behind it.** Upstream's `/rest/folder/versions`
returns every version of every file in one document. Staggered versioning at
thirty days keeps roughly fifty copies per file; five thousand files is a
quarter of a million entries and tens of megabytes of JSON, to paint twenty
rows. `/rest/folder/history` (`lib/api/api_history.go`) is the same data
summarised server-side, and the per-file list is fetched only when a row is
expanded. Restoring still goes through upstream's `POST`, which was always the
right shape.

## 20. Windows Defender may quarantine the tray on install

Seen for real on 2026-08-26, installing v2.1.4-desuq.4:

```
Trojan:Win32/Bearfoos.A!ml      ThreatID 2147731250
  %LOCALAPPDATA%\Programs\desuq-syncthing\desuq-syncthing-tray.exe
  the setup .exe
  Start Menu\Programs\desuqcafe Syncthing\desuqcafe Syncthing.lnk
  Start Menu\Programs\Startup\desuqcafe Syncthing.lnk
```

**It is a false positive**, and the `!ml` suffix says so fairly plainly: that is
Defender's machine-learning classifier guessing from behaviour, not a signature
match. `Bearfoos.A!ml` is one of its catch-all buckets and is well known for
eating unsigned Go binaries.

**But the consequences are not cosmetic.** Both shortcuts go with it, so there
is no Start Menu entry and **no start-at-sign-in** — Syncthing does not come
back after a reboot. Losing the tray also loses the desktop notifications
(§11), the Explorer folder icons (§10) and the newer-peer update toast (§17).
A modeller whose tray was eaten has a machine that silently stops syncing the
next time they restart it, which is the worst failure mode this project has.

### Why it fires

Nothing in the tray is hostile; it is a pile of individually-boring things that
together match a dropper profile. From an audit of `custom/tray/`:

| Behaviour | Why a classifier dislikes it |
| --- | --- |
| spawns a hidden, long-lived child and restarts it | `CREATE_NO_WINDOW` + `HideWindow` + supervision |
| `HKCU\Software\Classes\AppUserModelId\…` | looks like registry persistence |
| a named mutex for single-instance | common malware idiom |
| writes `desktop.ini` and sets system/read-only attributes | modifies user directories |
| `InsecureSkipVerify: true` | certificate validation off |
| unsigned, installs to `%LOCALAPPDATA%`, runs immediately | no publisher, not Program Files |

Every one has a reason: the registry key is the documented way to give a toast
a display name, `InsecureSkipVerify` is for Syncthing's self-signed loopback
certificate, the attributes are what make the folder icons work at all (§10).

The **network surface is one file**, `custom/tray/client.go`, and it points at
the GUI address from your own `config.xml`, forced to `127.0.0.1` when that is
bound to `0.0.0.0`. There is no hardcoded remote host anywhere in the tray.
The only external URL in the whole program is the releases page in
`update.go`, which is a string handed to a browser when somebody clicks a
toast — nothing fetches it.

One line *was* worth removing on its own merits and has been: `openURL` used to
call `rundll32 url.dll,FileProtocolHandler`, which is a textbook LOLBin —
launching things through `rundll32` is a documented evasion technique because
it hides the real parent process. It is `ShellExecute` now
(`custom/tray/platform_windows.go`).

**It did not help.** desuq.5 shipped with the change and was quarantined on
install exactly as desuq.4 had been — same `ThreatID 2147731250`, same four
files, within seconds of the installer finishing. So `rundll32` was not what
the classifier objected to.

**desuq.6 was not caught, and nobody should read that as a fix.** That build
replaced the tray's `InsecureSkipVerify: true` with a real certificate pin
(§21) — removing a bypass-TLS-verification pattern that behavioural scanners
genuinely do weigh. It installed, ran, and was left alone. But three other
explanations fit the same evidence just as well: Defender's cloud reputation
catching up after seeing the file repeatedly, a definition update landing
between the two builds, or the classifier's score simply sitting near a
threshold and falling the other side of it.

**Two of three builds were quarantined. Treat the detection as live.** Expect
it on any machine that has not seen this software before, and keep the
recovery steps below to hand.

### What was tried, and what is left

| Tried | Outcome |
| --- | --- |
| Remove the `rundll32` LOLBin (desuq.5) | No effect — quarantined identically |
| Pin the GUI certificate, dropping `InsecureSkipVerify` (desuq.6) | Not caught. Unattributable — see above |

Neither change is a fix, and reshaping the code until a classifier stops
objecting is not the goal. The durable answers are the ones below: report the
false positive to Microsoft, and sign the binaries.

### What to actually do

1. **Restore it**, per machine: *Windows Security → Virus & threat protection →
   Protection history → Restore* (or *Allow on device*), for this detection
   only. Do not exclude the install folder from scanning: it is writable by
   anything running as you.
2. **Report the false positive to Microsoft**, at their submission portal. Free,
   and it is the only route that fixes it for everyone rather than per machine.
3. Afterwards, check the tray is running and that *Start at sign-in* survived —
   the shortcut is what implements it, and it is one of the things quarantined.

Step 1 is not something a non-technical modeller will do over the phone. That
is the real cost, and it is what a **code-signing certificate** buys: a signed
binary from a consistent publisher is far less likely to be classified this way
at all, and it is the only durable answer wherever the installer is run by
somebody who will not go looking in Protection history.

### The product now notices

The worst part of the failure was that it was silent: a machine that syncs
until the next restart and then never again, with nothing on screen to say so.
Two things now say so, both in plain words and both ending at "open Windows
Security and choose Restore". Neither restores anything or touches Defender;
that stays the person's decision, made in Defender's own window.

- **The installer checks before it exits.** After the "Start now" entry has
  launched the tray, which is when a behavioural classifier looks, setup
  hides its window and watches the tray file for up to twenty seconds. If it
  goes, an error box says what happened and what it will cost, and offers to
  open Windows Security. Silent installs skip the wait. Verified on an
  isolated install with Defender simulated by killing and deleting the tray:
  the check starts 0.1 s after the tray is launched and fires 0.4 s after the
  file goes.
- **The main screen says so, for as long as it is true.** The daemon, which
  keeps running when the tray is taken, answers `GET /rest/system/tray`, and
  a missing tray is the headline, a red banner that outranks everything
  except a system error. This covers the case the installer cannot: a
  definition update that eats the tray a week later.

What neither can do is help a machine that has **already** restarted: then
nothing is running to notice. The installer check is there so that the
restart never comes first.

## 21. The tray now checks who it is talking to

Only relevant if you switch the web interface to HTTPS — not the default, and
not what this build seeds. Left here because the reasoning generalises.

The tray used to accept **any** certificate presented on the GUI port
(`InsecureSkipVerify: true`), justified in a comment two ways: the certificate
is self-signed and regenerated per install so "there is nothing to pin it
against", and loopback has no meaningful attacker in the path.

The first was simply false. `https-cert.pem` sits in the same directory as the
`config.xml` the tray already reads, so there is exactly one certificate it
should ever accept and it knows where to find it. The second is weaker than it
sounds: loopback is not private on a multi-user machine, and "anything that
answers on this port is trusted" hands over the API key — which is complete
control of every folder Syncthing manages.

**Why it needs a pin rather than ordinary verification.** Syncthing generates
that certificate with the device name as its common name and its only DNS SAN
(`CN=<device name>, DNS:<device name>`) and **no IP SAN**.
So `https://127.0.0.1:8384` can never satisfy hostname verification, whatever
is in the trust store — which is presumably how the bypass got there in the
first place. `custom/tray/tlspin.go` supplies both halves from the certificate
itself: a one-entry root pool, and `ServerName` read back off the certificate.
Full verification, exactly one acceptable certificate.

The tests are written to make it *refuse*. Connecting successfully proves
nothing — `InsecureSkipVerify` does that too — so what is asserted is that a
different certificate carrying the same name is rejected, and that a missing
or unparseable file is an error rather than a quiet fallback to trusting
anything.

## 22. Two people edited the same file, and the product stopped there

This is the normal week for the team this build is for, not an edge case.

Syncthing handles it correctly: nothing is lost, and the copy that loses the
race is renamed rather than overwritten:

    scene.sync-conflict-20260824-032916-F67Q3OS.blend

Then it stops. The tray raised a toast naming `scene.blend` (§8), and after
that the product had nothing more to say. What is left is two files, one of
them with a name nobody will type, no way to tell which is which, and a
well-founded suspicion that deleting either one loses somebody's afternoon. So
nobody deletes anything, and a year later the asset folder has four hundred
conflict copies in it.

A `.blend` cannot be merged. The only honest resolution is a choice between two
whole files, which makes the screen's real job **showing which is which**.

**The Conflicts tab** (History → Conflicts, and a row on the folder card while
there is anything in it) puts the two copies side by side: size, when each was
written, who wrote it, and — for an image — the picture. Two buttons, *Keep
this one* and *Use this one instead*.

**Neither button deletes anything.** Keeping the copy in use archives the other
into version history; adopting the copy set aside archives the one it replaces
first. Both choices are one click from being undone under *Older versions*,
and the screen says so. A folder with versioning switched off cannot make that
promise, so there the server **refuses** the operation and the screen turns
into a question naming what would be lost for good — you can still say yes.

Resolving on one machine is enough. The rename is an ordinary local change: the
other side sees the conflict copy deleted and the file updated, and its own
Conflicts tab empties on its own. Verified on a live pair.

### The device ID in the name is not who wrote that copy

Reading it as "Kai's copy" gets the attribution exactly backwards, and this
screen exists to answer "which of these two is mine".

`lib/model/folder_sendrecv.go` moves the **local** file aside and tags it with
`file.ModifiedBy` — where `file` is the **incoming** version, the one that
won. So the bytes inside the conflict copy are the *losing* edit and the ID in
its name belongs to whoever made the *winning* one. Confirmed on the pair: A and
B each edited `texture1.png`, B won, and both machines ended up with
`texture1.sync-conflict-…-V7OXBJ3.png` — B's ID on a file containing A's work.

Both authors on the screen therefore come from the **index**
(`FileInfo.ModifiedBy`, which for a conflict copy is the device that set it
aside), and the filename is read only for *when*. See
`lib/api/api_conflicts.go`.

This is the fifth instance of the pattern §18 describes: a value that looks
like an answer and is about something else.

### Pictures, because a timestamp is not an answer

The same thumbnails appear under *Older versions*. Staggered versioning at
thirty days keeps roughly fifty copies of a texture that gets saved twice an
afternoon, and the screen used to offer them as fifty timestamps. Nobody knows
what they did at 14:22 on the 3rd, and the only way to find out was to restore
a copy and look — which overwrites the file you were trying to protect.

`/preview/` decodes the image server-side and box-samples it down, so a strip
of twenty thumbnails costs a few kilobytes each rather than twenty 4K textures.
PNG, JPEG and GIF only: refusing everything else also keeps it from being a
"read any file in any folder" route. A `.blend` has its own reader, for the
preview Blender saves inside the file (§28).

**It is the one fork endpoint not under `/rest/`.** A thumbnail is an `<img>`
source, an `<img>` cannot send a header, and everything under `/rest/` is
behind the CSRF middleware. Upstream's QR-code image sits outside that prefix
for exactly the same reason, and this sits beside it — still behind the
authentication middleware, because a session cookie *is* sent by an `<img>`.

Also: a conflict copy that has been resolved keeps its generated name in the
archive, so *Older versions* lists the row as `texture4.png` with a **copy set
aside** tag rather than under forty characters of timestamp and device ID.

## 23. A folder that stopped, and a button that would have destroyed the library

A folder whose directory is not there stops, says *folder path missing*, and
waits. In practice that can last for days, behind a card that said
**Stopped** and offered nothing to press.

There are three ways out and the card now names all three: plug the drive back
in (and it clears on its own), point the folder at where it lives now under
*Settings*, or make the directory again.

**The third one is dangerous in a way that looks like success**, which is why
the server holds the rail rather than the button. Syncthing refuses to run a
folder with no marker precisely because an empty directory where a full one
used to be is indistinguishable from "the user deleted everything" — and a
send-receive folder that scans an empty root announces every one of those
deletions to everybody else. An unplugged drive becomes the whole team losing
the asset library, at the speed of a LAN.

So `POST /rest/folder/repair` will not create a directory for a folder whose
index still holds files, and the refusal names the count:

> This folder is supposed to hold 3 files, and the directory is empty. Setting
> it up again from here would tell everybody else that you deleted them, and
> they would delete their copies too. Plug the drive back in, or use Settings
> to point this folder at where it lives now.

What it *will* do:

| Situation | Answer |
| --- | --- |
| Directory gone, nothing ever in it | Creates it. The common case: a folder added but never populated |
| Directory gone, files in the index | **Refused**, naming the count |
| Marker gone, files still on disk | Puts the marker back. Somebody's cleaner ate a dot-directory |
| Marker gone, directory empty, files in the index | **Refused**. Same disk letter, different disk |
| Nothing wrong | Says so |

All four verified against a live instance — the third and fourth by accident,
when a delete of the folder root removed its contents and then failed on the
root itself because Syncthing held the directory open. That is exactly the
remounted-empty-drive shape, and the rail caught it.

## 24. Two more ways syncing stops without anybody noticing

The failure this fork keeps finding is not a sync that goes wrong; it is a sync
that quietly stops. §20 is one instance — Defender quarantining the tray takes
both shortcuts and start-at-sign-in with it, so the machine stops syncing at
its next reboot and nothing says so. **The machine it happens to is by
definition the one that cannot warn anybody.** The other side can.

**"Kai has not synced for four days."** The tray checks, slowly, whether any
peer has been silent for more than three days — three rather than one, so a
normal weekend never produces a toast. It cannot tell whose fault it is, so it
says three different things depending on what the evidence supports:

| Evidence | What it says |
| --- | --- |
| Others connected, one is not | Names them; it really is their computer |
| Nothing connected, one peer | "You and Kai have not synced for 4 days" — blames nobody |
| Nothing connected, several peers | "…usually this computer that is offline" |

A device that has never connected at all is excluded: that is the setup story,
and the first-run guide owns it. Worth knowing if you ever touch this code —
**`/rest/stats/device` reports a never-connected device's `lastSeen` as the
Unix epoch, not as a zero time**, so `IsZero` is false for it and the naive
version reports a brand new peer as twenty thousand days silent. Found by the
live test, not by reasoning; a hand-written fixture would have had the zero
time in it.

**A pause that ends by itself.** The tray's *Pause Syncing* checkbox pauses
until somebody unpauses it, and the reason people press it is never
open-ended — a render is going, or a big import, and they want the disk to
themselves for a while. Then they forget, because a paused Syncthing looks
exactly like a working one from inside Blender. *Pause for a while → 1 hour /
4 hours* pauses and then starts again on its own, says when in the menu while
it is holding, and toasts when it lifts.

The timer lives in the tray process and nowhere else, which is deliberate:
quitting the tray lifts the hold on the way out, so a timed pause can never
outlive the thing that promised to end it. The indefinite checkbox is still
there and still honest about being indefinite.

## 25. "I'm working on this file"

§22 made a conflict recoverable. This makes one avoidable. Two modellers
opening the same `.blend` is the one loss of work this setup produces by
design: Syncthing cannot merge a binary, so it keeps both copies and renames
one aside, and by then two afternoons have diverged. What would have prevented
it is a sentence said before anybody opened the file.

### How it is used

In Explorer, right-click the file → **Send to** → **desuqcafe Syncthing - I'm
working on this**. A toast confirms it. Everybody who shares that folder:

- gets a toast: *Yuki is working on cabin.blend*;
- sees it on the folder card: *Yuki is working on Scenes/cabin.blend · since
  14:20*;
- and, if the file changes on their computer anyway, gets a second, louder
  toast saying so while there is still time to pick up the phone.

Sending the same file again takes the mark off, and so does **Done** on the
card. A mark older than three days is dimmed and says it may have been
forgotten; it is never removed on its own, because a file still open after a
long weekend is still somebody's.

**Nothing is locked.** A marked file can be opened, saved and synced by anyone.
This is a note to the others, not a lock, and it says so. §26 has why it
cannot be a lock, tried on a pair.

**A mark says when it has not arrived.** It is a file in the folder, and a
file does not reach a computer that is switched off. Until §26 a mark made
while Kai was offline toasted *Everyone you share it with can see that
now* and reached nobody. Now the toast says *Kai is offline and will not
see it until they reconnect*, and the card carries the same line in amber
until Kai's own index has the file.

**Saving a `.blend` marks it.** The tray makes the mark itself the first
time a `.blend` is saved, says so in a toast, and takes it off after four
hours without another save (§26). Also from Explorer: right-click a
`.blend` → **desuqcafe Syncthing** → **I'm working on this** (§27).

### How it works, and why that way

Each device writes one file, `.desuq-claims/<its device ID>.json`, in each
folder it marks something in, and nothing else writes it
(`lib/api/api_claims.go`). One writer per file means the claims can never
conflict among themselves, which in the feature that exists to prevent
conflicts would be a poor joke. They are ordinary files in the folder, so they
reach exactly the people the folder is shared with, over the connection that
already exists: no new protocol, no server, nothing to configure. When a
device has nothing marked, its file is deleted rather than left empty.

**Who wrote a claim comes from the index, never from the file name.** Anybody
sharing the folder can create any file in it, including one named after
somebody else. A claims file is believed only when the global index's
`ModifiedBy` is the device it is named for -- our own included, because a
forged "you" would otherwise be kept and re-sent the next time we mark
something. Verified on a pair: B wrote a file in A's name; neither side
believed it, and A's next mark replaced it rather than extending it.

It has to survive the rest of the fork:

| Where | What it would have done | What happens |
| --- | --- | --- |
| The picker's hold-back (`*`, or an allow-list's `/*`) | Ignored the claims with everything else | `!/.desuq-claims` above the block, written by the picker and added by the tray to folders held back before this existed. Verified: a held-back peer receives the claim and nothing else |
| The picker's tree | Offered a box that does nothing | Hidden |
| File counts | "1 of 1 files chosen" about a folder where nothing was picked | Subtracted on the card; the server reports how many claims files each folder holds |
| History, Conflicts, the activity feed | Listed every mark and unmark as somebody's work | Left out |
| Explorer | A stray dot-folder in the modellers' asset folder | Hidden, on both sides: attributes do not sync, so the tray hides it where claims arrive |
| A receive-only copy | A mark that never leaves the machine | Not offered; marks from others still arrive and show |
| Notes (§31) | A second bookkeeping directory, and a new row in every line above | Kept in this directory as `<device>.notes.json`, so every exemption above covers them too; the file count subtracts every file here, not only marks |

### Found on the way: a peer who keeps part of the folder "had the same files"

Looking at a pair where B had picked one texture out of six, A's screen said
*Yuki Laptop has the same files as you*. Completion reads 100% for B, because
an ignored file is not needed. The fourth instance of the local-state-lies
class (§18), and the first seen from the *sending* side. B's own index knows
the truth -- every file it ignores arrives flagged `FlagLocalRemoteInvalid` --
so `GET /rest/db/peerheldback` counts those, with sizes from the global entry
because the peer's entry has its size blanked. The card now says *Yuki keeps
only part of it -- 5 files are not on their computer (10 MiB)* and the headline
stops saying "has the same".

## 26. Two people editing while apart

One on a train, one with the machine asleep, either of them paused: every
conflict this setup produces starts with two computers apart. Measured on a
test pair (2026-09-26) before anything was built on it, by disconnecting the
pair, editing on both sides and reconnecting.

### What Syncthing does

| Both sides... | What happens | Seen as |
| --- | --- | --- |
| changed the same file | Conflict. The later edit keeps the name; the earlier one is renamed `name.sync-conflict-<when>-<ID>.ext` on both machines | A conflict (§22) |
| one deleted it, the other changed it **later** | The change wins. The file **comes back** on the machine that deleted it, silently: no copy, no conflict, no event that says so | Nothing at all, until now |
| one changed it, the other deleted it **later** | The deletion keeps the name. The change survives **only** as a `sync-conflict` copy; the original name is gone everywhere | A conflict with no original beside it |
| a receive-only copy changed or deleted something | It never leaves. When the file then changes elsewhere, the local edit is renamed aside as a conflict copy **on that machine only** | On the sender: "catching up", which it is not |
| a receive-only copy is switched to Send & Receive | **Everything it was holding is sent, deletions included.** Verified: a file deleted on the receive-only side was deleted for everybody the moment the type changed | Nothing |

Edit against delete is decided by *which happened later*, not by which kind
of change it was. The starting assumption was "the edit wins",
which is half of it; both outcomes were produced on the pair by changing
only the order.

### What the product said, and what it says now

| Moment | Before | Now |
| --- | --- | --- |
| Reconnecting with conflicts | *Sync complete -- up to date*, then the conflict toast up to five minutes later | The conflict toast, within seconds of the folder finishing (the walk runs when a sync completes, and "Sync complete" stands aside) |
| An offline edit, conflict found after a tray restart | **Never announced**: "new" was judged by the copy's mtime, which is the *edit's* time | New is judged by the timestamp in the copy's name, which is when the conflict happened |
| Delete kept the name, edit is the copy | *Both versions were kept... a second copy* of a file that no longer exists | *texture3.png was deleted on one computer and changed on another. The deletion kept the name; the changes were kept as a copy... Rename it back if the file was still needed* |
| A deleted file came back | Nothing | *texture2.png came back -- You deleted it, but Yuki had changed it while you were apart, and the later change wins.* The tray remembers local deletions in `tray-deleted.json` beside the config, so a restart in between does not lose it |
| A peer changed files in a receive-only copy | *Sending 2 MiB to Yuki. Theirs is catching up* | *Yuki changed 2 files on their own computer. Their copy only receives, so those changes stay with them* -- from their index, where those entries are invalid but keep their size, unlike ignored ones |
| This computer has receive-only changes | *Nobody else can see these* | The same, plus: do not switch the folder to Send & Receive to share them -- that sends every one, deletions included |

### "Back in touch with Kai"

When a peer connects after being away -- ten minutes or more by a disconnect
the tray saw, or the first connection after the tray starts, which is signing
in in the morning -- the tray waits for the shared folders to settle and says
in **one** toast what happened: what they changed and deleted, what you both
changed, and what came back. Quiet if nothing did. The sync-complete, conflict
and came-back toasts hold off meanwhile, so a reunion is one notification,
not four. Verified on the pair across a tray restart:

> *texture6.png came back: you had deleted it, but they changed it while you
> were apart, and the later change wins. You both changed texture3.png and
> texture5.png while apart. Both versions were kept... They also changed
> cabin.blend and tree.blend, and deleted burst1.blend.*

Syncthing 2 opens several connections per peer and logs `DeviceConnected` for
each, while `DeviceDisconnected` fires only when the last one closes. The
first version briefed on the *second* connection of a two-minute blip -- after
the pull had finished, so it had nothing of theirs to report. The tray now
tracks who is connected, and for a connection it never saw start, asks the
link its age.

**Pausing from the tray while you hold marks** now says so: the others keep
seeing the mark, and taking it off while paused reaches nobody.

### Marks made by saving

The first save of a `.blend` marks it (`custom/tray/autoclaim.go`), from the
same disk-change feed the collision warning reads. The mark is flagged
automatic, the card says *marked when you saved it*, and the tray takes it off
once the file's modification time is four hours old, or the file is gone.
Marking the file by hand makes it a hand-made mark, which only a person takes
off. Not auto-marked: a file somebody else has marked (the collision warning
has already fired), conflict copies and `.blend1` backups, a folder shared
with nobody, and any folder reporting more than five `.blend` files in one
twenty-second pass -- that is a folder's first scan, which reports every file.
Verified: a six-file burst marked nothing.

**Opening a file is the better moment, and it is deliberately not used.**
Seeing it means inspecting Blender's process -- command lines or open handles
-- which is the behaviour a classifier dislikes, on a binary Defender already
quarantines (§20). Marking on open is what the optional Blender add-on does
instead (§33), from inside Blender.

### The soft lock, tried and dropped

The idea: when Kai marks a file, the tray sets it read-only on everybody
else's machine, so a save over it fails. Tried on the pair before building:
A (not the claimer) set `texture6.png` read-only and rescanned. **The bit was
sent.** A's index took a new version, B -- the person who made the mark --
received the read-only attribute, and B's own save then failed with *Access
denied*. The lock lands on exactly the wrong person. It could be kept local
with `ignorePerms` on every machine, but one machine without it -- such as a
peer still on an old build -- locks the claimer out of their own file. Not
built.

## 27. Explorer: right-click a .blend, and hover over the folder

**Right-click a `.blend` → desuqcafe Syncthing →**

- **I'm working on this (or done with it)** -- the same toggle as Send To.
- **Show history** -- opens the History screen on that file's older versions
  (`/?desuq-history=<folder>&file=<path>`; the GUI takes the parameters off the
  address once it has opened, so a reload does not reopen it).
- **Who has this?** -- a toast: who is working on it, whether each person has
  your version -- offline people included, from their index
  (`GET /rest/db/whohas`; upstream's `availability` lists only connected
  devices) -- and who changed it last. If *you* are the one behind, it says a
  newer version is on its way instead.
  It ends with the note on the current version, if there is one (§31).
- **Say why I changed this** -- opens the main screen with the note box on
  that file (`/?desuq-note=<folder>&file=<path>`, taken off the address like
  the history one). A browser, because a note is typed.

Written by the installer under
`HKCU\Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe` and
removed by the uninstaller: static verbs that run the tray with the file's
path, **no shell extension** -- the same reasoning as Send To, and as turning
down overlay icons and property handlers. `SystemFileAssociations`, so
Blender's own association is untouched and it does not matter whether Blender
is installed. **On Windows 11 they are under "Show more options"**, as Send To
is: the compact menu shows only a packaged app's `IExplorerCommand`, which is
a COM server in Explorer by another name.

Verified: a flat verb at that key is offered by the shell on a `.blend` and
not on a `.png`, and the command lines work when run. The *cascading*
menu itself is not enumerated by `Shell.Application`'s `Verbs()` -- neither is
Send To -- so it still wants one real right-click after installing.

**Hover over a synced folder** and the tooltip is live: *Project Assets --
synced by desuqcafe Syncthing. Kai is working on cabin.blend. You are
working on tree.blend. Up to date.* It is the `InfoTip` line of the
`desktop.ini` the tray already writes for the folder icon (§10), rewritten
only when the text changes, and within seconds of a mark arriving: the claims
pass pokes the folder marker. File names and labels go through the same
one-line filter as the label always did, because both are chosen by other
people and `desktop.ini` is obeyed by the shell.

## 28. Pictures of scenes, whether it arrived, and who is in the middle

Three things that all answer a question the modellers were asking out loud.

### A picture of every .blend

The History and Conflicts screens showed a thumbnail beside every texture
and a blank beside every scene -- the files that matter most. Blender writes
a small preview into every `.blend` it saves (the one its own file browser
shows), so `/preview/` now reads it out of the file header:
`lib/api/desuq_blendthumb.go`. It reads only the first few kilobytes, so a
600 MB scene costs the same as a small one, and `.blend1` backups work too.

Two things were not obvious:

- **Blender 5 compresses by default, with zstd.** Every 5.x file tested was
  compressed. The Go standard library has a zstd
  decoder but keeps it internal; rather than add a dependency to the root
  module (which conflicts on every upstream merge), the reader reaches the
  same decoder through `debug/elf`, which exposes it for compressed ELF
  sections. 4.x files are usually uncompressed; 2.x files may be gzip.
- **A file saved from a script has no preview.** `blender --background`
  writes none, and neither does a file saved with *Save Preview Images*
  turned off. The screen shows no picture rather than a broken one.

Verified on a pair, in Chrome: a live 4.5 file and an archived 5.x zstd
version of the same name, each with its own picture.

### "Sending cabin.blend to Kai" / "Kai does not have your latest"

The main screen said *"Kai is still catching up -- 12 MiB to go"* and
*"Sending 12 MiB to Kai"* about a computer that had been switched off
since Tuesday. Nothing was being sent. Completion is worked out from the
peer's stored index, and it reads the same whether they are here or not.

`GET /rest/db/delivery` walks each peer's remote need (the files whose
current version their index does not have) and splits it by who made that
version. The ones this computer made are *yours*. So the card now says:

| Their state | What the card says |
| --- | --- |
| Connected, missing some of yours | *Sending cabin.blend to Kai.* |
| Not connected, missing some of yours | *Kai does not have your latest cabin.blend yet -- their computer is not connected. It goes when they are back.* |
| Not connected, missing only other people's changes | *Kai is not connected. 3 MiB of changes will reach them when they are back.* |

A person who is behind and offline gets a plain outline instead of a
progress ring, because a ring frozen at 40% looks like a transfer that
stalled. The headline no longer says *Sending* about anybody who is offline.

Files somebody holds back on purpose (selective sync) are **not** counted as
undelivered. Remote need leaves ignored files out, which was checked on a
pair rather than assumed: completion is fooled by selective sync, this is
not.

The tray adds two things:

- **"Now on Kai's computer"** -- *cabin.blend and 2 more of your files
  have reached Kai.* Only for a delivery that **waited** at least two
  minutes: a big file over a slow link, or somebody coming back after being
  away. A save that arrives in seconds is the normal case, and a toast for
  every save would be noise.
- **Pausing warns about undelivered work.** *Kai does not have your latest
  cabin.blend yet. Pausing keeps it from reaching them until you resume.*
  This is folded into the existing pause-with-marks warning, so it is one
  toast.

### Three people, and one computer in the middle

The usual three-person setup: the person who made the folder shares it with
each of the other two. Syncthing then syncs A-B and A-C, **and never B-C**,
because neither was told about the other. Everything still reaches everyone
through A -- until A's computer is off. Then B and C stop syncing with each
other while both are online, and nothing anywhere says so.

Every peer announces who it shares each folder with, in the cluster config
sent when it connects. Upstream reads that list for introductions and then
drops it; `lib/model/desuq_hub.go` keeps it (one line in `model.go`, see
`CUSTOMIZATIONS.md`). `GET /rest/db/hub` then reports both ends:

- **On the computer in the middle:** *Kai and Mia only sync with each
  other through this computer. When it is off, their changes do not reach
  each other.* This is said only about two people who have both been heard
  from and both have the folder. Someone who has never connected has a
  different problem, and the card already says that one.
- **On a computer at the edge:** *You get Mia's changes only through
  Alex's computer. When it is off, the two of you do not sync.* This one
  has a **Connect directly** button. It adds Mia as a device (her ID and
  name come from Alex's announcement) and shares the folder with her. Mia
  is then asked to accept on her own computer, as for anybody new, and the
  verification card applies. The button refuses anybody who is not in that
  folder at one of your peers: it is not a general add-device route.

**Why not Syncthing's introducer setting**, which exists for exactly this?
It has to be turned on at the edges, about the middle computer. After that,
the middle computer's future choices of devices and folders become theirs
too, including removals. That is a permanent grant of trust. Connecting
directly is one person, one folder, one click, and it shows in the device
list afterwards.

Verified on a pair with a third device ID that never connects. The edge sees
the device and its route. Connecting to a stranger returns 404. Connecting
to the real one adds the device and shares the folder, and the notice then
goes away on both sides.

## 29. Pinning a version

Staggered versioning at thirty days is a good safety net and a bad archive.
The copy of a scene that the client signed off on was removed on day
thirty-one along with the forty autosaves around it. Every row under
*History → Older versions* now has a **Pin** toggle, and a pinned copy is
kept until somebody unpins it.

- **Every cleanup path checks for pins.** Simple and staggered versioning
  both remove through `cleanVersions`, on every save and on the periodic
  clean, and the trashcan has its own sweep. Both are guarded
  (`lib/versioner/desuq_pins.go`; the two upstream edits are in
  `CUSTOMIZATIONS.md`). The external versioner never cleans anything.
- **Restoring a pinned copy does not use it up.** Upstream *moves* the copy
  out of the archive on restore. A pinned copy that left the archive would
  lose its pin, because the next save re-archives it with a new timestamp
  and no pin. So a pinned copy is copied back and stays put. An unpinned one
  still moves, as upstream does.
- **Pins are this computer's.** The archive is not synced: each device fills
  its own `.stversions` when *it* replaces a file, stamped with *its* clock,
  so there is no shared identity for "Tuesday's version" to pin everywhere.
  The pins live in `desuq-pins.json` beside the database, not in the folder,
  and the History screen says so. A sidecar file inside `.stversions` was
  rejected: every walk in the versioner would see it, and the trashcan would
  list it as a version and then delete it at the cutoff.
- **A pin whose copy has gone is not tidied away.** Pruning would mean
  trusting one listing of the archive to be complete. One bad walk would then
  unpin a copy that is still there, and the next clean would delete it. A
  leftover entry costs a few bytes; the header counts only pins whose copy is
  still listed.
- **A pinned copy still counts toward the bytes on disk** shown in the
  History header. That header now says how many copies are exempt from the
  cleanup.

Verified on a pair with staggered versioning at thirty days and a ten-second
cleanup interval. Three copies older than thirty days were planted and two
were pinned. The next clean removed only the unpinned one. Restoring a pinned
copy put its bytes back in the folder and left it in the archive, still
pinned, through a further clean.

## 30. Handing a file over

"Kai, this one's yours now." Before this, passing a scene on meant taking
your mark off and hoping Kai put theirs on before anybody else opened it.
Now your own mark on the main screen has **Give to Kai** (or **Hand over**
and a list, when more than one person shares the folder). Kai's computer
picks it up by itself, marks it as Kai's, and the tray tells Kai: *Mia
handed you cabin.blend.* Your computer is told back: *Kai has taken
cabin.blend.* Until it is picked up, the mark stays yours and reads *You are
handing cabin.blend to Kai*, with **Keep it** to change your mind.

- **Still one writer per claims file.** Nobody writes another device's file,
  so a hand-over is three steps, each on the device that owns the file. The
  giver sets `to` on its own entry. The taker writes its own mark with
  `from`, plus an acknowledgement in an `accepted` list. The giver sees the
  acknowledgement and drops its entry. `lib/api/api_handoff.go`.
- **The file is never unmarked in between.** While it is in transit it is
  still the giver's mark, and third people see it as taken. Once the taker has
  it, the giver's leftover entry is hidden everywhere, not just on the
  giver's computer, because the acknowledgement says it has been taken.
- **The giver can switch off straight after handing over.** That is the
  normal case, not an edge case. The acknowledgement is kept in the taker's
  file after they press **Done**, until the giver's entry is gone. Without it,
  the taker's computer would see the old *to: Kai* and take the file again
  at every pass. Verified on a pair by never polling the giver: the taker
  took the file, pressed Done, and it did not come back. When the giver
  returned, its entry went, then the taker's acknowledgement, leaving no
  claims files in the folder at all.
- **Saving does not cancel a hand-over; marking by hand does.** The tray
  marks a `.blend` on every save. A save on the giver's side between handing
  over and the pick-up must not take it back, and it does not. Marking it by
  hand (**Keep it**) does.
- **A computer where the folder only receives cannot take a hand-over.** A
  mark made there reaches nobody, so acknowledging would hide the giver's mark
  and leave the file unmarked for everybody. The hand-over waits, and that
  computer's card says why.
- **It runs whenever claims are asked for**, which the tray does every twenty
  seconds and the main screen every ten. No new timer, and it keeps working
  with only one of the two running.
- **Older builds** ignore `to`, `from` and `accepted` as unknown fields. To
  them a mark in transit is simply the giver's mark, and a taken one is the
  taker's.

## 31. "Why I changed this"

History could already say *that* cabin.blend changed and who changed it. Now
the person who saved it can say *why*, in a sentence: *moved the camera,
lighting untouched*. Everybody the folder is shared with sees it:

- **On the main screen**, under the folder, for files as they are now: who,
  which file, when, and the words. Your own have **Edit** and **Remove**. A
  note usually arrives before the file it explains -- a few hundred bytes
  against a gigabyte -- and says *this version is still on its way to you*
  until it has.
- **In a notification** when somebody else's note arrives: *Kai changed
  cabin.blend -- "put the lighting back"*.
- **In History, beside the exact version it was written about**, for as long
  as that copy is kept -- which is the moment the question is actually asked:
  "which one still had the good lighting?" The note on the version in the
  folder now is shown above the older copies.
- **In *Who has this?*** on the right-click menu, as the last sentence.

**Writing one.** **Say why you changed a file** on the folder card lists your
own saves from the last two weeks that nobody has saved over since, and
starts on one without a note. Pressing **Done** on your own mark offers it
for that file, if you changed it and have not said why yet. Right-click a
`.blend` → **Say why I changed this** opens the same box on that file.

- **A note is about one version, not the file.** Stored with the save's
  modification time (to the second) and size, because those survive
  everywhere the version goes: the index, every peer's disk (the puller sets
  the mtime), and the archive, where a replaced copy is renamed into
  `.stversions` with its mtime intact. Verified on a pair: A's note stayed on
  A's copy in B's archive after B saved over it, and B's note became the
  current one. A note is never shown against a version it was not written
  about.
- **Only the person who saved a version can explain it.** Anybody else's note
  about your work would be a guess dressed as an explanation, so the server
  refuses (409) a note on a file whose latest version somebody else saved,
  and one on a file a newer version of which is still on its way to you. The
  author is the index's `ModifiedBy`, as everywhere else.
- **A save the scanner has not reached yet still counts.** Straight after
  pressing Ctrl+S the index still has the previous version. The disk is this
  computer's own work by definition, so the note is written against what is
  on disk and a scan of that one file is started. Verified: a note written
  before the scan was current at once, and still current after it.
- **Stored like the marks, and beside them**: `.desuq-claims/<device
  ID>.notes.json`, one writer per file, authorship from the index, a forged
  file ignored. **The same directory on purpose**: the picker's
  `!/.desuq-claims` line is already written into ignore files on machines in
  the field, and History, Conflicts, the activity feed and the tray's
  auto-mark all already leave that directory alone -- a `.desuq-notes/` would
  have been a new row in each, and held back on every machine that had used
  the picker until somebody reopened it. The main screen's file count now
  subtracts every file in that directory, not only the marks. Verified on a
  pair: B writing a notes file in A's name was believed by neither side, and
  A's next note replaced it rather than extending it. (A's earlier notes are
  lost when that happens, exactly as its marks would be.)
- **Bounds**: 500 characters a note, 200 notes per computer, the oldest going
  first, and nothing older than about thirteen months -- well past the
  archive, so a pinned copy (§29) keeps its reason. Removing your last note
  removes the file rather than leaving an empty list behind.
- **The tray remembers what it has announced**, on disk (`tray-notes.json`,
  beside `tray.log`), per author. A note is news whenever it arrives, and it
  typically arrives the morning after, when it is older than the tray -- so
  the marks' "newer than when the tray started" rule would have dropped
  exactly those. One high-water mark per author, so a machine with a slow
  clock cannot hide another's notes. A note about a version already replaced
  is recorded as seen without a toast. Verified: announced once, not again
  after a restart.
- **Toasts are cut by characters, not bytes** now. A note in Japanese reaches
  a multi-byte character within a dozen letters, and `truncate` used to cut
  it in half.
- **Older builds** do not know the file: to them it is one more file in a
  folder they already keep hidden and uncounted -- except the count, which
  they still include, as they would any unknown file there.

## 32. Who is around

Each folder card now has a line per person under the coloured initials:

*Kai — here now · working on cabin.blend · last saved tree.blend 12 min ago*
*Mia — last seen yesterday at 18:40 · last saved rock.blend on Tuesday*

The initials already said where each person's *copy* has got to. This says
whether the *person* is around and what they are doing -- the question behind
"should I phone before I open this?". An offline person's own card says
*Last seen yesterday at 18:40.* too, where it used to say only *Offline*.

- **Here** is the live connection, from the same poll as everything else on
  the screen. **Last seen** is `/rest/stats/device`, which reports a device
  that has never connected as 1970 rather than as nothing; the server filters
  that once (`lib/api/api_presence.go`) and the screen says *has never
  connected* -- but only after the server has answered, so it is never a guess.
- **Working on** is their marks (§25) in this folder.
- **Last saved** is the newest file in the folder whose current version they
  saved, from the index's `ModifiedBy` and the file's own modification time.
  From the index rather than `/rest/events/disk` because the event buffer
  starts empty whenever the daemon restarts, so on Monday it knows nothing
  about Friday; the index knows for good. The price is that it only knows
  current versions: a file Kai saved and Mia saved over since is Mia's, and
  a deletion is nobody's save. Nothing older than thirty days is said, and
  the server reads at most the 300 most recently modified files to find
  everybody -- someone whose newest save is older than all of those is not
  "around" in any useful sense.
- **Once a minute**, like the hub probe: one metadata walk per folder plus a
  few lookups.
- Verified on a pair: B saved a file and A's card named it within a scan; B
  shut down and A said *last seen* with the time; a device added and never
  connected read as never, not as fifty-six years ago.

## 33. The Blender add-on (optional)

Everything so far works from Explorer and the main screen. The add-on adds
the two things neither can do: it knows when a file is **opened**, and it can
ask why something changed where the change was made.

**When you open a `.blend` in a synced folder:**

- If somebody else has it marked, a popup says so -- *Kai is working on
  cabin.blend. If you both save it, one of you ends up with a copy of your
  own.* It does **not** mark the file over them.
- If a newer version is still on its way to this computer, it says to wait:
  otherwise you are editing an old copy, which ends as a conflict.
- If the version you opened has somebody's note (§31), it shows it.
- Otherwise it marks the file as yours, automatically, while you have it
  open. Close it **without saving** and the mark comes off again; close it
  after saving and the mark stays until the tray's quiet-hours rule takes it
  off -- the others should hear that it changed, not that it was let go of a
  second later. A mark made by hand is never taken off by the add-on. A
  preference turns marking-on-open off.

**File › desuqcafe Syncthing:** *I'm Working on This*, *I'm Done with This*,
*Say Why I Changed This…* (refused while there are unsaved changes: a note is
about a saved version, and the one on disk would be the previous save), and
*Who Has This?*.

- **Why an add-on, when the tray already marks on save?** The tray learns
  about a file only when it is written, and deliberately does not watch which
  programs are running -- process inspection is exactly the kind of code
  Defender flags (§20). Blender telling us is the sanctioned way.
- **It talks only to the Syncthing on this computer**, with the address and
  API key from its own `config.xml` in `%LOCALAPPDATA%\desuqcafe-syncthing`,
  exactly as the tray does -- including the certificate pin when the GUI is on
  HTTPS (the pinning path is written but not exercised: GUI TLS is off by
  default and not seeded on). Three-second timeouts, and a Syncthing that is
  not running is a line in Blender's console, not a popup on every open.
- **Installed by the modeller, not the installer.** The installer puts
  `desuq_syncthing.zip` in the program folder's `blender\`; putting it into
  Blender means writing Blender's own settings, per Blender version, and that
  is their choice. In Blender: *Edit › Preferences › Add-ons*, the arrow at the
  top right, *Install from Disk*. The main screen's **Working on a file?**
  panel gives the exact path (`/rest/system/tray` reports it). The zip is both
  an extension (Blender 4.2+, `blender_manifest.toml`) and a legacy add-on
  (`bl_info`). Verified: Blender 5.2's own `extension install-file` installed
  and enabled it in an isolated user directory, menu and operators
  registered.
- **Tested in a real Blender.** `custom/scripts/test-blender-live.ps1` runs it
  headless (`--factory-startup`, so nobody's preferences are read or changed)
  against a fresh pair: 30 checks, from the mark on open and its removal on
  close to the popup about somebody else's mark, the note operator, the
  refusal on a version still arriving, and -- checked after Blender has
  exited -- the mark coming off on quit. By hand, because CI has no Blender;
  `custom/scripts/test-blender-addon.py` checks the client's decisions with
  plain Python and is suite 13. Only Blender 5.2 has been run live, so that
  is the version covered.

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
| `device/@name` — the Windows user name, rather than upstream's host name, because `DESKTOP-A1B2C3` tells nobody which machine they are looking at. **Only when the existing name is one Syncthing picked**; see below | A device name somebody has chosen |

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

**The device name is the one seeded value that is not simply overwritten.**
Everything else in the table above is a default that a re-seed is *meant* to
reset. A device name is not: it is the label the other two people see in their
device list, and taking it back is both surprising and invisible from the
machine it happened on.

So the name is replaced only when the existing one is a name Syncthing picked
rather than a person. `generate` writes `os.Hostname()`
(`lib/config/config.go`), so the test is: empty, equal to this machine's host
name, or one of Windows' own out-of-box forms (`DESKTOP-`, `LAPTOP-`, `WIN-`
followed by the generated suffix). The pattern is anchored, so *Desktop
upstairs* is a name and stays. Passing `-DeviceName` explicitly is an
instruction rather than a default, and is applied whatever is already there.

`custom/scripts/test-seed-naming.ps1` runs all of that against the real script
and the real binary in a throwaway directory.

**Watch for this whenever `$SeedVersion` changes.** At 2, installing over an
existing machine re-seeds it -- and on a real install that renamed the device
from the machine's host name to the Windows user name. The rule worked
exactly as written: the device name still matched `os.Hostname()`, so it
counted as a name `generate` picked rather than one a person typed, and the
user name replaced it. But a host name can be chosen on purpose too, which is
the finding: a **deliberately meaningful host name gets treated as
auto-generated**, because the test cannot tell the two apart. If any of the
three machines has a host name somebody chose on purpose, set the device name
explicitly before shipping the upgrade:

```powershell
& "$env:LOCALAPPDATA\Programs\desuq-syncthing\seed-config.ps1" `
    -DataDir "$env:LOCALAPPDATA\desuqcafe-syncthing" `
    -Binary  "$env:LOCALAPPDATA\Programs\desuq-syncthing\desuq-syncthing.exe" `
    -DeviceName "Studio Workstation" -Force
```

`-DeviceName` is an instruction rather than a default, so it wins over the
heuristic and over anything already there. The other two people see whatever
this ends up as, and a rename is invisible from the machine it happened on.
