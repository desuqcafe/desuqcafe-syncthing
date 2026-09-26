package main

import (
	"strings"
	"testing"
	"time"
)

func saveEvent(folder, path, action string) diskEvent {
	var ev diskEvent
	ev.Type = "LocalChangeDetected"
	ev.Data.Folder = folder
	ev.Data.Path = path
	ev.Data.Type = "file"
	ev.Data.Action = action
	return ev
}

func TestAutoClaimableIsOnlyABlendSomebodySaved(t *testing.T) {
	cases := map[string]bool{
		`scenes\cabin.blend`: true,
		"cabin.BLEND":        true,
		"cabin.blend1":       false, // Blender's own backup
		"cabin.blend@":       false, // Blender's temporary, mid-save
		"texture.png":        false,
		`scenes\cabin.sync-conflict-20260926-150211-OHQN3WH.blend`: false,
		`.desuq-claims\ABC.json`: false,
	}
	for p, want := range cases {
		if got := autoClaimable(p); got != want {
			t.Errorf("autoClaimable(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSavedBlendsReadsSavesNotScans(t *testing.T) {
	evs := []diskEvent{
		saveEvent("a", `scenes\cabin.blend`, "modified"),
		saveEvent("a", `scenes\cabin.blend`, "modified"), // saved twice in one pass
		saveEvent("a", "rig.blend", "deleted"),           // deleting is not working on it
		saveEvent("a", "notes.txt", "modified"),
	}
	remote := saveEvent("a", "tree.blend", "modified")
	remote.Type = "RemoteChangeDetected" // pulled, not saved here
	evs = append(evs, remote)

	// A folder reporting a burst is a first scan, not somebody saving.
	for i := range autoClaimBurst + 1 {
		evs = append(evs, saveEvent("b", "scene"+string(rune('a'+i))+".blend", "modified"))
	}

	got := savedBlends(evs)
	if len(got["a"]) != 1 || got["a"][0] != "scenes/cabin.blend" {
		t.Errorf("folder a: %v, want only scenes/cabin.blend", got["a"])
	}
	if _, ok := got["b"]; ok {
		t.Errorf("a burst of %d files was read as saves", autoClaimBurst+1)
	}
}

func TestAutoReleaseDue(t *testing.T) {
	now := time.Date(2026, 9, 26, 18, 0, 0, 0, time.UTC)
	since := now.Add(-6 * time.Hour)
	if !autoReleaseDue(since, now.Add(-autoClaimQuiet-time.Minute), now) {
		t.Error("a file left alone past the quiet time kept its mark")
	}
	if autoReleaseDue(since, now.Add(-time.Hour), now) {
		t.Error("a file saved an hour ago lost its mark")
	}
	if !autoReleaseDue(since, time.Time{}, now) {
		t.Error("a file that is gone kept its mark")
	}
	// Marked a moment ago over an old file: the mark is what is recent.
	if autoReleaseDue(now.Add(-time.Minute), now.Add(-48*time.Hour), now) {
		t.Error("a fresh mark on an old file came straight off")
	}
}

// The toast after marking used to say "everyone can see that now" whoever
// was switched off. Offline and not-taking-marks are said; a connected peer
// who has not pulled it yet is a second's wait and is not.
func TestReachSentence(t *testing.T) {
	if s := reachSentence(nil); s != "" {
		t.Errorf("everybody reached, got %q", s)
	}
	if s := reachSentence([]claimPeer{{Name: "Mia", State: "sending"}}); s != "" {
		t.Errorf("a mark on its way to a connected peer was reported: %q", s)
	}
	s := reachSentence([]claimPeer{
		{Name: "Kai", State: "offline"},
		{Name: "Mia", State: "heldBack"},
	})
	if !strings.Contains(s, "Kai is offline") || !strings.Contains(s, "Mia cannot see marks") {
		t.Errorf("reachSentence = %q", s)
	}
	s = reachSentence([]claimPeer{{Name: "Kai", State: "offline"}, {Name: "Mia", State: "offline"}})
	if !strings.Contains(s, "Kai and Mia are offline") {
		t.Errorf("two offline: %q", s)
	}
}

func TestPausedClaimsMessage(t *testing.T) {
	if title, _ := pausedClaimsMessage([]claimRow{{Name: "Kai", Path: "a.blend"}}); title != "" {
		t.Errorf("somebody else's mark raised a pause warning: %q", title)
	}
	title, body := pausedClaimsMessage([]claimRow{{Mine: true, Path: "scenes/cabin.blend"}})
	if !strings.Contains(title, "cabin.blend") || !strings.Contains(body, "resume") {
		t.Errorf("pause warning: %q / %q", title, body)
	}
}
