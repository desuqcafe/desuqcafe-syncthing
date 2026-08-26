package main

// "Kai has not synced for four days."
//
// WHY THIS EXISTS
//
// The failure this fork keeps meeting is not a sync that goes wrong, it is a
// sync that quietly stops. Defender quarantining the tray took both shortcuts
// and start-at-sign-in with it, so a machine stopped syncing at its next
// reboot and nothing anywhere said so -- see DEPLOYMENT-3D-TEAM.md section 20.
// The machine it happened to is by definition the one that cannot warn
// anybody: its tray is the thing that was eaten.
//
// The other side can. Every device already knows when it last spoke to each of
// its peers, and a peer that has been silent for days is worth one toast --
// not because anything is broken here, but because "I sent you the textures on
// Tuesday" is otherwise believed by both people right up until a deadline.
//
// WHY THE WORDING IS SO CAREFUL
//
// This alert cannot tell whose fault it is. If nothing at all has connected
// for four days, the likeliest explanation is that *this* computer has been
// off the network, and a toast blaming Kai for that is worse than silence.
// So there are three sentences, chosen by how much the evidence actually
// supports:
//
//   - some peers connected, one is not:  it really is their computer.
//   - nothing has connected, one peer:   say it as a pair, blame nobody.
//   - nothing has connected, several:    say this computer may be offline.
//
// Same discipline as the main screen's headline rules: a sentence derived from
// one side's state alone is usually a lie about a shared folder.

import (
	"sort"
	"time"
)

const (
	// staleAfter is how long a peer may be silent before it is worth saying
	// something. Three days rather than one so that a normal weekend -- a
	// modeller's machine off from Friday evening to Monday morning -- never
	// produces a toast.
	staleAfter = 72 * time.Hour

	// staleCooldown is per device. A machine that has been away four days is
	// still away on the fifth, and repeating it daily is how people learn to
	// dismiss notifications without reading them.
	staleCooldown = 72 * time.Hour

	// staleCheckInterval is the poll. Nothing about this is urgent: the
	// condition it detects is measured in days.
	staleCheckInterval = 6 * time.Hour

	// staleFirstCheck delays the first look so that connections opened at
	// start-up have completed. A peer that is connecting right now is not a
	// peer that has been away.
	staleFirstCheck = 5 * time.Minute
)

// peerSeen is one configured device, as much as this check needs to know.
type peerSeen struct {
	id        string
	name      string
	connected bool
	paused    bool
	// lastSeen is the Unix epoch for a device that has never connected at all
	// -- see seenEver, and note that it is NOT Go's zero time. Those devices
	// are excluded: one added an hour ago and not yet accepted at the other
	// end is the setup story, which the first-run guide owns, and saying "has
	// not synced for 3 days" about a device that has never synced at all would
	// be true and useless.
	lastSeen time.Time
}

// staleVerdict is what the evidence supports.
type staleVerdict struct {
	// Stale is the silent devices, oldest contact first.
	Stale []peerSeen
	// Eligible is how many devices were in a position to be judged at all --
	// not paused, and seen at least once.
	Eligible int
	// All says every eligible device is stale, which is the case that points
	// at this computer rather than at them.
	All bool
	// Oldest is the longest silence in the set.
	Oldest time.Duration
}

// stalePeers applies the rules. Pure, so the thing that decides whether to
// blame somebody is provable without a network.
func stalePeers(peers []peerSeen, now time.Time, after time.Duration) staleVerdict {
	var v staleVerdict
	for _, p := range peers {
		if p.paused || !seenEver(p.lastSeen) {
			continue
		}
		v.Eligible++
		if p.connected {
			continue
		}
		silence := now.Sub(p.lastSeen)
		if silence < after {
			continue
		}
		if silence > v.Oldest {
			v.Oldest = silence
		}
		v.Stale = append(v.Stale, p)
	}

	sort.Slice(v.Stale, func(i, j int) bool {
		if !v.Stale[i].lastSeen.Equal(v.Stale[j].lastSeen) {
			return v.Stale[i].lastSeen.Before(v.Stale[j].lastSeen)
		}
		return v.Stale[i].name < v.Stale[j].name
	})
	v.All = len(v.Stale) > 0 && len(v.Stale) == v.Eligible
	return v
}

// staleMessage writes the toast. Returns empty strings for a verdict with
// nothing in it.
func staleMessage(v staleVerdict) (title, body string) {
	if len(v.Stale) == 0 {
		return "", ""
	}

	span := staleSpan(v.Oldest)
	names := make([]string, 0, len(v.Stale))
	for _, p := range v.Stale {
		names = append(names, p.name)
	}
	who := joinNames(names)
	last := v.Stale[0].lastSeen.Format("2 January")
	// joinNames collapses three or more into "Kai and 2 others", so the
	// count decides the verb rather than the string.
	verb := "has"
	if len(v.Stale) > 1 {
		verb = "have"
	}

	switch {
	case !v.All:
		// Others are connected right now, so this is about them and can be
		// said plainly.
		return who + " " + verb + " not synced for " + span,
			"Last connected on " + last + ". Everything else is connected, so " +
				"it is that computer which is not reaching the network. " +
				"Anything you have changed is waiting for it."

	case len(v.Stale) == 1:
		// One peer and nothing connected: no way to tell which end it is.
		return "You and " + who + " have not synced for " + span,
			"Last connected on " + last + ". Either computer being offline " +
				"looks like this. Nothing has been lost, but nothing is arriving either."

	default:
		return "Nothing has synced for " + span,
			who + " have all been out of contact since " + last +
				". When it is everybody at once it is usually this computer that is offline."
	}
}

// seenEver reports whether a device has ever connected.
//
// /rest/stats/device answers with the Unix epoch, not Go's zero time, for a
// device that has not -- so IsZero alone is false and the device reads as
// fifty-six years silent. Found against a live instance rather than reasoned
// about; the fixture that would have been written by hand had the zero time in
// it, which is the whole reason the live test exists.
//
// The cutoff is deliberately loose. Any timestamp before Syncthing existed
// means the same thing whichever sentinel produced it.
func seenEver(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 2000
}

// staleSpan is "4 days", or "less than a day" for a silence that has not
// reached one -- reachable only from a test, since the threshold is three
// days. plural and joinNames are conflicts.go's and status.go's; a second
// spelling of "Kai and 2 others" in the same tray would be the kind of
// divergence nobody chose.
func staleSpan(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	if days <= 0 {
		return "less than a day"
	}
	return plural(days, "day")
}
