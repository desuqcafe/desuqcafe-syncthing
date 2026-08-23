// Copyright (C) 2026 desuqcafe.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package build

// TelemetryEnabled reports whether this build is allowed to send anything
// about the user, their machine or their crashes to a third party. It is
// false, permanently, and this fork has no way to make it true at runtime.
//
// Upstream Syncthing has three separate reporters, gated by two different
// options, and only one of them is off by default:
//
//   - the usage report        lib/ur/usage_report.go     -> Options.URURL
//     (Options.URAccepted >= 2)
//   - non-fatal failure reports  lib/ur/failurereporting.go -> Options.CRURL
//     (Options.URAccepted > 0)
//   - the panic-log upload    cmd/syncthing/crash_reporting.go -> Options.CRURL
//     (Options.CREnabled, which upstream defaults to TRUE)
//
// So a stock build already uploads the log of any crash without ever having
// asked. Turning the options off would fix that for a config we wrote, but
// not for one we did not, and the GUI offered a dropdown to turn it back on.
//
// Each of the three now consults this constant before it does anything, so
// the guarantee does not depend on a config value being right. The options
// are still defaulted off and still seeded off (see
// custom/scripts/seed-config.ps1) so that config.xml does not claim
// otherwise, but that is now cosmetic rather than load-bearing.
//
// build.IsCandidate force-enables usage reporting upstream. It is
// strings.Contains(Version, "-rc.") and our versions are v2.1.4-desuq.N, so
// it is false for us -- but lib/syncthing consults this constant there too
// rather than relying on a release-naming convention holding forever.
//
// Deliberately a constant rather than a build tag: a tag someone forgets to
// pass is a silent regression, and there is no build of this fork that should
// ever want telemetry.
const TelemetryEnabled = false
