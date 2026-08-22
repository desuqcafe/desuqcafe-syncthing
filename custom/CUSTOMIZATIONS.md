# What this fork changes, and how to stay mergeable with upstream

This fork exists so we can add our own behaviour later. The overriding design
rule right now is: **keep the divergence from upstream Syncthing as small as
possible**, so `git merge upstream/main` stays boring.

Everything below is deliberate. Read this before adding a change of your own.

## The rule

> Add new files under `custom/`. Only edit an upstream file when there is no
> other way, and when you do, make the edit additive and keep upstream's
> behaviour as the default.

Anything in `custom/` and `.github/workflows/desuq-release.yaml` can never
conflict, because upstream has no such files.

## Files we modify from upstream

This is the complete list. Keep it that way, and keep this table current.

| File | Change | Conflict risk |
| --- | --- | --- |
| `build.go` | Added an `envOr()` helper and used it for the six Windows version-resource strings in `shouldBuildSyso()` (product name, publisher, description, internal/original filename, icon). | **Low.** 16 lines in one rarely-touched function. With the `ST_BRAND_*` variables unset the behaviour is byte-for-byte upstream's, so the change is safe to keep across merges. |

Nothing else. In particular we have **not**:

- renamed the Go module (`github.com/syncthing/syncthing`) — renaming it would
  rewrite every import in the tree and make merges impossible;
- renamed packages, types or internal identifiers;
- edited anything under `lib/`, `cmd/` or `gui/`.

## How the branding is done without touching upstream

| What | How |
| --- | --- |
| Executable name (`desuq-syncthing.exe`) | The build script renames build.go's output. No source change. |
| Publisher / product shown in file properties and Add-Remove Programs | `ST_BRAND_*` environment variables read by the patched `build.go`. |
| Separate config and database (`%LOCALAPPDATA%\desuqcafe-syncthing`) | The shortcuts pass `--home=...`. Upstream already supports this flag; no source change. This is also why the fork can run alongside a stock Syncthing install. |
| GUI port | Not forced. Syncthing probes for a free port on first start, so it settles on 8384 or the next free port automatically. |
| Auto-upgrade | Compiled out with the `noupgrade` build tag (see below). No source change. |
| GUI logo / CSS *(not used yet)* | Syncthing serves `$STGUIASSETS/<theme>/<file>` in preference to its built-in copy and falls back per file, so dropping art into an assets directory rebrands the web UI **without editing `gui/`**. Wire it up by setting `STGUIASSETS` in the shortcuts. |

All names live in one place: `custom/branding.ps1`.

## Why auto-upgrade is disabled

Syncthing verifies downloaded upgrades against the upstream project's release
signing key (`lib/upgrade`, `signature.Verify(SigningKey, ...)`). That key
cannot validate builds produced from this fork. Leaving the upgrade feature on
would therefore either fail, or — worse — replace our build with stock
Syncthing and silently drop our customisations.

So releases are built with `-no-upgrade`, which strips the feature entirely.
Users update by running a newer installer.

If we ever want real in-app updates for the fork, the work is: generate our own
signing key, replace `SigningKey` in `lib/upgrade`, sign each release, and point
`ReleasesURL` at our own metadata. That is a genuine feature, not a config
tweak, and it means guarding a private key.

## Merging upstream

```powershell
.\custom\scripts\sync-upstream.ps1 -DryRun   # see what is coming
.\custom\scripts\sync-upstream.ps1           # merge, then verify the build
```

The script refuses to run on a dirty tree, warns you if upstream touched
`build.go`, merges, and then rebuilds to prove the fork still compiles.

To track stable releases rather than upstream's development branch, pass a tag:

```powershell
.\custom\scripts\sync-upstream.ps1 -Ref v2.1.4
```

If `build.go` ever does conflict, the resolution is always the same: take
upstream's version of the function and re-apply the `envOr(...)` wrappers.

## Upstream's CI is disabled on this fork

The inherited workflows (`build-syncthing.yaml` and friends) are upstream's full
multi-platform release pipeline. On a fork they burn Actions minutes and fail on
missing secrets.

They are switched off **through the GitHub API rather than by deleting the
files**, precisely so they never conflict during a merge. Re-apply the setting
with:

```powershell
.\custom\scripts\disable-inherited-ci.ps1
```

Because that state lives in GitHub rather than in the repository, it must be
re-applied if the repository is ever recreated or cloned to a new remote.

Our own pipeline is `.github/workflows/desuq-release.yaml`.
