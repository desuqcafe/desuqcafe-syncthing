package main

import (
	"strings"
	"testing"
	"time"
)

func remoteEvent(id int, path, action, by string) diskEvent {
	var ev diskEvent
	ev.ID = id
	ev.Type = "RemoteChangeDetected"
	ev.Data.Folder = "assets"
	ev.Data.Path = path
	ev.Data.Type = "file"
	ev.Data.Action = action
	ev.Data.ModifiedBy = by
	return ev
}

func TestBriefingWanted(t *testing.T) {
	start := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	at := start.Add(3 * time.Hour)
	if briefingWanted(at.Add(-time.Minute), true, at, start) {
		t.Error("a one-minute blip was briefed")
	}
	if !briefingWanted(at.Add(-2*time.Hour), true, at, start) {
		t.Error("two hours apart was not briefed")
	}
	// The first connection after signing in: nothing is known, and it is the
	// commonest reunion there is.
	if !briefingWanted(time.Time{}, false, start.Add(time.Minute), start) {
		t.Error("the first connection after start was not briefed")
	}
	// A connection never seen to drop, hours into the session, is the event
	// stream coming back, not a reunion.
	if briefingWanted(time.Time{}, false, at, start) {
		t.Error("an unexplained connection mid-session was briefed")
	}
}

func TestChangesByAttributesAndSkipsBookkeeping(t *testing.T) {
	evs := []diskEvent{
		remoteEvent(11, `scenes\cabin.blend`, "modified", "KAI00"),
		remoteEvent(12, "old.png", "deleted", "KAI00"),
		remoteEvent(13, "mia.blend", "modified", "MIA0000"),
		remoteEvent(14, `.desuq-claims\KAI.json`, "modified", "KAI00"),
		remoteEvent(15, "cabin.sync-conflict-20260926-150211-KAI00.blend", "modified", "KAI00"),
		remoteEvent(16, `scenes\cabin.blend`, "modified", "KAI00"),
	}
	got, floor := changesBy(evs, "KAI00", 10)
	if floor {
		t.Error("a complete feed was reported as short")
	}
	if len(got) != 2 || got[0].path != "scenes/cabin.blend" || !got[1].deleted {
		t.Errorf("changesBy = %+v", got)
	}
	if _, floor := changesBy(evs, "KAI00", 5); !floor {
		t.Error("a feed that had dropped events past the cursor was not reported as short")
	}
}

func TestBriefingMessage(t *testing.T) {
	if title, _ := briefingMessage("Kai", nil, false, nil, nil); title != "" {
		t.Errorf("nothing happened, but got %q", title)
	}

	theirs := []theirChange{
		{path: "scenes/cabin.blend"}, {path: "rig.blend"}, {path: "tree.blend"},
		{path: "rock.blend"}, {path: "old.png", deleted: true},
		{path: "texture1.png"}, // also in conflict below; said once, as the conflict
	}
	conflicts := []conflictFile{
		{name: "texture1.sync-conflict-20260926-150211-KAI00.png"},
		{name: "texture2.sync-conflict-20260926-150211-ME00000.png", gone: true},
	}
	title, body := briefingMessage("Kai", theirs, false, conflicts, nil)
	if title != "Back in touch with Kai" {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{
		"You both changed texture1.png",
		"texture2.png was deleted on one side and changed on the other",
		"They also changed cabin.blend, rig.blend, rock.blend and 1 other file",
		"deleted old.png",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "texture1.png") != 1 {
		t.Errorf("a file in conflict was also listed as simply changed:\n%s", body)
	}

	_, body = briefingMessage("Kai", theirs[:2], true, nil, nil)
	if !strings.Contains(body, "While you were apart they changed at least 2 files") {
		t.Errorf("a short feed did not say 'at least':\n%s", body)
	}
}
