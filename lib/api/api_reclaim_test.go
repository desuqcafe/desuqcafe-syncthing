// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_reclaim.go.
//
// Reclaiming is the only thing this fork does that deletes a person's files,
// so the rails get the same treatment api_reveal_test.go gives the path check:
// the deciding logic is pure, and this file tries to make it say yes when it
// should say no.
//
// The bias under test is one-directional. A rail that wrongly keeps a file
// wastes disk; a rail that wrongly deletes one loses work that may exist
// nowhere else. Every ambiguous case below expects "keep".

package api

import (
	"strings"
	"testing"
	"time"
)

func TestReclaimVerdictRefusesUnlessEverythingLinesUp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	const size = 1024

	cases := []struct {
		name       string
		globalSize int64
		globalMod  time.Time
		diskSize   int64
		diskMod    time.Time
		holders    int
		window     time.Duration
		wantDelete bool
		wantReason string
	}{
		{
			name:       "identical and somebody has it",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now,
			holders: 1, wantDelete: true,
		},
		{
			name:       "nobody connected has it",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now,
			holders: 0, wantReason: "nobody else",
		},
		{
			name:       "our copy is bigger",
			globalSize: size, globalMod: now, diskSize: size + 1, diskMod: now,
			holders: 1, wantReason: "different size",
		},
		{
			name:       "our copy is smaller, eg a half-finished write",
			globalSize: size, globalMod: now, diskSize: size - 1, diskMod: now,
			holders: 1, wantReason: "different size",
		},
		{
			name:       "same size, edited in place",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now.Add(time.Hour),
			holders: 1, wantReason: "has been changed",
		},
		{
			name:       "same size, older on disk",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now.Add(-time.Hour),
			holders: 1, wantReason: "has been changed",
		},
		{
			// The dangerous one: modified *and* nowhere else. Must report the
			// unavailability, because that is the reason it can never be safe.
			name:       "modified and unavailable reports unavailable",
			globalSize: size, globalMod: now, diskSize: size + 5, diskMod: now.Add(time.Hour),
			holders: 0, wantReason: "nobody else",
		},
		{
			name:       "a one second drift inside a two second window",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now.Add(time.Second),
			holders: 1, window: 2 * time.Second, wantDelete: true,
		},
		{
			name:       "a three second drift outside a two second window",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now.Add(3 * time.Second),
			holders: 1, window: 2 * time.Second, wantReason: "has been changed",
		},
		{
			// A zero window means exact, and a nanosecond is a difference.
			name:       "sub-second drift with no window is still a difference",
			globalSize: size, globalMod: now, diskSize: size, diskMod: now.Add(time.Nanosecond),
			holders: 1, wantReason: "has been changed",
		},
		{
			// An empty file is a real file, and zero == zero must not be
			// mistaken for "no information".
			name:       "an empty file that matches is deletable",
			globalSize: 0, globalMod: now, diskSize: 0, diskMod: now,
			holders: 1, wantDelete: true,
		},
		{
			// The failure mode this endpoint exists because of: the local
			// index reports zero for an ignored file. If that number ever
			// reaches the comparison, it must not look like agreement.
			name:       "a blanked size against a real file is a mismatch",
			globalSize: size, globalMod: now, diskSize: 0, diskMod: now,
			holders: 1, wantReason: "different size",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := reclaimVerdict(tc.globalSize, tc.globalMod, tc.diskSize, tc.diskMod, tc.holders, tc.window)
			if tc.wantDelete {
				if got != "" {
					t.Fatalf("expected the file to be deletable, kept because %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected the file to be kept, it was marked deletable")
			}
			if !strings.Contains(got, tc.wantReason) {
				t.Fatalf("reason %q does not mention %q", got, tc.wantReason)
			}
		})
	}
}

func TestModTimeEqualWindow(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	if !modTimeEqual(base, base, 0) {
		t.Error("a time should equal itself with no window")
	}
	if modTimeEqual(base, base.Add(time.Nanosecond), 0) {
		t.Error("no window means exact")
	}
	// Symmetric: which side is newer must not change the answer.
	if !modTimeEqual(base, base.Add(time.Second), 2*time.Second) {
		t.Error("a drift inside the window should compare equal")
	}
	if !modTimeEqual(base.Add(time.Second), base, 2*time.Second) {
		t.Error("the comparison should be symmetric")
	}
	if modTimeEqual(base, base.Add(3*time.Second), 2*time.Second) {
		t.Error("a drift outside the window should not compare equal")
	}
	// Exactly on the boundary counts as equal, matching the scanner.
	if !modTimeEqual(base, base.Add(2*time.Second), 2*time.Second) {
		t.Error("the window should be inclusive")
	}
}

func TestParentDir(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a.png":           "",
		"refs/a.png":      "refs",
		"refs/wood/a.png": "refs/wood",
		"/leading":        "",
		"refs/wood/":      "refs/wood",
		"":                "",
	}
	for in, want := range cases {
		if got := parentDir(in); got != want {
			t.Errorf("parentDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeepestFirstCollapsesAChain(t *testing.T) {
	t.Parallel()

	// Deleting refs/wood/oak/a.png should offer refs/wood/oak, then refs/wood,
	// then refs -- in that order, or the parents are still non-empty when the
	// attempt is made and the chain never collapses.
	got := deepestFirst(map[string]struct{}{
		"refs/wood/oak": {},
		"models":        {},
	})

	want := []string{"refs/wood/oak", "refs/wood", "models", "refs"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// The real invariant, stated independently of the exact ordering above:
	// no directory may appear before one of its own descendants.
	seen := map[string]int{}
	for i, d := range got {
		seen[d] = i
	}
	for d, i := range seen {
		if p := parentDir(d); p != "" {
			if j, ok := seen[p]; ok && j < i {
				t.Errorf("%q (parent of %q) is removed first", p, d)
			}
		}
	}
}

func TestCapEntriesLeavesShortListsAlone(t *testing.T) {
	t.Parallel()

	short := make([]reclaimEntry, 3)
	if got := capEntries(short); len(got) != 3 {
		t.Errorf("a short list should be returned whole, got %d", len(got))
	}

	long := make([]reclaimEntry, reclaimListCap+50)
	if got := capEntries(long); len(got) != reclaimListCap {
		t.Errorf("a long list should be capped at %d, got %d", reclaimListCap, len(got))
	}
}
