package main

// Telling somebody they are running an old build, without asking anybody.
//
// In-app upgrade is compiled out of this fork (`-no-upgrade`): Syncthing
// verifies downloads against upstream's release signing key, which cannot
// validate our builds, so leaving it on would either fail or quietly replace
// this build with stock Syncthing. Updating means running a newer installer.
//
// That left a real gap. An install ran the version it was given forever, and
// nothing anywhere said a newer one existed -- for two non-technical modellers
// that means the developer has to notice, tell them, and watch them do it.
//
// The obvious fix is to poll GitHub's releases API. This does not do that, on
// purpose. The README's central claim is that this build contacts nobody, and
// a daily request to api.github.com carrying the user's IP would be a footnote
// on that claim forever -- for information the tray can already get for free.
//
// Syncthing tells every device it connects to what version it is running.
// It arrives in the Hello message, `lib/model` puts it in ConnectionStats, and
// /rest/system/connections has served it all along -- upstream's own GUI shows
// it in the device detail table. So the tray compares itself to the machines
// it is already talking to, and says something when one of them is ahead.
//
// That is not the same signal as "a release exists", and it is worth being
// clear about the difference:
//
//   - It cannot fire before a peer connects, so a machine sitting alone stays
//     quiet. For this team that is right: the folders are the point, and a
//     machine with no peers has nothing to be out of date *for*.
//   - It fires when the person who cuts the releases upgrades, which is the
//     order this actually happens in. The developer builds it, installs it,
//     and the two modellers find out from their own machines rather than from
//     a message they have to be online to read.
//   - It costs nothing: no new connection, no new dependency, no key to guard,
//     and one more field on a struct the tray already decodes.
//
// **A peer running stock Syncthing must never trigger this.** Their version
// numbers come from upstream's release train and are routinely ahead of the
// base version this fork was built from -- a modeller connected to somebody
// on plain Syncthing 2.3.0 would otherwise be told to update forever, to
// something that does not exist. So only versions carrying this fork's own
// `-desuq.N` suffix are ever compared.

import (
	"regexp"
	"strconv"
)

// releasesURL is opened in a browser when the toast is clicked, and is the
// only externally-hosted address anywhere in the tray. Nothing fetches it:
// it is handed to the shell by a person clicking a notification, exactly as
// if they had typed it. The tray makes no outbound request of its own, here
// or anywhere else.
const releasesURL = "https://github.com/desuqcafe/desuqcafe-syncthing/releases/latest"

// forkVersion is a parsed desuqcafe Syncthing version.
//
// Release builds are stamped from the tag, e.g. "v2.1.4-desuq.2". A build made
// between tags gets `git describe`'s suffix as well --
// "v2.1.4-desuq.1-23-gd6c6dde6-dirty" -- which is how the developer's own
// machine identifies itself and is deliberately treated differently below.
type forkVersion struct {
	major, minor, patch int
	// rev is the N in -desuq.N.
	rev int
	// dev is the number of commits past that tag; 0 for a build made at one.
	dev int
}

// Deliberately tolerant about the tail. The three numbers, the desuq revision
// and the commit count are the only parts with meaning here, and refusing to
// parse a version because it grew a suffix would turn this feature off
// silently rather than loudly.
var forkVersionRe = regexp.MustCompile(
	`^v?(\d+)\.(\d+)\.(\d+)-desuq\.(\d+)(?:[-+](\d+)-g[0-9a-fA-F]+)?`)

// parseForkVersion reports whether s is a version of this fork, and what it
// means. Anything else -- stock Syncthing, an unparseable string, an empty
// field for a device that has never connected -- comes back false, and is then
// never compared against.
func parseForkVersion(s string) (forkVersion, bool) {
	m := forkVersionRe.FindStringSubmatch(s)
	if m == nil {
		return forkVersion{}, false
	}
	v := forkVersion{
		major: atoi(m[1]),
		minor: atoi(m[2]),
		patch: atoi(m[3]),
		rev:   atoi(m[4]),
	}
	if m[5] != "" {
		v.dev = atoi(m[5])
	}
	return v, true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// newerThan orders two fork versions. A build made partway between two tags
// sorts after the tag it was made from and before the next one, which is what
// `git describe` means by it.
func (v forkVersion) newerThan(o forkVersion) bool {
	for _, pair := range [][2]int{
		{v.major, o.major},
		{v.minor, o.minor},
		{v.patch, o.patch},
		{v.rev, o.rev},
		{v.dev, o.dev},
	} {
		if pair[0] != pair[1] {
			return pair[0] > pair[1]
		}
	}
	return false
}

// isDevBuild reports whether this version was built between tags rather than
// at one.
func (v forkVersion) isDevBuild() bool { return v.dev > 0 }

func (v forkVersion) String() string {
	s := "v" + strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) + "." +
		strconv.Itoa(v.patch) + "-desuq." + strconv.Itoa(v.rev)
	if v.dev > 0 {
		s += "+" + strconv.Itoa(v.dev)
	}
	return s
}

// peerVersion is one connected device and what it is running.
type peerVersion struct {
	name    string
	version string
}

// newestPeerAhead picks the connected device running the newest version of
// this fork, if any of them is ahead of `mine`.
//
// Returns the raw version string the peer reported rather than the parsed
// form, because that string is what goes in the toast and what the person will
// be looking for on the releases page.
func newestPeerAhead(mine string, peers []peerVersion) (peerVersion, bool) {
	self, ok := parseForkVersion(mine)
	if !ok {
		// Not a build of this fork at all. Nothing sensible to compare.
		return peerVersion{}, false
	}
	if self.isDevBuild() {
		// Somebody running a build they made themselves. They are the person
		// who cuts the releases, they know what they are running, and their
		// version legitimately sorts oddly against the tags. Nagging them is
		// pure noise.
		return peerVersion{}, false
	}

	var best peerVersion
	var bestV forkVersion
	found := false
	for _, p := range peers {
		v, ok := parseForkVersion(p.version)
		if !ok {
			// Stock Syncthing, or something else entirely. Never compared --
			// see the header.
			continue
		}
		if !v.newerThan(self) {
			continue
		}
		if !found || v.newerThan(bestV) {
			best, bestV, found = p, v, true
		}
	}
	return best, found
}
