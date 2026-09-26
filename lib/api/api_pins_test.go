// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork: see api_pins.go.

package api

import (
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/versioner"
)

func TestHistoryCountPinned(t *testing.T) {
	at := time.Date(2026, 9, 26, 15, 2, 11, 0, time.Local)
	tag := func(t time.Time) string { return t.Format(versioner.TimeFormat) }

	versions := map[string][]versioner.FileVersion{
		"Scenes/cabin.blend":   {{VersionTime: at}, {VersionTime: at.Add(time.Hour)}},
		"tex.png":              {{VersionTime: at}},
		".desuq-claims/X.json": {{VersionTime: at}},
	}
	pinned := versioner.PinSet{
		"Scenes/cabin.blend": {tag(at): {}, tag(at.Add(time.Hour)): {}},
		// A pin whose copy has gone protects nothing and is not counted.
		"tex.png": {tag(at.Add(-time.Hour)): {}},
		// Bookkeeping files are hidden from History, so not counted either.
		".desuq-claims/X.json": {tag(at): {}},
		"gone.png":             {tag(at): {}},
	}
	rows := []historyFile{{Name: "Scenes/cabin.blend"}, {Name: "tex.png"}}

	if total := historyCountPinned(versions, pinned, rows); total != 2 {
		t.Errorf("total pinned = %d, want 2", total)
	}
	if rows[0].Pinned != 2 || rows[1].Pinned != 0 {
		t.Errorf("row pins = %d, %d; want 2, 0", rows[0].Pinned, rows[1].Pinned)
	}
}

func TestHasVersionMatchesToTheSecond(t *testing.T) {
	at := time.Date(2026, 9, 26, 15, 2, 11, 0, time.Local)
	list := []versioner.FileVersion{{VersionTime: at}}
	if !hasVersion(list, at.Add(400*time.Millisecond).UTC()) {
		t.Error("same second in another zone did not match")
	}
	if hasVersion(list, at.Add(time.Second)) {
		t.Error("a different second matched")
	}
}
