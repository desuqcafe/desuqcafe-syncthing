<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.6-desuq.8

Mostly about things that looked fine and were not.

Update by running the installer over the top. Your folders, devices and
settings are untouched.

## If Windows Security removes part of the app, you are told

Windows Defender sometimes removes this app's notification-area icon right
after installing, mistaking it for malware (desuq.5 has the story). The
damage is worse than a missing icon: Syncthing keeps running, so everything
looks fine, until the next restart. After that it does not start at all.

Two things now catch this:

- **The installer checks before it closes.** If the file disappears in the
  seconds after installing, you get a message saying what happened, what it
  will cost, and how to put it back, with a button that opens Windows
  Security.
- **The main screen says so for as long as it is true**, in red, at the top.
  This covers the case where Defender acts days later, after an update.

Neither changes any security setting for you. Putting the file back is one
click in Windows Security's *Protection history*: choose **Restore**.

## A share nobody has accepted no longer looks like one in progress

Share a folder with somebody who is already online and the main screen used to
say they were *catching up — 12 MiB to go*, when they had not accepted it at
all. That is the usual way to share a folder, so it was wrong most of the
time. It now says they have not accepted it yet.

## Choosing files

- If you close *Choose what to sync* without choosing, nothing is downloaded
  and the folder waits, which is what it did before. What changed is how it
  describes itself: it used to say *Everything you chose is here* about a
  folder where nothing had been chosen. It now says **Nothing has been picked
  yet** and points at *Choose files*.
- The file list in the picker had a stray dot beside every row. Gone.

## Based on Syncthing 2.1.6

This build now includes upstream Syncthing's latest changes, currently a
release candidate for 2.1.6. The two that matter here:

- **A device you let introduce others could add itself to folders it was never
  given.** Fixed upstream.
- Syncthing could stop writing its log file when started without a console
  window, which is how this app always starts it. Fixed upstream.

That is also why the version number moves from 2.1.4 to 2.1.6. It tracks the
Syncthing it is built on; the number after *desuq* keeps counting as before.

## Still true

Windows Defender may still quarantine the tray on install, as
`Trojan:Win32/Bearfoos.A!ml`. It is a false positive; the difference now is
that you will hear about it. `custom/DEPLOYMENT-3D-TEAM.md` section 20 has the
full picture.
