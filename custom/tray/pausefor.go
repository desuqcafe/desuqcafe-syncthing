package main

// "Pause for an hour" -- and start again by itself.
//
// WHY THIS EXISTS
//
// The tray has had a Pause Syncing checkbox since wave 1, and it pauses until
// somebody unpauses it. The reason people press it is not open-ended: a render
// is running, or a big import is going, and they want the disk and the network
// to themselves for a while. Then they forget, because a paused Syncthing
// looks exactly like a working one from inside Blender.
//
// That is the same failure as every other one this fork keeps finding -- sync
// stops and nothing says so -- except this time the user caused it and is
// therefore the last person to suspect it. A pause that ends on its own cannot
// have that failure mode.
//
// WHY THE TIMER IS NOT PERSISTED
//
// It lives in this process and nowhere else, and quit() lifts the pause before
// the tray goes away. So the promise "for one hour" is kept by the tray being
// alive, and if the tray is not alive the daemon is not either -- it is the
// tray that supervises it. The one case left is -attach, where the daemon
// outlives us: there quit() still resumes on the way out, which is why it is
// done there rather than left to the timer.
//
// Persisting it to disk was the alternative, and it costs more than it buys:
// the tray deliberately keeps no state on disk (see conflicts.go), and a
// stored deadline that outlives a crash would have to be reconciled against a
// pause somebody set by hand in the GUI, which this cannot see.

import (
	"sync"
	"time"
)

// pauseHold is a pause with an end. Zero value is "not holding".
type pauseHold struct {
	mu    sync.Mutex
	until time.Time
	timer *time.Timer
}

// arm starts (or replaces) the hold. onExpiry runs once, in its own goroutine,
// when the time is up -- not if the hold is cancelled first.
func (p *pauseHold) arm(d time.Duration, onExpiry func()) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
	}
	p.until = time.Now().Add(d)
	p.timer = time.AfterFunc(d, func() {
		// Clear before running, so anything onExpiry does that reads the
		// deadline -- the menu title, for one -- sees the hold as over.
		p.mu.Lock()
		p.until = time.Time{}
		p.timer = nil
		p.mu.Unlock()
		onExpiry()
	})
}

// cancel drops the hold without running onExpiry, and says whether there was
// one. Every manual pause or resume goes through here first: a person pressing
// the button is a decision that outranks a timer they set ten minutes ago.
func (p *pauseHold) cancel() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer == nil {
		return false
	}
	p.timer.Stop()
	p.timer = nil
	p.until = time.Time{}
	return true
}

// deadline is when the hold ends, or the zero time if there is no hold.
func (p *pauseHold) deadline() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.until
}

// pauseChoice is one entry in the "Pause for a while" submenu.
type pauseChoice struct {
	label string
	tip   string
	d     time.Duration
}

// pauseChoices are deliberately few and short. This is "leave me alone while I
// render", not a scheduler: anything longer than an afternoon is better served
// by the plain checkbox, which is honest about being indefinite.
var pauseChoices = []pauseChoice{
	{"Pause for 1 hour", "Stop syncing, then start again by itself in an hour", time.Hour},
	{"Pause for 4 hours", "Stop syncing, then start again by itself in four hours", 4 * time.Hour},
}

// pauseHoldTitle is the Resume item's label, which is where the deadline is
// shown. A pause with an end has to say when, or it is indistinguishable from
// the pause that has none -- which is the whole point of having both.
func pauseHoldTitle(until time.Time) string {
	if until.IsZero() {
		return "Resume Syncing"
	}
	return "Resume Syncing (paused until " + until.Format("15:04") + ")"
}
