<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.2

The first release with any of the fork's own features in it. `desuq.1` was the
packaging — a per-user installer and a branded binary — and nothing else. This
one is everything built since.

If you are already running `desuq.1`, this is worth updating for. If you are
installing for the first time, this is the one to install.

## You can see it running, and it tells you things

- **A notification-area icon** with five states. It starts Syncthing, keeps it
  running, and is the only thing on screen that says whether syncing is
  working — upstream has no tray icon and no service mode, so started at
  sign-in it was completely invisible. Right-click for **Open**, **Pause
  Syncing** and **Quit**.
- **Windows notifications** for the six things that actually need a person: a
  device asking to connect, a folder being offered, a sync finishing, a folder
  in trouble, a disk about to fill, and a teammate on a newer version. Every
  one is rate-limited and clicks through to the page that can act on it.
- **Synced folders look synced in Explorer** — a violet folder icon on every
  one, written per user with no shell extension, no COM registration and no
  administrator.

## Setting it up for the first time

- **A first-run guide.** A fresh install used to be an empty screen with no
  next action on it, and the one thing you needed — your device code — was
  behind *Actions → Show ID*. There is now a four-step guide that names the
  machine, shows the code with a Copy button and a QR, walks the device
  verification, and explains what a folder offer will look like. It closes at
  any point and comes back where it left off from **Actions → Setup guide**.
- **A verified device handshake.** Both machines show the same collectible
  card — `《 CRIMSON TALISMAN NOCTURNE 》Rank CLXXVI` — derived from the two
  device IDs. Read it to each other on a call. Confirming means picking the
  real card out of three, because a checkbox saying "it matched" gets ticked
  by reflex.

## Choosing what lands on your disk

- **A real file picker.** Accepting a share now holds everything back, waits
  for the file list to arrive, and shows you the whole tree to tick from.
  Nothing downloads until you have chosen. Upstream's answer was a textarea
  full of glob patterns.
- **Free space you can see** — under the folder path, in the folder detail, and
  as a warning when a folder will not fit. Upstream checks capacity per file
  and never up front, so a 400 GB share could be accepted onto a 250 GB drive.
- **Rate limits that admit the truth.** Upstream ignores rate limits on the
  local network by default and says so nowhere. The limit fields now say
  whether the limit applies — and in the device editor, whether it is applying
  right now.

## It sends nothing

Upstream has three telemetry reporters gated by two options, and the third —
the panic-log upload — is **on by default and never asks**: a stock build that
crashes uploads goroutine stacks and the tail of its log, which for this team
means folder names and paths.

All three now consult a compile-time constant. The consent modal and the
settings dropdown are gone with them, and three tests assert the wire stays
silent with every telemetry option forced *on*. It is not a setting you can
turn back on by accident.

## Smaller things

- **You will be told when an update exists.** Syncthing already reports what
  version every device you connect to is running, so the tray compares itself
  to your team and raises a notification when one of them is ahead. Nothing is
  polled and nothing external is contacted — the check is entirely between you
  and the machines you already sync with.
- The uninstaller takes the folder icons back off before it goes.
- A re-seed never renames a device whose name somebody chose.
- Nine test suites, run on every push and again before a release is built.

## Upgrading from desuq.1

Run the installer over the top. Your device identity, folders, settings and
"start at sign-in" choice are all kept — the seeded defaults are only ever
written once, so nothing you have changed since is touched.
