<!-- The body of the next release, written by hand and rewritten each time.
     .github/workflows/desuq-release.yaml reads this file and appends the
     install section to it, so this file is only ever "what changed".
     Old releases keep their own notes on GitHub; git history keeps these. -->

# desuqcafe Syncthing v2.1.4-desuq.5

A small release with one change, and a warning worth reading if you install
this on somebody else's machine.

Update by running the installer over the top. Your folders, devices and
settings are untouched.

## The tray no longer launches your browser the way malware does

Opening the web interface from the tray menu used to go through
`rundll32 url.dll,FileProtocolHandler`. That was chosen for good reasons — no
console window flashes up, and there are no command-line quoting rules to get
wrong — but it is also a well-known technique for hiding which program really
started something, which is why antivirus software is trained to be suspicious
of it.

It now uses `ShellExecute`, the interface Windows actually provides for this.
Same result, no child process, nothing to be suspicious of.

## Windows Defender may quarantine the tray

This happened on a real machine installing **desuq.4**:

```
Trojan:Win32/Bearfoos.A!ml
  desuq-syncthing-tray.exe
  the setup .exe
  both Start Menu shortcuts
```

**It is a false positive.** The `!ml` means it is Defender's machine-learning
classifier guessing from behaviour rather than recognising known malware, and
that classifier is well known for eating small unsigned programs written in Go.
The change above removes the most suspicious-looking thing in the file, but
nothing can guarantee it stops.

**It matters more than a missing icon.** Both shortcuts are quarantined too, so
"start when I sign in" stops working and Syncthing does not come back after a
restart — the machine goes quiet without saying anything.

If it happens, in **Windows Security → Virus & threat protection → Protection
history**, restore the files, then add an exclusion for the install folder so
the next update survives. Reporting the file to Microsoft as a false positive
is the fix that helps everybody rather than one machine.

`custom/DEPLOYMENT-3D-TEAM.md` section 20 has the details: what the tray
actually does, why a classifier dislikes it, and the fact that its entire
network surface is one file talking to Syncthing on this computer.
