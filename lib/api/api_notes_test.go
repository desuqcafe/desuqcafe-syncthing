// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork: see api_notes.go.

package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

func TestCleanNoteText(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"  moved the camera  ", "moved the camera", true},
		{"line one\r\nline two", "line one\nline two", true},
		{"bell\athere\x00", "bellthere", true},
		{strings.Repeat("é", noteMaxRunes), strings.Repeat("é", noteMaxRunes), true},
		{strings.Repeat("é", noteMaxRunes+1), strings.Repeat("é", noteMaxRunes+1), false},
		{"   ", "", true},
	}
	for _, c := range cases {
		got, ok := cleanNoteText(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("cleanNoteText(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestApplyNote(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	saved := time.Date(2026, 9, 26, 11, 30, 15, 123456789, time.UTC)

	list, changed := applyNote(nil, "Scenes/cabin.blend", saved, 1000, "moved the camera", now)
	if !changed || len(list) != 1 {
		t.Fatalf("adding: %+v changed=%v", list, changed)
	}
	if !list[0].Modified.Equal(saved.Truncate(time.Second)) {
		t.Fatalf("stored mtime %v, want it to the second", list[0].Modified)
	}

	// The same version, at a different sub-second precision -- which is what
	// the archive on another filesystem reports -- is the same note.
	list, changed = applyNote(list, "Scenes/cabin.blend", saved.Truncate(time.Millisecond), 1000, "moved the camera and the key light", now.Add(time.Minute))
	if !changed || len(list) != 1 || list[0].Text != "moved the camera and the key light" {
		t.Fatalf("editing: %+v changed=%v", list, changed)
	}

	// Saying the same thing again changes nothing, so nothing is rewritten
	// and re-sent.
	if _, changed = applyNote(list, "Scenes/cabin.blend", saved, 1000, "moved the camera and the key light", now); changed {
		t.Fatal("an identical note should not count as a change")
	}

	// A new save is a new version, with a note of its own.
	list, _ = applyNote(list, "Scenes/cabin.blend", saved.Add(time.Hour), 1200, "put the lighting back", now)
	if len(list) != 2 {
		t.Fatalf("second version: %+v", list)
	}

	// Removing one leaves the other.
	list, changed = applyNote(list, "Scenes/cabin.blend", saved, 1000, "", now)
	if !changed || len(list) != 1 || list[0].Size != 1200 {
		t.Fatalf("removing: %+v changed=%v", list, changed)
	}

	// Removing one that is not there is no change.
	if _, changed = applyNote(list, "Scenes/cabin.blend", saved, 999, "", now); changed {
		t.Fatal("removing a note that does not exist should change nothing")
	}
}

func TestApplyNoteKeepsWithinBounds(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	old := []noteEntry{{Path: "a.blend", Modified: now.Add(-500 * 24 * time.Hour), Size: 1, Text: "ancient", At: now.Add(-notesKeepFor - time.Hour)}}
	list, changed := applyNote(old, "b.blend", now, 2, "fresh", now)
	if !changed || len(list) != 1 || list[0].Path != "b.blend" {
		t.Fatalf("a note past keeping should go on the next write: %+v", list)
	}

	var many []noteEntry
	for i := range notesMaxPerDevice {
		many = append(many, noteEntry{Path: fmt.Sprintf("f%03d.blend", i), Modified: now, Size: 1, Text: "x", At: now.Add(time.Duration(i-notesMaxPerDevice) * time.Minute)})
	}
	list, _ = applyNote(many, "new.blend", now, 1, "newest", now)
	if len(list) != notesMaxPerDevice {
		t.Fatalf("kept %d notes, want the cap of %d", len(list), notesMaxPerDevice)
	}
	for _, n := range list {
		if n.Path == "f000.blend" {
			t.Fatal("the oldest note should have been the one dropped")
		}
	}
	if list[0].Path != "new.blend" {
		t.Fatalf("newest first after trimming, got %s", list[0].Path)
	}
}

func TestParseNotesDoc(t *testing.T) {
	dev := protocol.DeviceID{7}
	other := protocol.DeviceID{8}
	at := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	doc := notesFile{Version: 1, Device: dev.String(), Notes: []noteEntry{
		{Path: "Scenes/cabin.blend", Modified: at, Size: 10, Text: "fine", At: at},
		{Path: "../escape.blend", Modified: at, Size: 10, Text: "outside the folder", At: at},
		{Path: ".desuq-claims/x.json", Modified: at, Size: 10, Text: "bookkeeping", At: at},
		{Path: "b.blend", Modified: at, Size: 10, Text: "   ", At: at},
		{Path: "c.blend", Modified: at, Size: 10, Text: strings.Repeat("x", noteMaxRunes+1), At: at},
		{Path: "d.blend", Size: 10, Text: "no version", At: at},
	}}
	data, _ := json.Marshal(doc)

	got, ok := parseNotesDoc(data, dev)
	if !ok || len(got.Notes) != 1 || got.Notes[0].Path != "Scenes/cabin.blend" {
		t.Fatalf("parse: %+v ok=%v; want only the one sound entry", got, ok)
	}

	// A file that says it is somebody else's is not believed at all -- the
	// same rule as for marks.
	if _, ok := parseNotesDoc(data, other); ok {
		t.Fatal("a notes file naming another device should be refused")
	}
	if _, ok := parseNotesDoc([]byte("{not json"), dev); ok {
		t.Fatal("garbage should be refused")
	}
}

func TestPlaceNote(t *testing.T) {
	me := protocol.DeviceID{1}
	kai := protocol.DeviceID{2}
	saved := time.Date(2026, 9, 26, 11, 0, 0, 0, time.UTC)
	note := noteEntry{Path: "cabin.blend", Modified: saved, Size: 100}

	file := func(by protocol.DeviceID, mod time.Time, size int64, ver uint64) protocol.FileInfo {
		f := protocol.FileInfo{Name: "cabin.blend", Size: size, ModifiedBy: by.Short(), ModifiedS: mod.Unix(), ModifiedNs: int32(mod.Nanosecond())}
		f.Version = f.Version.Update(by.Short())
		for range ver {
			f.Version = f.Version.Update(by.Short())
		}
		return f
	}

	// Kai's note, Kai's version is current, and it has arrived here.
	g := file(kai, saved, 100, 0)
	v := versionState{global: g, globalOK: true, local: g, localOK: true, diskMod: saved, diskSize: 100, diskOK: true}
	if cur, here := placeNote(note, kai, false, v); !cur || !here {
		t.Fatalf("arrived: current=%v here=%v", cur, here)
	}

	// The note came first and the file is still on its way: current, not here.
	v.local, v.diskMod, v.diskSize = file(me, saved.Add(-time.Hour), 90, 0), saved.Add(-time.Hour), 90
	if cur, here := placeNote(note, kai, false, v); !cur || here {
		t.Fatalf("on its way: current=%v here=%v", cur, here)
	}

	// Same mtime and size but somebody else saved it: not this note's
	// version. Nobody's note is shown against another person's work.
	v.global = file(me, saved, 100, 0)
	if cur, _ := placeNote(note, kai, false, v); cur {
		t.Fatal("a note must not attach to a version its author did not save")
	}

	// Replaced since by a newer save: history.
	v.global = file(me, saved.Add(time.Hour), 120, 0)
	if cur, _ := placeNote(note, kai, false, v); cur {
		t.Fatal("a replaced version's note is not current")
	}

	// My own note about a save the scanner has not reached yet: the index
	// still has the old version, the disk has the new one.
	old := file(me, saved.Add(-time.Hour), 90, 0)
	v = versionState{global: old, globalOK: true, local: old, localOK: true, diskMod: saved.Add(300 * time.Millisecond), diskSize: 100, diskOK: true}
	if cur, here := placeNote(note, me, true, v); !cur || !here {
		t.Fatalf("mine, not yet scanned: current=%v here=%v", cur, here)
	}
	// ...which is not something anybody else's note gets to claim.
	if cur, _ := placeNote(note, kai, false, v); cur {
		t.Fatal("only the author's own disk can stand in for the index")
	}
}

func TestHistoryNotesFor(t *testing.T) {
	saved := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	notes := []noteRow{
		{Path: "cabin.blend", Modified: saved, Size: 100, Text: "good lighting"},
		{Path: "cabin.blend", Modified: saved.Add(time.Hour), Size: 100, Text: "broke the lighting"},
	}
	// The archive reports the mtime at whatever precision the disk keeps.
	got := historyNotesFor(notes, saved.Add(400*time.Millisecond), 100)
	if len(got) != 1 || got[0].Text != "good lighting" {
		t.Fatalf("matched %+v", got)
	}
	if got := historyNotesFor(notes, saved, 101); len(got) != 0 {
		t.Fatalf("a different size is a different version: %+v", got)
	}
}

func TestNotesFileIsClaimsBookkeeping(t *testing.T) {
	dev := protocol.DeviceID{9}
	name := notesFileName(dev)
	if !isClaimsPath(name) {
		t.Fatalf("%s should be inside the claims directory, so every view that leaves marks out leaves notes out too", name)
	}
	if !isNotesName(name[len(claimsDir)+1:]) {
		t.Fatal("isNotesName should recognise its own file name")
	}
	// A notes file must never be read as a claims file: the claims reader
	// takes "<id>.json", and "<id>.notes" is not a device ID.
	if _, err := protocol.DeviceIDFromString(strings.TrimSuffix(name[len(claimsDir)+1:], ".json")); err == nil {
		t.Fatal("a notes file name parses as a device ID")
	}
}
