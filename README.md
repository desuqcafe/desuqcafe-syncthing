# desuqcafe Syncthing

A fork of [Syncthing](https://github.com/syncthing/syncthing), packaged as a
**per-user Windows installer** for a small team sharing large binary assets —
`.blend` files, textures, renders.

The syncing is upstream's, unchanged: the same peer-to-peer protocol, the same
end-to-end encryption, the same no-server-in-the-middle design. What is
different is everything around it. It installs without an administrator, it
puts an icon in the notification area and tells you when something happens, it
lets you tick which files you want *before* any of them download, and it sends
nothing to anybody.

[![desuq tests](https://github.com/desuqcafe/desuqcafe-syncthing/actions/workflows/desuq-test.yaml/badge.svg)](https://github.com/desuqcafe/desuqcafe-syncthing/actions/workflows/desuq-test.yaml)
[![MPLv2 License](https://img.shields.io/badge/license-MPLv2-blue.svg?style=flat-square)](https://www.mozilla.org/MPL/2.0/)

---

## Install it (Windows 10/11, 64-bit)

1. Download **`desuq-syncthing-setup-*.exe`** from the
   [latest release](https://github.com/desuqcafe/desuqcafe-syncthing/releases/latest).
2. Run it. **No administrator rights are needed** — it installs into your own
   account and asks for nothing.
3. Windows will probably say *"Windows protected your PC"*. That is because the
   installer is not code-signed, not because anything is wrong with it. Click
   **More info**, then **Run anyway**. If you would rather check first, every
   release ships a `SHA256SUMS.txt` next to the installer.
4. Leave both tickboxes as they are and click through. It opens the web
   interface for you when it finishes.

That is the whole installation. There is no service to configure, no account to
create, and nothing to sign in to.

### What you get

- **A violet icon by the clock**, bottom-right. That icon *is* the app — it
  starts Syncthing, keeps it running, and is the only thing on screen that says
  whether syncing is working. Right-click it for **Open**, **Pause Syncing** and
  **Quit**. Quitting stops syncing until you sign in again.
- **It starts by itself when you sign in.** You should not have to think about
  it again.
- **Windows notifications** when something actually needs you: a new device
  wants to connect, someone has offered you a folder, a sync finished, a folder
  is in trouble, the disk is nearly full, a teammate is on a newer version.
  Nothing else — see
  [§8](custom/DEPLOYMENT-3D-TEAM.md#8-nothing-reached-the-user-unless-the-gui-was-open---now-it-does)
  for why the list is deliberately that short.
- **Synced folders look different in Explorer** — a violet folder with a sync
  ring, so you can tell at a glance which folder is the shared one.
- **Sensible defaults already set**: 30 days of file history, a 20 GB disk
  reserve, and Blender's `.blend1`/`.blend2` backups excluded so they do not
  churn across the network.

Everything lives in `%LOCALAPPDATA%\desuqcafe-syncthing`, which is why a stock
Syncthing can be installed alongside this one without the two interfering.

To remove it: **Settings → Apps → desuqcafe Syncthing → Uninstall**. Your files
are never touched, and your configuration is kept unless you tick the box that
says otherwise.

## What this fork adds

| | What | Detail |
| --- | --- | --- |
| **Installs like an app** | Per-user Windows installer. No administrator, no service, no command line. Starts at sign-in. | [`installer.iss`](custom/installer/installer.iss) |
| **You can see it running** | A notification-area icon with five states, which also supervises Syncthing and restarts nothing behind your back. Upstream has no tray icon and no service mode: started at sign-in it is completely invisible, and if it stops, nothing says so. | [§7](custom/DEPLOYMENT-3D-TEAM.md#7-nothing-showed-that-syncthing-was-running--now-the-tray-does) |
| **It tells you things** | Real Windows toasts for six events, every one rate-limited, each click-through to the page that can act on it. Upstream has a full event stream and no notifications of any kind. | [§8](custom/DEPLOYMENT-3D-TEAM.md#8-nothing-reached-the-user-unless-the-gui-was-open---now-it-does) |
| **It shows you what to do first** | A four-step setup guide on a fresh install: name this machine, send your code, check it is really them, choose what to sync. Every step is closeable and comes back where it left off. Upstream's first-run screen is an empty folder list, with the device code you need behind *Actions → Show ID*. | [§16](custom/DEPLOYMENT-3D-TEAM.md#16-the-first-ten-minutes-were-an-empty-screen) |
| **A screen that answers your questions** | The default view answers *is everything here, who am I sharing with, is anything wrong* — one headline sentence, folders and people as cards, and a button that opens the folder in Explorer. Upstream's own screen is a table carrying thirty-one actions, several destructive; it is still there, collapsed, under *Technical details*. | [§18](custom/DEPLOYMENT-3D-TEAM.md#18-the-screen-you-land-on-was-built-for-somebody-else) |
| **Two copies, side by side** | When two people change one file, Syncthing keeps both and renames the loser to something nobody will type. This puts the two copies next to each other — size, time, who wrote it, and for a texture the picture itself — and either choice archives the other rather than deleting it. Upstream leaves both files in the folder and says nothing more. | [§22](custom/DEPLOYMENT-3D-TEAM.md#22-two-people-edited-the-same-file-and-the-product-stopped-there) |
| **"I'm working on this"** | Right-click a file, *Send to → I'm working on this*, and everyone you share the folder with sees your name on it — and gets a warning if they change it too. Nothing is locked; it is the phone call nobody makes, made automatically. Upstream has no way to say it at all, so the first anybody hears is the conflict copy. | [§25](custom/DEPLOYMENT-3D-TEAM.md#25-im-working-on-this-file) |
| **Did it reach them?** | *Sending cabin.blend to Kai* while it goes, *Kai does not have your latest yet — not connected* while they are away, and a toast when a delivery that waited has arrived. Pausing warns about work that has not left yet. | [§28](custom/DEPLOYMENT-3D-TEAM.md#28-pictures-of-scenes-whether-it-arrived-and-who-is-in-the-middle) |
| **Nobody silently in the middle** | With three people, the two who were each shared with separately only sync *through* the third. The screen says so, on both ends, and the one at the edge gets *Connect directly*. | [§28](custom/DEPLOYMENT-3D-TEAM.md#28-pictures-of-scenes-whether-it-arrived-and-who-is-in-the-middle) |
| **Working apart** | When two computers meet again, one toast says what the other person changed and what you both changed. A file you deleted that came back because somebody edited it later is said out loud. | [§26](custom/DEPLOYMENT-3D-TEAM.md#26-two-people-editing-while-apart) |
| **Keep a version for good** | *Pin* any older copy in History and the thirty-day cleanup leaves it alone, even after you restore it. | [§29](custom/DEPLOYMENT-3D-TEAM.md#29-pinning-a-version) |
| **Hand a file over** | *Give to Kai* on your own mark: their computer takes it and tells them, and the file is never unmarked in between. | [§30](custom/DEPLOYMENT-3D-TEAM.md#30-handing-a-file-over) |
| **Why it changed** | A sentence on your own save — *moved the camera, lighting untouched* — shown to everyone on the main screen, in a notification when it arrives, and beside that exact version in History for as long as the copy is kept. | [§31](custom/DEPLOYMENT-3D-TEAM.md#31-why-i-changed-this) |
| **Who is around** | Each folder names who is here now or since when, what they have marked, and the last file they saved — from the index, so it survives a restart. | [§32](custom/DEPLOYMENT-3D-TEAM.md#32-who-is-around) |
| **Right-click a .blend** | *I'm working on this*, *Show history*, *Who has this?*, *Say why I changed this* — plain registry verbs, no shell extension. Hover a synced folder for who is working on what. | [§27](custom/DEPLOYMENT-3D-TEAM.md#27-explorer-right-click-a-blend-and-hover-over-the-folder) |
| **Old copies you can see** | Version history with thumbnails — `.blend` scenes included, read from the preview Blender saves inside the file — so picking one out of fifty timestamps is looking rather than guessing and restoring. Upstream's endpoint answers with every version of every file in one document. | [§22](custom/DEPLOYMENT-3D-TEAM.md#22-two-people-edited-the-same-file-and-the-product-stopped-there) |
| **A stopped folder tells you why** | *Folder path missing* used to be a card that said **Stopped** and offered nothing to press. Now it names all three ways out — and refuses to recreate an empty folder whose index still holds files, because that is how an unplugged drive becomes a deletion everybody receives. | [§23](custom/DEPLOYMENT-3D-TEAM.md#23-a-folder-that-stopped-and-a-button-that-would-have-destroyed-the-library) |
| **It notices when syncing stops** | A peer that has been silent for days gets a toast — from the other side, because the machine that stopped is the one that cannot warn anybody. And *Pause for an hour* really is an hour: the pause ends by itself. | [§24](custom/DEPLOYMENT-3D-TEAM.md#24-two-more-ways-syncing-stops-without-anybody-noticing) |
| **Choose what to sync** | A real file picker over the *global* tree. Accepting a share holds everything back, the index arrives, you tick what you want, and only then does any file data move. Upstream's answer is a textarea full of globs. | [§2](custom/DEPLOYMENT-3D-TEAM.md#2-selective-sync-existed-but-only-as-ignore-patterns--now-there-is-a-picker) |
| **Disk space you can see** | Free space shown under the folder path and in the folder detail, and a warning when a folder will not fit. Upstream checks capacity per file and never up front, so a 400 GB share can be accepted onto a 250 GB drive. | [§1](custom/DEPLOYMENT-3D-TEAM.md#1-disk-space-the-check-is-weaker-than-it-looks) |
| **Verified device handshake** | Both devices show the same collectible card — `《 CRIMSON TALISMAN NOCTURNE 》Rank CLXXVI` — derived from the two device IDs. Read it to each other on a call. Confirming means picking the right card out of three, because a checkbox saying "it matched" gets ticked by reflex. | [§9](custom/DEPLOYMENT-3D-TEAM.md#9-adding-a-device-is-mutual-but-it-is-not-authentication) |
| **Folders look synced** | A violet folder icon in Explorer for every synced folder. No shell extension, no COM registration, no administrator. | [§10](custom/DEPLOYMENT-3D-TEAM.md#10-a-synced-folder-looked-like-any-other-folder--now-it-does-not) |
| **Rate limits that admit the truth** | Upstream ignores rate limits on the local network by default and says so nowhere, so people conclude the feature is broken. The limit fields now carry a note saying whether the limit applies — and in the device editor, whether it is applying *right now*. | [§11](custom/DEPLOYMENT-3D-TEAM.md#11-rate-limits-do-nothing-on-the-local-network-and-nothing-said-so) |
| **You are told when there is an update** | The tray notices when a machine you sync with is running a newer build and says so. It does this **without contacting anything** — Syncthing already reports every connected device's version, so there is no releases API to poll and no signing key to guard. | [§17](custom/DEPLOYMENT-3D-TEAM.md#17-nothing-ever-said-a-new-version-existed) |
| **No telemetry at all** | Not a setting: a compile-time constant every reporter consults. See below. | [§12](custom/DEPLOYMENT-3D-TEAM.md#12-syncthing-phoned-home-on-a-crash-and-never-asked) |
| **Defaults for this work** | Staggered versioning at 30 days, a 20 GB absolute disk reserve, the Blender ignore set, the violet theme, and a device named after you rather than `DESKTOP-A1B2C3`. Seeded into `config.xml` before Syncthing first starts. | [seed-config.ps1](custom/scripts/seed-config.ps1) |
| **Violet** | A theme for the web interface, seeded as the default, plus the tray and folder icons to match. | [`gui/violet/`](gui/violet) |

## What it deliberately does not do

- **It sends no telemetry, and that is a constant rather than a setting.**
  Upstream has three reporters gated by two options, and the third — the
  panic-log upload — is **on by default and never asks**: a stock build that
  crashes uploads goroutine stacks and the tail of its log, which for us means
  folder names and paths. All three now consult
  `const TelemetryEnabled = false`, the consent modal and the settings control
  are gone with them, and three tests assert the wire stays silent with every
  telemetry option forced *on*.
- **No in-app auto-upgrade.** Syncthing verifies upgrades against upstream's
  signing key, which cannot validate builds from this fork — leaving it on would
  either fail or quietly replace this build with stock Syncthing. Update by
  running a newer installer over the top of the old one; your device identity,
  folders and settings are kept. The tray will tell you when there is one.
- **The installer is not code-signed**, which is why Windows warns about it.
- **Global discovery and relays are still on.** They are a much larger
  third-party surface than the telemetry ever was, and turning them off is still
  the wrong call: they are load-bearing, and their failure mode is silent and
  indistinguishable from the other machine being switched off.
  [§14](custom/DEPLOYMENT-3D-TEAM.md#14-global-discovery-and-relays-a-bigger-surface-and-a-different-question)
  has the reasoning and what to do instead.
- **Windows only.** The installer, the tray, the notifications and the folder
  icons are all Win32. For any other platform, use
  [upstream Syncthing](https://syncthing.net/) — it is the same protocol and the
  two interoperate.

## Relationship to upstream Syncthing

This is a fork of [syncthing/syncthing](https://github.com/syncthing/syncthing),
and almost all of the code here is theirs. Thirteen upstream files carry fork
edits; everything else the fork adds lives in files upstream does not have —
`custom/`, `gui/violet/`, `gui/default/syncthing/desuq/` and a handful more.
[`custom/CUSTOMIZATIONS.md`](custom/CUSTOMIZATIONS.md) lists every one of those
edits, what it does, and how a conflict resolves.

Please do not take fork problems to upstream:

- **A bug in this fork** → [this issue tracker](https://github.com/desuqcafe/desuqcafe-syncthing/issues).
- **A bug in Syncthing itself** → the [Syncthing forum](https://forum.syncthing.net/)
  or [upstream's tracker](https://github.com/syncthing/syncthing/issues), and a
  security vulnerability in Syncthing to security@syncthing.net as their
  [README](https://github.com/syncthing/syncthing/blob/main/README.md) asks.
  That address is upstream's, not ours.
- **How Syncthing works** → the [Syncthing documentation](https://docs.syncthing.net/)
  applies to this build too. Upstream's goals are in [GOALS.md](GOALS.md), and
  [README-Docker.md](README-Docker.md) documents upstream's Docker image, which
  this fork does not build or change.

## Building from source

Needs [Go](https://go.dev/dl/) (the version in [`go.mod`](go.mod)) and
[Inno Setup 6](https://jrsoftware.org/isdl.php). The build script provisions
`goversioninfo` itself.

```powershell
.\custom\build-windows.ps1 -Installer     # binary + installer into custom\dist
.\custom\scripts\run-tests.ps1            # all eight suites; -Quick for the fast six
```

`go run build.go` still builds a stock, unbranded Syncthing binary the way
upstream's does.

The three documents worth reading before changing anything:

| | |
| --- | --- |
| [`CLAUDE.md`](CLAUDE.md) | How to work in this repository, and the gotchas that cost somebody an afternoon |
| [`custom/CUSTOMIZATIONS.md`](custom/CUSTOMIZATIONS.md) | Every divergence from upstream, and the merge plan |
| [`custom/DEPLOYMENT-3D-TEAM.md`](custom/DEPLOYMENT-3D-TEAM.md) | What the fork adds, why each thing exists, and what is verified against a running pair rather than assumed |

## Licence

All code is licensed under the [MPLv2 License](LICENSE), as upstream's is.

Syncthing is copyright the Syncthing Authors — see [AUTHORS](AUTHORS) — and this
fork keeps their licence, their copyright notices and their protocol. It is not
affiliated with or endorsed by the Syncthing project.
