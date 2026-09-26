// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork: see api_presence.go.

package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

func TestNewestPerDevice(t *testing.T) {
	kai, mia, me := protocol.DeviceID{2}, protocol.DeviceID{3}, protocol.DeviceID{1}
	want := map[protocol.ShortID]protocol.DeviceID{kai.Short(): kai, mia.Short(): mia}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	files := []recentFile{
		{`Scenes\cabin.blend`, now.Add(-time.Minute)},
		{"mine.blend", now.Add(-2 * time.Minute)},
		{"tree.blend", now.Add(-3 * time.Minute)},
		{"old-kai.blend", now.Add(-time.Hour)},
		{"rock.blend", now.Add(-2 * time.Hour)},
	}
	by := map[string]protocol.ShortID{
		`Scenes\cabin.blend`: kai.Short(), "mine.blend": me.Short(), "tree.blend": kai.Short(),
		"old-kai.blend": kai.Short(), "rock.blend": mia.Short(),
	}
	asked := 0
	got := newestPerDevice(files, want, func(n string) (protocol.ShortID, bool) {
		asked++
		s, ok := by[n]
		return s, ok
	})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Device != kai.String() || got[0].Path != "Scenes/cabin.blend" {
		t.Errorf("Kai's newest: %+v; want cabin.blend with a slash path", got[0])
	}
	if got[1].Device != mia.String() || got[1].Path != "rock.blend" {
		t.Errorf("Mia's newest: %+v", got[1])
	}
	if asked != 5 {
		t.Errorf("asked about %d files; it should stop once everybody is found, at 5", asked)
	}

	// Somebody with nothing recent is left out rather than guessed at, and
	// the lookups are bounded however many files there are.
	var many []recentFile
	for i := range 1000 {
		many = append(many, recentFile{fmt.Sprintf("f%d", i), now})
	}
	asked = 0
	got = newestPerDevice(many, want, func(string) (protocol.ShortID, bool) { asked++; return me.Short(), true })
	if len(got) != 0 || asked != lastSavedMaxLookups {
		t.Fatalf("got %+v after %d lookups; want nothing after %d", got, asked, lastSavedMaxLookups)
	}
}
