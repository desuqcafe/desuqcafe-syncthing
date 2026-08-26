<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.6

One security fix in the notification-area app. Nothing you will see.

Update by running the installer over the top. Your folders, devices and
settings are untouched.

## The tray now checks who it is talking to

If you switch the web interface over to HTTPS — not the default, and not what
this build sets up — the tray used to accept **any** certificate offered on
that port, on the reasoning that the connection never leaves your computer.

That reasoning does not hold on a machine with more than one account. Anything
that managed to answer on that port first would have been handed the tray's
API key, which is complete control of every folder Syncthing is managing.

It now accepts exactly one certificate: the one Syncthing generated for your
install, sitting beside your configuration. Anything else is refused.

On a normal install, where the web interface is plain HTTP on this computer
only, none of this code runs at all.

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
