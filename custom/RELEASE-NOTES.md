<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.3

The screen you land on is new. `desuq.2` added the features; this one is about
what you actually see, what the words mean, and a handful of things that could
previously go wrong quietly.

Update by running the installer over the top.

## A main screen that answers the questions you have

Stock Syncthing opens on a table of folders and devices with thirty-one
controls on it, several of them destructive. It is a good screen for somebody
who administers a sync network, and the wrong screen for somebody who wants to
know whether their work is safe.

- **One sentence at the top**, and it is the answer: *"Everything is here"*,
  or what is wrong, or what is still coming.
- **Folders and people as cards** — how many files, how big, who has them, how
  far each person has got, and whether you have verified them.
- **It says what is true of the other machine**, not what is true of yours.
  A folder can be perfectly idle on your disk because a teammate never accepted
  the share; that used to read as "up to date". So does a folder where you have
  only chosen some of the files. Both now say so, by name.
- **Upstream's full view is still there**, one click down, under *Technical
  details* — every folder and device setting exactly as Syncthing shows them.

## Open folder

The path to a synced folder used to be text you selected and pasted into an
Explorer window. There is now a button on every folder card that opens it.

## Nothing that deletes your work by accident

- **Closing the file picker no longer downloads everything.** It used to clear
  the "hold everything back" rule on the way out — measured at seven files held
  back becoming twenty-six files and 18 MB — while the text beside the button
  said nothing was being downloaded. Closing now holds the folder back and
  pauses it, and *Choose files* is the way back in.
- **New top-level files and folders a teammate adds are held back** rather than
  arriving unasked. Files added inside a folder you kept still arrive, which is
  the point of keeping it.
- **"Revert Local Changes" is no longer one click under a green tick.** A
  receive-only folder holding your own edits was painted success-green with a
  checkmark, directly above a button that deletes them. The panel is now amber,
  and the dialogue names the folder, how many files, how big, and whether
  copies go to version history first or are gone for good.
- **A teammate cannot write a rule into your ignore file** by naming a file
  carefully.

## The notification-area icon

- **Every shortcut starts the tray**, not the daemon. Searching Windows for the
  app used to open a browser tab and leave nothing on screen.
- **One icon per configuration.** A second launch opens the window and exits,
  instead of leaving you with two icons and two copies of everything behind
  them.
- **Sync conflicts raise a notification**, naming the file you know —
  `scene.blend`, not the generated conflict name beside it.
- **Upgrading no longer ends in a hard kill.** The installer now asks the tray
  to shut Syncthing down properly and waits for it.

## Words that mean the same thing everywhere

The one thing two people have to say out loud to each other had four names in
the interface: *Device ID*, *Identification*, *Show ID*, and *device code*.
It is **device code** now, in all of them.

- **The shutdown dialogue** was a green box saying "Syncthing has been shut
  down", with no button, no Escape, no backdrop and no mention of how to start
  it again. It now says your files are where you left them, that closing the
  window is safe, and that the tray icon starts it back up.
- **The green "GUI Authentication" panel** is off the front page. It was the
  largest thing on a fresh install, coloured the same as "your files are safe",
  for something that is neither a success nor a fact about syncing. The
  information now sits in Settings, directly above the two fields that resolve
  it. Upstream's much stronger red warning, for a GUI reachable from off your
  machine, is untouched.
- **"Automatic upgrades"** is gone from Settings. This build is compiled
  without self-update, so the field could only ever read "Disabled by
  administrator or maintainer", which was not true of anybody. It says plainly
  that the build does not update itself.
- **Help** points at this fork's own changelog, issues and source, and no
  longer at a statistics page built entirely out of usage reports this build
  does not send.

## Fixes worth naming

- **Free-space warnings fire when they should.** The 20 GB reserve was
  declared and then ignored, so "not enough space" arrived about 20 GB late in
  all three places it appears.
- **Folder sizes in the picker are real.** Syncthing's directories-only listing
  reaches its answer by skipping every file, so every directory in it reports
  as zero bytes — which made the picker's disk check inert on exactly the
  folders large enough to matter.
- **The verification card opens for people you have already added.** It was
  reachable only while adding somebody, which is the one moment you do not need
  it; re-reading the phrase to each other later was impossible.
- **Dark and Black themes** no longer draw this fork's dialogues in light text
  on a light background. The first-run guide was effectively invisible under
  both.
- **A re-seed never renames a device** whose name somebody chose.

## Under the hood

Eleven test suites now run on every push and again as a gate before any release
is built — including, new this release, the fork's own server-side handlers,
which had no automated coverage at all.

## Upgrading

Run the installer over the top. Your device identity, folders, settings and
"start at sign-in" choice are all kept: the seeded defaults are only ever
written once, so nothing you have changed since is touched.
