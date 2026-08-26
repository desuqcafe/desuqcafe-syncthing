package main

import (
	"testing"
	"time"
)

func TestPauseHoldFires(t *testing.T) {
	var h pauseHold
	done := make(chan struct{})

	h.arm(20*time.Millisecond, func() { close(done) })
	if h.deadline().IsZero() {
		t.Fatal("armed hold has no deadline")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the hold never expired")
	}

	// Cleared before the callback runs, so anything the callback does that
	// reads the deadline -- the menu title, for one -- sees the hold as over.
	if !h.deadline().IsZero() {
		t.Error("deadline outlived the expiry")
	}
	if h.cancel() {
		t.Error("cancel reported a hold that had already fired")
	}
}

// A person pressing Resume outranks a timer they set earlier: the callback
// must not fire afterwards, or syncing would be resumed twice and -- worse --
// a toast would announce it long after they turned it off by hand.
func TestPauseHoldCancelStopsTheCallback(t *testing.T) {
	var h pauseHold
	fired := make(chan struct{}, 1)

	h.arm(30*time.Millisecond, func() { fired <- struct{}{} })
	if !h.cancel() {
		t.Fatal("cancel did not report an armed hold")
	}
	if !h.deadline().IsZero() {
		t.Error("deadline survived cancel")
	}

	select {
	case <-fired:
		t.Error("the callback ran after cancel")
	case <-time.After(200 * time.Millisecond):
	}
}

// Choosing a second duration replaces the first rather than stacking, so two
// clicks cannot leave a stray timer that resumes syncing an hour early.
func TestPauseHoldRearmReplaces(t *testing.T) {
	var h pauseHold
	first := make(chan struct{}, 1)
	second := make(chan struct{}, 1)

	h.arm(30*time.Millisecond, func() { first <- struct{}{} })
	h.arm(60*time.Millisecond, func() { second <- struct{}{} })

	select {
	case <-first:
		t.Fatal("the replaced timer still fired")
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("the second hold never expired")
	}
}

func TestPauseHoldTitle(t *testing.T) {
	if got := pauseHoldTitle(time.Time{}); got != "Resume Syncing" {
		t.Errorf("no hold: %q", got)
	}
	at := time.Date(2026, 8, 26, 15, 40, 0, 0, time.Local)
	if got := pauseHoldTitle(at); got != "Resume Syncing (paused until 15:40)" {
		t.Errorf("with a hold: %q", got)
	}
}

// The offered durations are short on purpose -- this is "leave me alone while
// I render", not a scheduler. A four-hour maximum is the design, so a change
// to it should be a deliberate one.
func TestPauseChoicesAreShort(t *testing.T) {
	if len(pauseChoices) == 0 {
		t.Fatal("no pause choices")
	}
	for _, c := range pauseChoices {
		if c.d <= 0 || c.d > 4*time.Hour {
			t.Errorf("%q is %v, which is outside the intended range", c.label, c.d)
		}
	}
}
