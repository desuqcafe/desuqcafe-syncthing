# desuqcafe Syncthing

A fork of [Syncthing](https://github.com/syncthing/syncthing) packaged as a
one-click Windows install, kept deliberately close to upstream so we can pull in
their changes and add our own behaviour on top.

Upstream's own documentation still applies — this file only covers what is
different here.

---

## For the person you are sending this to

1. Download `desuq-syncthing-setup-<version>.exe` from the
   [Releases page](https://github.com/desuqcafe/desuqcafe-syncthing/releases).
2. Run it. There is **no UAC prompt** — it installs only for the current user.
3. Tick *"Start automatically when I sign in"* if wanted, and finish.

Syncthing starts and the web interface opens at <http://127.0.0.1:8384>.
No console window appears; it runs quietly in the background.

Windows SmartScreen will likely warn that the publisher is unknown, because the
installer is not code-signed. *More info → Run anyway.*

**To update:** run a newer installer over the top. Settings are preserved.
In-app auto-update is deliberately switched off (see below).

**To uninstall:** Settings → Apps → *desuqcafe Syncthing*. It asks whether to
keep your configuration; synced files are never touched.

| | Location |
| --- | --- |
| Program | `%LOCALAPPDATA%\Programs\desuq-syncthing` |
| Config, keys, database, log | `%LOCALAPPDATA%\desuqcafe-syncthing` |

Those paths are separate from stock Syncthing's, so both can be installed on the
same machine without interfering.

---

## Building it yourself

One-time setup:

```powershell
winget install GoLang.Go
winget install JRSoftware.InnoSetup
```

Then:

```powershell
.\custom\build-windows.ps1                     # binary only
.\custom\build-windows.ps1 -Installer          # binary + installer
.\custom\build-windows.ps1 -Version v2.1.4-desuq.2 -Installer
```

Output lands in `custom/dist/`. The build script provisions `goversioninfo`
itself if it is missing.

## Cutting a release

```powershell
git tag v2.1.4-desuq.1
git push origin v2.1.4-desuq.1
```

`.github/workflows/desuq-release.yaml` builds the installer on a Windows runner,
generates SHA256 checksums and publishes a GitHub Release. Send your user the
release link.

Tags must match `v*-desuq*` to trigger it.

## Pulling in upstream Syncthing changes

```powershell
.\custom\scripts\sync-upstream.ps1 -DryRun   # preview
.\custom\scripts\sync-upstream.ps1           # merge and verify the build
```

The fork touches exactly one upstream file, so this is normally a clean merge.
See [CUSTOMIZATIONS.md](CUSTOMIZATIONS.md) for the details, the reasoning, and
what to do in the rare case of a conflict.

## Renaming or rebranding

Edit `custom/branding.ps1` — product name, executable name, publisher and data
directory all come from there. Drop a `custom/branding/logo.ico` in to replace
the icon.

Do **not** change `AppId` after the first release; Inno Setup uses it to
recognise existing installations and upgrade them in place.

## Why auto-upgrade is off

Syncthing verifies upgrades against upstream's release signing key, which cannot
validate builds from this fork. Left enabled it would either fail or replace our
build with stock Syncthing. Releases are therefore built with `-no-upgrade`.
[CUSTOMIZATIONS.md](CUSTOMIZATIONS.md) explains what enabling real self-updates
would involve.
