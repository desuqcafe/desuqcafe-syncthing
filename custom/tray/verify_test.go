package main

import (
	"strings"
	"testing"
	"time"
)

func TestFreshKnocks(t *testing.T) {
	now := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	told := map[string]time.Time{}
	knocks := map[string]knock{
		"KAIKAIK-AAAAAAA": {Name: "DESKTOP-K41", At: now.Add(-10 * time.Second)},
		"MIAMIAM-BBBBBBB": {Name: "Mia's laptop", At: now.Add(-5 * time.Second)},
	}
	names := map[string]string{"KAIKAIK-AAAAAAA": "Kai"}

	got := freshKnocks(knocks, told, names, now, knockCooldown)
	if len(got) != 2 {
		t.Fatalf("want a toast per device, got %d", len(got))
	}
	if !strings.HasPrefix(got[0].Title, "Kai ") {
		t.Errorf("this computer's name for them comes first: %q", got[0].Title)
	}
	if !strings.HasPrefix(got[1].Title, "Mia's laptop ") {
		t.Errorf("the name they sent is the fallback: %q", got[1].Title)
	}
	if !strings.Contains(got[0].Body, "compare cards") {
		t.Errorf("the toast should say what to do: %q", got[0].Body)
	}

	// They knock again a minute later, as their computer does all day.
	if again := freshKnocks(knocks, told, names, now.Add(time.Minute), knockCooldown); len(again) != 0 {
		t.Errorf("the same device must not toast inside the cooldown, got %d", len(again))
	}
	if later := freshKnocks(knocks, told, names, now.Add(knockCooldown+time.Minute), knockCooldown); len(later) != 2 {
		t.Errorf("after the cooldown they are worth saying again, got %d", len(later))
	}
}

// An unverified device is refused by the daemon on purpose, so its silence is
// not "stopped syncing" and must not be reported as such.
func TestStaleSkipsUnverified(t *testing.T) {
	now := time.Now()
	v := stalePeers([]peerSeen{
		{id: "A", name: "Kai", lastSeen: now.Add(-10 * 24 * time.Hour), unverified: true},
		{id: "B", name: "Mia", lastSeen: now.Add(-10 * 24 * time.Hour)},
	}, now, 3*24*time.Hour)
	if len(v.Stale) != 1 || v.Stale[0].name != "Mia" {
		t.Errorf("only the verified device is judged, got %+v", v.Stale)
	}
	if v.Eligible != 1 {
		t.Errorf("an unverified device is not eligible, got %d", v.Eligible)
	}
}
