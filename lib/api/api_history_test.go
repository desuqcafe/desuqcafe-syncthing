// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_history.go.
//
// The point of this endpoint is that the browser never sees the full archive,
// so the browser cannot check the arithmetic. That makes the totals the thing
// worth testing: a version count that is quietly low still looks like a
// version count, and a person deciding whether thirty days of history is worth
// the disk has nothing else to go on.

package api

import (
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/versioner"
)

func at(day int, size int64) versioner.FileVersion {
	t := time.Date(2026, 8, day, 12, 0, 0, 0, time.UTC)
	return versioner.FileVersion{VersionTime: t, ModTime: t, Size: size}
}

func TestHistorySummariseTotalsAndOrder(t *testing.T) {
	t.Parallel()

	archive := map[string][]versioner.FileVersion{
		// Deliberately not in time order in the slice: the versioner does not
		// promise one, and the row must not depend on it.
		"refs/chair.blend": {at(20, 100), at(25, 300), at(22, 200)},
		"models/leg.blend": {at(26, 50)},
		"notes.txt":        {at(10, 7), at(11, 8)},
		// An empty list is not a file with history and must not be counted.
		"ghost.png": {},
	}

	res, rows := historySummarise(archive, "", nil)

	if res.Files != 3 {
		t.Errorf("Files = %d, want 3 (the empty list should not count)", res.Files)
	}
	if res.Versions != 6 {
		t.Errorf("Versions = %d, want 6", res.Versions)
	}
	if want := int64(100 + 300 + 200 + 50 + 7 + 8); res.Bytes != want {
		t.Errorf("Bytes = %d, want %d", res.Bytes, want)
	}

	// Newest first, so the most recently touched file leads.
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].Name != "models/leg.blend" {
		t.Errorf("first row is %q, want models/leg.blend", rows[0].Name)
	}
	if rows[1].Name != "refs/chair.blend" {
		t.Errorf("second row is %q, want refs/chair.blend", rows[1].Name)
	}
	if rows[2].Name != "notes.txt" {
		t.Errorf("third row is %q, want notes.txt", rows[2].Name)
	}

	// Newest and Oldest must bracket the archive regardless of slice order.
	chair := rows[1]
	if got, want := chair.Newest, at(25, 0).VersionTime; !got.Equal(want) {
		t.Errorf("chair Newest = %v, want %v", got, want)
	}
	if got, want := chair.Oldest, at(20, 0).VersionTime; !got.Equal(want) {
		t.Errorf("chair Oldest = %v, want %v", got, want)
	}
	if chair.Versions != 3 {
		t.Errorf("chair Versions = %d, want 3", chair.Versions)
	}
	if chair.Bytes != 600 {
		t.Errorf("chair Bytes = %d, want 600", chair.Bytes)
	}
}

func TestHistorySummariseTotalsIgnoreTheFilter(t *testing.T) {
	t.Parallel()

	archive := map[string][]versioner.FileVersion{
		"refs/chair.blend": {at(20, 100)},
		"models/leg.blend": {at(21, 50)},
	}

	unfiltered, allRows := historySummarise(archive, "", nil)
	filtered, someRows := historySummarise(archive, "chair", nil)

	if len(allRows) != 2 || len(someRows) != 1 {
		t.Fatalf("filter matched %d rows of %d, want 1 of 2", len(someRows), len(allRows))
	}
	// This is the assertion that matters: searching narrows the list, not the
	// statement about how much disk the archive is using.
	if filtered.Bytes != unfiltered.Bytes || filtered.Versions != unfiltered.Versions || filtered.Files != unfiltered.Files {
		t.Errorf("filtering changed the totals: %+v vs %+v", filtered, unfiltered)
	}
}

func TestHistorySummariseMarksDeleted(t *testing.T) {
	t.Parallel()

	archive := map[string][]versioner.FileVersion{
		"kept.blend": {at(20, 1)},
		"gone.blend": {at(21, 1)},
	}
	_, rows := historySummarise(archive, "", func(name string) bool {
		return name == "gone.blend"
	})

	for _, r := range rows {
		want := r.Name == "gone.blend"
		if r.Deleted != want {
			t.Errorf("%s Deleted = %v, want %v", r.Name, r.Deleted, want)
		}
	}
}

func TestHistorySearchIsCaseInsensitiveSubstring(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, needle string
		want         bool
	}{
		{"refs/wood/Chair.blend", "chair", true},
		{"refs/wood/Chair.blend", "CHAIR", true},
		{"refs/wood/Chair.blend", "refs/", true},
		{"refs/wood/Chair.blend", "wood/ch", true},
		{"refs/wood/Chair.blend", ".blend", true},
		{"refs/wood/Chair.blend", "", true},
		{"refs/wood/Chair.blend", "oak", false},
		// Not a glob: a star is a literal, and must not silently match all.
		{"refs/wood/Chair.blend", "*", false},
		// A needle longer than the name cannot match.
		{"a.png", "a.png.and.more", false},
	}
	for _, tc := range cases {
		if got := historyMatches(tc.name, tc.needle); got != tc.want {
			t.Errorf("historyMatches(%q, %q) = %v, want %v", tc.name, tc.needle, got, tc.want)
		}
	}
}

func TestHistoryPagingClampsToSaneValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		page, perpage         string
		wantPage, wantPerpage int
	}{
		{"", "", 1, historyPageCap},
		{"2", "50", 2, 50},
		{"0", "50", 1, 50},
		{"-3", "50", 1, 50},
		// Above the cap is clamped, not honoured: the cap is the reason the
		// endpoint exists.
		{"1", "100000", 1, historyPageCap},
		{"1", "0", 1, historyPageCap},
		// Garbage falls back rather than erroring, so a hand-edited URL still
		// renders something.
		{"abc", "xyz", 1, historyPageCap},
	}
	for _, tc := range cases {
		gotPage, gotPerpage := historyPaging(tc.page, tc.perpage)
		if gotPage != tc.wantPage || gotPerpage != tc.wantPerpage {
			t.Errorf("historyPaging(%q,%q) = %d,%d want %d,%d",
				tc.page, tc.perpage, gotPage, gotPerpage, tc.wantPage, tc.wantPerpage)
		}
	}
}
