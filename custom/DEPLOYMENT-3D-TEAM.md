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

**Mitigations available today**

- Raise the default reserve. 1 % of a 500 GB drive is 5 GB — far too little when
  a single asset drop can be tens of GB. An absolute value is more predictable
  across differently-sized drives than a percentage.
- Before sharing, tell them the folder size. The receiving side *can* see it as
  "Global State" in the GUI, but only after accepting the share.

**If we want to fix it properly in the fork**, the smallest useful change is a
pre-flight check when accepting a pending folder: compare the offering device's
advertised folder size against `fs.Usage()` on the chosen path and warn. That
needs a real code change in `lib/api` plus the GUI accept dialog, so it is a
feature, not a config tweak — see the rule in `CUSTOMIZATIONS.md` before
starting.

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
