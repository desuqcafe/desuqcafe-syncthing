// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_conflicts.go.
//
// conflictParse decides which file gets overwritten when somebody clicks "use
// the copy set aside", so it is tested the way reclaimVerdict is: as a pure
// function, with the awkward names spelled out rather than generated.

package api

import (
	"testing"
	"time"
)

func TestConflictParse(t *testing.T) {
	when := time.Date(2026, 8, 24, 3, 29, 16, 0, time.Local)

	cases := []struct {
		name     string
		in       string
		original string
		when     time.Time
		short    string
	}{
		{
			name:     "a name Syncthing made",
			in:       "scene.sync-conflict-20260824-032916-F67Q3OS.blend",
			original: "scene.blend",
			when:     when,
			short:    "F67Q3OS",
		},
		{
			// Index names carry the OS separator, and on Windows that is a
			// backslash. Both are cut, and whichever came in is what goes
			// back out -- the result is handed to the filesystem.
			name:     "in a subdirectory, backslash",
			in:       `refs\chair.sync-conflict-20260824-032916-F67Q3OS.png`,
			original: `refs\chair.png`,
			when:     when,
			short:    "F67Q3OS",
		},
		{
			name:     "in a subdirectory, slash",
			in:       "refs/chair.sync-conflict-20260824-032916-F67Q3OS.png",
			original: "refs/chair.png",
			when:     when,
			short:    "F67Q3OS",
		},
		{
			name:     "no extension",
			in:       "notes.sync-conflict-20260824-032916-F67Q3OS",
			original: "notes",
			when:     when,
			short:    "F67Q3OS",
		},
		{
			// Older Syncthing wrote no device into the name. Still resolvable:
			// the fork never reads the device out of the name anyway.
			name:     "no device in the name",
			in:       "scene.sync-conflict-20260824-032916.blend",
			original: "scene.blend",
			when:     when,
		},
		{
			// The last marker wins, so the file this is a conflict *of* is the
			// intermediate copy, not the original at the bottom.
			name:     "a conflict copy of a conflict copy",
			in:       "scene.sync-conflict-20260824-032916-AAAAAAA.sync-conflict-20260825-101500-BBBBBBB.blend",
			original: "scene.sync-conflict-20260824-032916-AAAAAAA.blend",
			when:     time.Date(2026, 8, 25, 10, 15, 0, 0, time.Local),
			short:    "BBBBBBB",
		},
		{
			// Hand-made or truncated: still resolvable, just undated. The
			// screen shows no time rather than refusing to offer the choice.
			name:     "stamp will not parse",
			in:       "scene.sync-conflict-notatime.blend",
			original: "scene.blend",
		},
		{
			name: "not a conflict at all",
			in:   "scene.blend",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := conflictParse(tc.in)
			if got.Original != tc.original {
				t.Errorf("original = %q, want %q", got.Original, tc.original)
			}
			if !got.When.Equal(tc.when) {
				t.Errorf("when = %v, want %v", got.When, tc.when)
			}
			if got.Short != tc.short {
				t.Errorf("short = %q, want %q", got.Short, tc.short)
			}
		})
	}
}

func TestIsConflictName(t *testing.T) {
	cases := map[string]bool{
		"scene.sync-conflict-20260824-032916-F67Q3OS.blend":       true,
		`refs\scene.sync-conflict-20260824-032916-F67Q3OS.blend`:  true,
		"scene.blend": false,
		"":            false,
		// The marker has to be in the file name. A directory that happens to
		// carry it does not make everything under it resolvable -- resolving
		// renames the file, and there is nothing here to rename it to.
		"sync-conflict-20260824-032916-F67Q3OS/scene.blend":        false,
		`a.sync-conflict-20260824-032916-F67Q3OS\scene.blend`:      false,
		"a.sync-conflict-20260824-032916-F67Q3OS/scene.blend":      false,
		"deep/dir/scene.sync-conflict-20260824-032916-AAAAAAA.png": true,
	}

	for in, want := range cases {
		if got := isConflictName(in); got != want {
			t.Errorf("isConflictName(%q) = %v, want %v", in, got, want)
		}
	}
}

// The two halves of the split have to compose back into the input, or a
// resolve writes to a path that is not where the file came from.
func TestConflictDirBaseCompose(t *testing.T) {
	for _, in := range []string{
		"scene.blend",
		"refs/chair.png",
		`refs\chair.png`,
		`a/b\c/d.png`,
		"/rooted.png",
	} {
		if got := conflictDir(in) + conflictBase(in); got != in {
			t.Errorf("dir+base for %q = %q", in, got)
		}
	}
}
