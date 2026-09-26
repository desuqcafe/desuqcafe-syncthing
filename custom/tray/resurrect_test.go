package main

import (
	"strings"
	"testing"
	"time"
)

func localEvent(path, action string, at time.Time) diskEvent {
	var ev diskEvent
	ev.Type = "LocalChangeDetected"
	ev.Time = at
	ev.Data.Folder = "assets"
	ev.Data.Path = path
	ev.Data.Type = "file"
	ev.Data.Action = action
	return ev
}

// Delete here, restart, the file comes back from somebody else's later edit:
// the memo has to survive the restart, and has to say it once.
func TestDeletedMemoFindsAResurrectionAcrossARestart(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)

	d := loadDeletedMemo(home)
	if back := d.observe([]diskEvent{localEvent(`maps\texture2.png`, "deleted", now)}, now); len(back) != 0 {
		t.Fatalf("a deletion was reported as a return: %v", back)
	}

	d = loadDeletedMemo(home) // the tray restarted
	evs := []diskEvent{remoteEvent(1, `maps\texture2.png`, "modified", "YUKI000")}
	back := d.observe(evs, now.Add(24*time.Hour))
	if len(back) != 1 || back[0].path != "maps/texture2.png" || back[0].by != "YUKI000" {
		t.Fatalf("resurrection not found after a restart: %+v", back)
	}
	if again := d.observe(evs, now.Add(24*time.Hour)); len(again) != 0 {
		t.Errorf("the same return was reported twice")
	}
}

func TestDeletedMemoForgetsWhatWasSettled(t *testing.T) {
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	d := loadDeletedMemo(t.TempDir())
	d.observe([]diskEvent{
		localEvent("a.png", "deleted", now),
		localEvent("b.png", "deleted", now),
		localEvent("c.png", "deleted", now.Add(-deletedMemoKeep-time.Hour)),
	}, now)
	// Put back by hand here, and deleted everywhere by somebody else: neither
	// is news when it later changes.
	d.observe([]diskEvent{
		localEvent("a.png", "modified", now),
		remoteEvent(1, "b.png", "deleted", "YUKI000"),
	}, now)
	back := d.observe([]diskEvent{
		remoteEvent(2, "a.png", "modified", "YUKI000"),
		remoteEvent(3, "b.png", "modified", "YUKI000"),
		remoteEvent(4, "c.png", "modified", "YUKI000"),
	}, now)
	if len(back) != 0 {
		t.Errorf("settled or expired deletions were reported: %+v", back)
	}
}

func TestResurrectionMessage(t *testing.T) {
	names := map[string]string{"YUKI000": "Yuki"}
	labels := map[string]string{"assets": "Project Assets"}
	title, body := resurrectionMessage([]resurrected{{folder: "assets", path: "maps/texture2.png", by: "YUKI000"}}, names, labels)
	if title != "texture2.png came back" ||
		!strings.Contains(body, "You deleted it in Project Assets, but Yuki had changed it") {
		t.Errorf("one: %q / %q", title, body)
	}
	title, _ = resurrectionMessage([]resurrected{{path: "a.png"}, {path: "b.png"}}, names, labels)
	if title != "2 files you deleted came back" {
		t.Errorf("two: %q", title)
	}
}

// Syncthing 2 logs DeviceConnected once per connection, and opens several.
// Only the first may start a briefing.
func TestASecondConnectionIsNotAReunion(t *testing.T) {
	a, _ := testAlerter()
	a.current = func() *client { return nil } // brief() returns at once
	start := a.started
	a.peerDisconnected("PEER", start.Add(time.Hour))
	a.peerConnected("PEER", start.Add(time.Hour+time.Minute)) // a blip: no briefing
	a.peerConnected("PEER", start.Add(time.Hour+time.Minute+2*time.Second))
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.briefing) != 0 {
		t.Errorf("a sibling connection started a briefing")
	}
	if !a.connected["PEER"] {
		t.Errorf("the peer is not recorded as connected")
	}
}
