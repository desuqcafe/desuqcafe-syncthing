<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.6-desuq.11

Nothing syncs with somebody until the two of you have compared cards, inviting
somebody is one short dialog, and updating is one click.

## Before you update: check who is verified

**From this version on, your computer will not connect to anybody you have not
verified.** Cards you already confirmed are carried over the first time you
open the main screen, but only on the computer and in the browser where you
confirmed them.

So after updating, open the main screen and look under **Sharing with**. Anybody
marked **Not verified** will not sync until you get them on a call, press
**Verify**, and each pick the other's card. Comparing works even if they are
still on the old version.

## Verifying is now required

- **Nobody connects until verified.** Adding someone proves your two computers
  agreed on a code, not whose code it was. Comparing the card on a call is how
  you both know it was not swapped on the way. It used to be optional, and was
  skipped.
- **The card is clearer.** Three numbered steps: get them on a call, take turns
  reading your card out loud, pick the one they describe. The why is folded
  away under *Why this matters*.
- **You are told when an unverified computer is trying to connect**, both on
  the main screen and as a notification. That is usually the moment they are on
  the phone asking whether you can see them.

## Inviting someone

**Invite someone** on the main screen does it in one go: paste their code (or
show them yours), compare cards while you are on the call, and tick which
folders they get. When somebody adds *you*, they show up under **Waiting for
you** with **Add and verify**.

When a folder is offered to you, the offer now says **who else is in it**, and
which of those people reach you only through someone else.

## Who a folder is shared with

Folder cards now name everybody the folder is shared with, rather than showing
initials you had to hover over.

## Updating

- The update notification now **downloads the installer** instead of opening a
  web page. Run the downloaded file to update.
- The tray menu keeps a **Download update** entry until you have updated, and
  the main screen says so too, so dismissing the notification no longer means
  never hearing about it again.
- You are no longer told about an "update" that is really the version you
  already have. That happened whenever the person who builds releases was
  running a build of their own.

## Still true

Windows Defender may still quarantine the tray on install as
`Trojan:Win32/Bearfoos.A!ml`. It is a false positive, and you are told when
it happens. `custom/DEPLOYMENT-3D-TEAM.md` section 20 has the details.
