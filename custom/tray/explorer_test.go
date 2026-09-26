package main

import (
	"strings"
	"testing"
	"time"
)

type whoPeer = struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Has       bool   `json:"has"`
	HeldBack  bool   `json:"heldBack"`
}

func TestWhoMessage(t *testing.T) {
	now := time.Date(2026, 9, 26, 16, 0, 0, 0, time.Local)
	claims := []claimRow{{Name: "Kai", Path: "cabin.blend", Since: now.Add(-time.Hour)}}
	wh := whoHas{Exists: true, ModifiedBy: "Kai", Peers: []whoPeer{
		{Name: "Mia", Connected: false},
		{Name: "Kai", Connected: true, Has: true},
	}}
	title, body := whoMessage("cabin.blend", claims, wh, now)
	if title != "cabin.blend" {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{
		"Kai is working on it, since 15:00.",
		"Kai has your version.",
		"Mia is offline and does not have your version yet.",
		"Last changed by Kai.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}

	// Everybody has it: say "the same version", and that nobody is on it.
	wh.Peers[0] = whoPeer{Name: "Mia", Connected: true, Has: true}
	_, body = whoMessage("cabin.blend", nil, wh, now)
	if !strings.Contains(body, "Nobody has marked it") || !strings.Contains(body, "Mia and Kai have the same version as you.") {
		t.Errorf("all current:\n%s", body)
	}

	// This computer is the one behind: "they do not have your version" would
	// be the wrong way round.
	wh.Behind = true
	_, body = whoMessage("cabin.blend", nil, wh, now)
	if !strings.Contains(body, "newer version by Kai is on its way to you") || strings.Contains(body, "your version") {
		t.Errorf("behind:\n%s", body)
	}
}

func TestSinceWords(t *testing.T) {
	now := time.Date(2026, 9, 26, 16, 0, 0, 0, time.Local) // a Saturday
	cases := map[time.Time]string{
		now.Add(-2 * time.Hour):       "since 14:00",
		now.Add(-3 * 24 * time.Hour):  "since Wednesday",
		now.Add(-20 * 24 * time.Hour): "since 6 September",
	}
	for in, want := range cases {
		if got := sinceWords(in, now); got != want {
			t.Errorf("sinceWords(%v) = %q, want %q", in, got, want)
		}
	}
}
