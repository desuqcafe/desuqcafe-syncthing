# Our own file browser: the options, not yet decided

Recorded 2026-09-26, after Wave B. **Nothing here is built.** The decision is
deferred; this is what it will be made from.

## The question

Should the fork have its own Explorer-like browser for synced folders, and
would that remove the Explorer-integration limits we keep running into?

The Explorer limits in question: overlay-icon slots are full; on Windows 11
our right-click verbs sit under *Show more options*; cascading menus cannot
be verified from a script; the `desktop.ini` hover text is cached; anything
running inside Explorer attracts Defender; the Cloud Files API (`cfapi`) was
rejected earlier.

## The main finding

A browser of our own does not fix those limits. It moves the status display
to a window the modellers have to choose to open. They open files through
Blender's *File › Open* and by double-clicking in Explorer, and a new app
changes neither. What it can do is show what Explorer never can:

- status on each file: synced, only here, on its way, conflict, marked by
  somebody, changed while you were apart;
- **files you have not downloaded** -- the global index lists them, and
  Explorer cannot show a file that is not on disk;
- thumbnails (`.blend` too, since Wave B), who has this file, version
  history, and resolving a conflict with both copies side by side.

The server side of almost all of that already exists: `/preview/`,
`/rest/db/whohas`, `/rest/db/delivery`, `/rest/folder/claims`,
`/rest/folder/history`, `/rest/folder/conflicts`.

File operations are where our own browser can beat Explorer, because it
knows what an action means for the other people. Delete can say "this also
removes it from Kai's computer" and archive the file first. Renaming a file
somebody has marked can warn before it causes a conflict copy. Syncthing
itself does the work, so this is the same whatever window the interface is
in.

## The options

| # | Option | Pros | Cons |
| --- | --- | --- | --- |
| 1 | **Files screen in the existing GUI** (browser; opened from the tray, the folder card, and a "Show in desuq" right-click) | No new exe, so no Defender risk and no install size. Reuses everything already built. Shows files not yet downloaded. File operations done safely by Syncthing: rename, move, new folder, delete with archive, dropping files in. About one wave of work. Nothing is thrown away if option 2 follows | A browser tab, not "an app". No dragging out into Blender or Explorer, no Explorer copy/paste, no *Open with*. A second place to look |
| 2 | **The same screen in its own window** (WebView2, pure Go, in a nested module) | Feels like an app. Adds drag-out, Explorer copy/paste, *Open with*, and dropping files in with real paths. WebView2 is already on Windows 11, so about 2-3 MB. Only adds to option 1 | A new unsigned exe making shell calls: the same Defender false-positive gamble as the tray (§20). Separate exe or folded into the tray: each has a cost. Native drag-and-drop and clipboard code to build and test |
| 3 | Native WinUI 3 / .NET app | The most native feel | 60-150 MB or a runtime to install. New toolchain and CI. Rewrites every screen. Same Defender and signing problem. Hardly any gain over 2 for three users |
| 4 | Explorer's own view embedded in our window, with a side panel | Every Explorer habit kept | Still cannot put status on files. Heavy COM from Go, the worst fit for Defender. Loads other programs' shell extensions into our process |
| 5 | Cloud Files API (how OneDrive does it) | Status in Explorer itself. Files not downloaded appear and download on open. Every habit kept, Blender's Open dialog included | Fights Syncthing's temp-file-then-rename writes, a change at the core. Fails **on the files**: stuck placeholders, files that look missing. Bigger than the whole fork so far. Needs a provider process always running |
| 6 | Tauri / Electron | App window around web code | A Rust toolchain, or 100 MB+. No advantage over 2 |
| 7 | Don't build it | No work | Explorer status stays limited to the hover text and the right-click menu. No way to see or get files not downloaded except the picker |

## Recommendation (not yet agreed)

**Option 1, including file operations; option 2 later if dragging out into
Blender turns out to be missed. Not 3, 4, 5 or 6.**

Option 1 is useful even if nothing follows it, and option 2 only adds to it.
Option 5 is the only way to get everything inside Explorer itself, and it
puts the files themselves at risk to get there.

## Smallest useful first version of option 1

1. `GET /rest/db/listing?folder=&prefix=`: one directory level from the
   global index, with each file's state on this computer, whether each peer
   has your latest, marks and conflicts. The comparison happens on the
   server, because working it out from local state alone has been wrong
   several times (see the local-state-lies entries in CLAUDE.md).
2. A grid of tiles with thumbnails and status badges, a breadcrumb, and a
   side panel with who has it, Mark, History, Open and Show in Explorer.
3. Ways in: a **Browse** button on the folder card, a tray menu item, and
   **Show in desuq** on the `.blend` right-click menu.
4. File operations done by Syncthing, each guarded by marks, peers and
   versioning.

**Open** must launch only known-safe types (`.blend`, images) and select
anything else in Explorer (`explorer /select`). A peer can sync you an
`.exe` or a `.lnk`, for the same reason `/rest/system/reveal` only ever
opens a directory.

Never: the full Explorer right-click menu (every shell extension loaded in
our process).
