<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.7

Five things Syncthing did correctly and then had nothing more to say about.

Update by running the installer over the top. Your folders, devices and
settings are untouched.

## Two people changed the same file

This is the normal week, not an edge case, and until now it ended badly: your
folder quietly grew a second file called something like

    scene.sync-conflict-20260824-032916-F67Q3OS.blend

and nothing anywhere would tell you which of the two was yours. Nobody deletes
a file they cannot identify, so they pile up.

There is now a **Conflicts** screen — a row on the folder card while there is
anything to decide, and a tab under History. It puts the two copies next to
each other: how big each one is, when it was written, who wrote it, and, for a
texture, **the picture itself**. Two buttons: keep the one in use, or use the
one that was set aside.

Neither button throws anything away. Whichever copy you do not keep goes into
*Older versions*, so a wrong click is one more click to undo. On a folder with
version history switched off it cannot make that promise, so it says so and
asks first.

You only have to do this on one computer. The other person's copy sorts itself
out.

## Pictures instead of timestamps

*Older versions* used to offer a list of times. Fifty saves of one texture is
fifty timestamps, and the only way to find the right one was to restore it and
look — which overwrites the file you were trying to protect.

Image files now show a thumbnail, in version history and in the conflicts
screen. `.blend` files do not; there is no honest way to read one.

## A folder that has stopped now tells you why

*Stopped* used to be the whole message. The card now says what Syncthing
actually reported, and offers the three things that fix it: plug the drive
back in, point the folder at where it lives now, or set it up again.

**Setting it up again is refused when it would be dangerous.** If the folder is
supposed to hold files and the directory is empty, making it again would tell
everybody else you deleted them — and their copies would go too. It says that,
with the number of files, instead of doing it.

## It notices when syncing has stopped

If a computer you sync with has not been in touch for three days, you are told.
Three days rather than one, so an ordinary weekend is quiet.

The wording is careful about blame, because it cannot know: if everything else
is connected it says whose computer it is, and if nothing at all has connected
it says so without pointing at anybody — that usually means this computer is
the one that is offline.

This is the other half of the Defender problem below. A machine that has
stopped syncing is the last one able to warn you; the people it syncs with can.

## Pause for an hour, and mean it

The tray's *Pause Syncing* stays paused until you turn it back on, which is
fine until you pause for a render and forget — a paused Syncthing looks exactly
like a working one from inside Blender.

**Pause for a while → 1 hour / 4 hours** starts again on its own, tells you
when in the menu while it is holding, and says so when it lifts. Quitting the
tray also lifts it, so a timed pause can never outlast the thing that promised
to end it.

## Still true from desuq.5

Windows Defender may quarantine the tray on install, as
`Trojan:Win32/Bearfoos.A!ml`. It is a false positive — the `!ml` marks it as a
machine-learning guess rather than recognised malware, and that classifier is
well known for eating small unsigned programs written in Go.

**It takes both Start Menu shortcuts with it**, so "start when I sign in"
stops working and Syncthing will not come back after a restart. If it happens:
*Windows Security → Virus & threat protection → Protection history*, restore
the files, then add an exclusion for the install folder so future updates
survive.

`custom/DEPLOYMENT-3D-TEAM.md` section 20 has the full picture, including an
audit of what the tray actually does and why a classifier dislikes it.
