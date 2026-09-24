// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/versioner"
)

func TestCleanClaimPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Scenes/cabin.blend", "Scenes/cabin.blend", true},
		// Send To hands over Windows separators.
		{`Scenes\cabin.blend`, "Scenes/cabin.blend", true},
		{"/Scenes/cabin.blend", "Scenes/cabin.blend", true},
		{"Scenes/./cabin.blend", "Scenes/cabin.blend", true},
		{"  texture1.png ", "texture1.png", true},

		{"", "", false},
		{".", "", false},
		{"..", "", false},
		{"../outside.blend", "", false},
		{"Scenes/../../outside.blend", "", false},
		{`C:\Users\yuki\cabin.blend`, "", false},
		// Claiming a claims file would be a claim about the bookkeeping.
		{".desuq-claims", "", false},
		{".desuq-claims/ABC.json", "", false},
		// A sibling that merely starts with the same letters is a file.
		{".desuq-claims-notes.txt", ".desuq-claims-notes.txt", true},
	}
	for _, c := range cases {
		got, ok := cleanClaimPath(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("cleanClaimPath(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestApplyClaim(t *testing.T) {
	monday := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	later := monday.Add(26 * time.Hour)

	list, changed, err := applyClaim(nil, "a.blend", false, monday)
	if err != nil || !changed || len(list) != 1 || !list[0].Since.Equal(monday) {
		t.Fatalf("first claim: %+v %v %v", list, changed, err)
	}

	// Marking again keeps the original time.
	again, changed, _ := applyClaim(list, "a.blend", false, later)
	if changed || len(again) != 1 || !again[0].Since.Equal(monday) {
		t.Errorf("re-claim changed the claim: %+v %v", again, changed)
	}

	two, _, _ := applyClaim(list, "b.blend", false, later)
	if len(two) != 2 {
		t.Fatalf("second file: %+v", two)
	}

	one, changed, _ := applyClaim(two, "a.blend", true, later)
	if !changed || len(one) != 1 || one[0].Path != "b.blend" {
		t.Errorf("release: %+v %v", one, changed)
	}

	// Releasing something not held is not a change, and not an error.
	same, changed, err := applyClaim(one, "zzz.blend", true, later)
	if changed || err != nil || len(same) != 1 {
		t.Errorf("release of unheld: %+v %v %v", same, changed, err)
	}

	var full []claimEntry
	for i := 0; i < claimsMaxPerDevice; i++ {
		full = append(full, claimEntry{Path: string(rune('a'+i%26)) + time.Duration(i).String(), Since: monday})
	}
	if _, _, err := applyClaim(full, "one-too-many.blend", false, later); err != errClaimTooMany {
		t.Errorf("cap: got %v, want errClaimTooMany", err)
	}
	// ...but releasing from a full list still works.
	if _, changed, err := applyClaim(full, full[0].Path, true, later); err != nil || !changed {
		t.Errorf("release from a full list: %v %v", changed, err)
	}
}

func TestParseClaimsFile(t *testing.T) {
	yuki := protocol.LocalDeviceID
	other := protocol.DeviceID{1, 2, 3}
	since := time.Date(2026, 9, 24, 14, 20, 0, 0, time.UTC)

	good, _ := json.Marshal(claimsFile{
		Version: 1,
		Device:  yuki.String(),
		Claims: []claimEntry{
			{Path: "Scenes/cabin.blend", Since: since},
			{Path: "../escape.blend", Since: since}, // dropped, not fatal
			{Path: "no-time.blend"},                 // dropped: no time
		},
	})

	got, ok := parseClaimsFile(good, yuki)
	if !ok || len(got) != 1 || got[0].Path != "Scenes/cabin.blend" {
		t.Errorf("parse: %+v %v", got, ok)
	}

	// A file that names somebody else inside is not believed at all.
	if _, ok := parseClaimsFile(good, other); ok {
		t.Error("a claims file speaking for another device was accepted")
	}
	if _, ok := parseClaimsFile([]byte("not json"), yuki); ok {
		t.Error("garbage parsed")
	}
}

func TestIsClaimsPath(t *testing.T) {
	for name, want := range map[string]bool{
		".desuq-claims":               true,
		".desuq-claims/X.json":        true,
		`.desuq-claims\X.json`:        true,
		".desuq-claims-notes.txt":     false,
		"Scenes/.desuq-claims/X.json": false,
		"Scenes/cabin.blend":          false,
		"":                            false,
	} {
		if got := isClaimsPath(name); got != want {
			t.Errorf("isClaimsPath(%q) = %v, want %v", name, got, want)
		}
	}
}

// The History screen must not list the claims files beside the work.
func TestHistoryLeavesClaimsOut(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	versions := map[string][]versioner.FileVersion{
		"Scenes/cabin.blend":   {{VersionTime: at, Size: 800}},
		".desuq-claims/Y.json": {{VersionTime: at, Size: 90}, {VersionTime: at.Add(time.Hour), Size: 91}},
	}
	res, rows := historySummarise(versions, "", nil)
	if len(rows) != 1 || rows[0].Name != "Scenes/cabin.blend" {
		t.Errorf("rows: %+v", rows)
	}
	if res.Files != 1 || res.Versions != 1 || res.Bytes != 800 {
		t.Errorf("totals count the claims: %+v", res)
	}
}
