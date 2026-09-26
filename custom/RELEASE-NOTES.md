<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.6-desuq.10

Know what happened while you were apart, know whether your work has reached
the others, and see a picture of every scene in the history.

Update by running the installer over the top. Your folders, devices and
settings are untouched. **Everyone you share with needs this version** for
most of this to work between you.

## Working apart

- **When two computers meet again, one notification says what happened.**
  What the other person changed, and which files you *both* changed, which
  are the ones that became conflict copies.
- **A file you deleted can come back** if somebody else edited it later.
  That used to happen silently. Now you are told.
- **Saving a `.blend` marks it as "I'm working on this" automatically**, and
  the mark comes off after a few quiet hours.
- **A mark you make while someone is offline says so.** The notification no
  longer implies they have seen it.

## Right-click a .blend

In Explorer, right-click a `.blend` → *desuqcafe Syncthing* → **I'm working
on this**, **Show history**, or **Who has this?** (on Windows 11 this is under
*Show more options*). Hover over a synced folder to see who is working on
what in it.

## Did it reach them?

- While your changes are on their way: *Sending cabin.blend to Kai.*
- While they are offline: *Kai does not have your latest cabin.blend yet —
  their computer is not connected. It goes when they are back.* This used to
  say "Sending" and "catching up" about a computer that was switched off.
- When a delivery that waited has arrived, a notification: *cabin.blend has
  reached Kai.* Deliveries that finish in seconds stay quiet.
- Pausing syncing warns you if somebody does not have your latest yet.

## Nobody silently in the middle

With three people, the usual setup has the other two syncing only *through*
you. When your computer is off, they stop syncing with each other, and
nothing said so. Now the screen does, on every computer involved. The two at
the edges get a **Connect directly** button, which adds the other person;
they then accept it as usual.

## A picture of every scene

History and Conflicts now show the preview Blender saves inside each `.blend`,
beside every older version, so you can find the version you want by looking
at it. This works for files saved in Blender 5 too, which are compressed.

## Hand a file over

Your own "I'm working on this" mark now has **Give to Kai**. Their computer
picks it up and marks the file as theirs, and they get a notification. The
file is never unmarked in between, so nobody else opens it by mistake. It
still works if you switch off straight after handing it over.

## Keep a version for good

Older copies are cleaned up after thirty days. Now any one of them can be
**pinned** in History, and a pinned copy is kept until you unpin it —
restoring it does not use it up. Pins are kept on the computer where you
made them: each computer keeps its own older copies.

## Say why you changed it

After saving a file you can add a sentence — *moved the camera, lighting
untouched* — with **Say why you changed a file** on the folder, when you
press **Done** on your mark, or by right-clicking a `.blend`. Everyone you
share with sees it on the main screen and gets a notification. History shows
each note beside the exact version it was written about, so an older copy
says why it was saved. Only the person who saved a version can write its note.

## Who is around

Each folder now says, for each person, whether they are here now or when you
last saw them, what they have marked as working on, and the last file they
saved. An offline person's card says since when.

## Still true

Windows Defender may still quarantine the tray on install as
`Trojan:Win32/Bearfoos.A!ml`. It is a false positive, and you are told when
it happens. `custom/DEPLOYMENT-3D-TEAM.md` section 20 has the details.
