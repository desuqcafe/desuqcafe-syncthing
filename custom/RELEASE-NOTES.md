<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.4

Two things the interface was only half telling you, and a first look at three
screens nobody had ever seen in a browser.

Update by running the installer over the top. Your folders, devices and
settings are untouched.

## "Choose files" was never about disk space

Unticking something in the picker stops Syncthing keeping it up to date. It
does **not** delete what is already on your disk — and until now nothing said
so plainly, so unticking three gigabytes of reference scans to make room left
your free space exactly where it was.

Those are two separate actions and both are now offered. Unticking stops
updates. **Free it up**, on the folder's card, deletes the local copies — and
it appears whenever there is something to reclaim, not only in the moment
after a pick, because the bytes outlive the moment.

This is the only thing this build does that deletes your files, so what it
*refuses* to delete is the important part. Before anything is removed, every
file is checked again on the spot:

- it must match the ignore rules **currently** in force,
- somebody you are **connected to right now** must still have it,
- and your copy must match theirs in size and timestamp.

So a file you edited after unticking it is kept, and a file nobody else has is
kept. Everything it declines to touch is listed by name with the reason:

> Deleted 3,178 files, 3.0 GB freed. 2 files were kept:
> RefPhotos/notes.txt — nobody else connected has this copy

Because the guarantee is "somebody else still has it", ticking the item again
brings the files back.

## Thirty days of history you could not read

Every folder this build creates keeps old copies of files for thirty days.
That has been true for several releases. What was missing was any way to look
at them — so the copies were being written, taking up space, and were
unreachable.

**History**, on every folder card, has two tabs.

**Recent changes** — who touched what, lately, collapsed so that one save or
one scan is one line rather than four hundred. It is honest about its limits:
the list lives in memory and starts again whenever Syncthing restarts, and the
screen says so rather than showing you an empty list you might read as "nobody
has done anything".

**Older versions** — the archive, which survives restarts. Files are listed
newest first with how many copies are kept and what they cost; open one and
every copy has a **Restore** beside it. A file somebody deleted is tagged, and
its button says **Put it back**.

Restoring is safer than it sounds, and the screen says so: your current file is
archived *before* it is replaced, so it becomes the newest entry in the same
list. An accidental restore is undone by restoring again.

If a folder has versioning switched off, the tab now says exactly that, and
warns that deleting a file there is permanent. It used to show an error.

## Three things that had nowhere to live

Reaching them meant opening **Technical details** and knowing what to look
for. All three are now on the folder's card:

- **History** — described above.
- **N that would not sync** — *which* files failed, not just how many.
- **Undo my changes here** — on a receive-only folder, the only way to unstick
  it after something changed locally. This is the folder type recommended for
  people who only receive work, so until now that configuration had no exit.

## Smaller

- The folder card said *"879 KiB not taken"* about files that were sitting on
  the disk in full. It now says "not kept up to date", which is true either way.
- The first-run **Setup guide** used green ticks for finished steps — the one
  fork screen that did. It is violet throughout now, like everything else.
- Empty folders left behind by reclaiming are tidied up.
- A twelfth test suite, and the first-run guide, the main screen and the new
  history screen have all now been looked at in a real browser rather than
  only in tests.
