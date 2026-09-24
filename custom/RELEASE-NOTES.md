<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.6-desuq.9

Say "I'm working on this" before you open a file, so nobody ends up with a
conflict copy.

Update by running the installer over the top. Your folders, devices and
settings are untouched. **Everyone you share with needs this version** to see
each other's marks.

## "I'm working on this"

When two people open the same `.blend` and both save, Syncthing keeps both and
renames one aside. Nothing is lost, but two afternoons have gone different
ways, and one of you has to redo work. What would have stopped it is saying
something before either of you opened the file.

Now there is a way to say it. In Explorer, **right-click the file → Send to →
desuqcafe Syncthing - I'm working on this.** Everyone you share that folder
with:

- gets a notification: *Yuki is working on cabin.blend*;
- sees it on the folder in the app, with the time you started;
- and, if they change that file anyway, gets a second warning saying so,
  while there is still time to call you.

When you are done, send the file there again, or press **Done** in the app.
A mark more than three days old is shown faded, as possibly forgotten.

**Nothing is locked.** Anyone can still open and save a marked file. It is a
note to the others, not a lock.

## It no longer says someone has "the same files" when they do not

If somebody picked only some of a folder's files to keep, your screen used to
say they had *the same files as you*. Now it says they keep only part of it,
and how much is not on their computer.

## The activity list no longer stops after a restart

*What has been happening* stopped showing new changes after Syncthing
restarted, until you reloaded the page. It now notices and starts again.

## Still true

Windows Defender may still quarantine the tray on install as
`Trojan:Win32/Bearfoos.A!ml`. It is a false positive, and since desuq.8 you
are told when it happens. `custom/DEPLOYMENT-3D-TEAM.md` section 20 has the
details.
