// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_repair.go.
//
// This is the test that matters most in the fork: the difference between the
// two allowed cases and the two refused ones is the difference between fixing
// a stuck folder and telling three people's computers to delete an asset
// library.

package api

import (
	"strings"
	"testing"
)

func TestRepairVerdict(t *testing.T) {
	cases := []struct {
		name           string
		pathMissing    bool
		rootHasContent bool
		indexed        int
		wantCreateRoot bool
		wantRefused    bool
	}{
		{
			// The folder stuck on the author's own machine since 24 August: a
			// share that was accepted and never filled.
			name:           "missing directory, nothing ever in it",
			pathMissing:    true,
			indexed:        0,
			wantCreateRoot: true,
		},
		{
			// The drive is unplugged. Creating an empty directory here is how
			// a team loses everything.
			name:        "missing directory, 45 files indexed",
			pathMissing: true,
			indexed:     45,
			wantRefused: true,
		},
		{
			// Somebody's cleaner ate the dot-directory. The files are all
			// still there, so putting the marker back is the whole fix.
			name:           "marker gone from a directory that still has files",
			rootHasContent: true,
			indexed:        45,
		},
		{
			// The same disk letter, a different disk. Looks exactly like the
			// case above from the folder's point of view, and is the opposite.
			name:        "marker gone and the directory is empty",
			indexed:     45,
			wantRefused: true,
		},
		{
			// A brand new empty share with no marker: nothing to lose.
			name:    "marker gone, empty directory, empty index",
			indexed: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repairVerdict(tc.pathMissing, tc.rootHasContent, tc.indexed)
			if (got.reason != "") != tc.wantRefused {
				t.Fatalf("reason = %q, refused should be %v", got.reason, tc.wantRefused)
			}
			if got.createRoot != tc.wantCreateRoot {
				t.Errorf("createRoot = %v, want %v", got.createRoot, tc.wantCreateRoot)
			}
			// A refusal has to name the number, because that is the part that
			// stops somebody clicking through it.
			if tc.wantRefused && !strings.Contains(got.reason, repairItoa(tc.indexed)) {
				t.Errorf("refusal does not name the file count: %q", got.reason)
			}
		})
	}
}

func TestRepairItoa(t *testing.T) {
	for n, want := range map[int]string{0: "0", 1: "1", 45: "45", 12345: "12345"} {
		if got := repairItoa(n); got != want {
			t.Errorf("repairItoa(%d) = %q, want %q", n, got, want)
		}
	}
}
