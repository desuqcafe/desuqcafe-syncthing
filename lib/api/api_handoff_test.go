// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork: see api_handoff.go.

package api

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

var (
	handGiver = protocol.DeviceID{1}
	handTaker = protocol.DeviceID{2}
	handThird = protocol.DeviceID{3}
)

// docsOf builds what readClaimDocs would find, from each device's file.
func docsOf(files map[protocol.DeviceID]claimsFile) claimDocs {
	d := claimDocs{byDevice: map[protocol.DeviceID]claimsFile{}, present: map[protocol.DeviceID]bool{}}
	for dev, f := range files {
		d.byDevice[dev] = f
		d.present[dev] = true
		d.order = append(d.order, dev)
	}
	sort.Slice(d.order, func(i, j int) bool { return d.order[i].Compare(d.order[j]) < 0 })
	return d
}

// roundTrip is what a file looks like after being written and read back,
// which is how every peer sees it.
func roundTrip(t *testing.T, dev protocol.DeviceID, f claimsFile) claimsFile {
	t.Helper()
	f.Version, f.Device = claimsFileVersion, dev.String()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := parseClaimsDoc(data, dev)
	if !ok {
		t.Fatalf("could not read back %s", data)
	}
	return out
}

func TestHandoffRoundTrip(t *testing.T) {
	monday := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	later := monday.Add(time.Hour)

	// The giver marked the file on Monday, automatically, by saving it.
	giver := claimsFile{Claims: []claimEntry{{Path: "Scenes/cabin.blend", Since: monday, Auto: true}}}
	var err error
	giver.Claims, err = applyHandoff(giver.Claims, "Scenes/cabin.blend", handTaker, later)
	if err != nil {
		t.Fatal(err)
	}
	if c := giver.Claims[0]; c.To != handTaker.String() || c.Auto || !c.Since.Equal(monday) {
		t.Fatalf("handing over: %+v; want to=taker, not auto, since kept", c)
	}
	giver = roundTrip(t, handGiver, giver)

	// Step 2: the taker's computer sees it.
	taker, changed := planHandoffs(handTaker, claimsFile{}, docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver}), true, later)
	if !changed || len(taker.Claims) != 1 || len(taker.Accepted) != 1 {
		t.Fatalf("taking: %+v changed=%v", taker, changed)
	}
	if c := taker.Claims[0]; c.From != handGiver.String() || c.Path != "Scenes/cabin.blend" || c.Auto {
		t.Fatalf("taken mark: %+v", c)
	}
	taker = roundTrip(t, handTaker, taker)

	// Asking again changes nothing.
	docs := docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver, handTaker: taker})
	if _, changed := planHandoffs(handTaker, taker, docs, true, later); changed {
		t.Error("taking twice changed the taker's file")
	}

	// Everybody now sees the giver's half as taken, even before the giver's
	// computer has tidied it away.
	if !docs.accepted(handTaker.String(), handGiver, giver.Claims[0]) {
		t.Error("the giver's entry is not seen as taken")
	}

	// Step 3: the giver's computer drops its entry.
	giverNext, changed := planHandoffs(handGiver, giver, docs, true, later)
	if !changed || len(giverNext.Claims) != 0 {
		t.Fatalf("giver tidy: %+v changed=%v", giverNext, changed)
	}
	giverNext = roundTrip(t, handGiver, giverNext)

	// And once that has arrived, the taker prunes its acknowledgement but
	// keeps the mark.
	docs = docsOf(map[protocol.DeviceID]claimsFile{handGiver: giverNext, handTaker: taker})
	takerNext, changed := planHandoffs(handTaker, taker, docs, true, later)
	if !changed || len(takerNext.Accepted) != 0 || len(takerNext.Claims) != 1 {
		t.Fatalf("taker prune: %+v changed=%v", takerNext, changed)
	}
}

// The giver hands over and switches off for the weekend. The taker works on
// the file and presses Done. It must not come back.
func TestHandoffDoneWhileGiverAway(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	giver := claimsFile{}
	giver.Claims, _ = applyHandoff(nil, "tex.png", handTaker, at)
	giver = roundTrip(t, handGiver, giver)

	docs := docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver})
	taker, _ := planHandoffs(handTaker, claimsFile{}, docs, true, at)

	// Done.
	taker.Claims, _, _ = applyClaim(taker.Claims, "tex.png", true, false, at.Add(time.Hour))
	taker = roundTrip(t, handTaker, taker)
	if len(taker.Claims) != 0 || len(taker.Accepted) != 1 {
		t.Fatalf("after Done: %+v", taker)
	}

	// The giver's "to: taker" is still there, and nothing changes.
	docs = docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver, handTaker: taker})
	if next, changed := planHandoffs(handTaker, taker, docs, true, at.Add(2*time.Hour)); changed || len(next.Claims) != 0 {
		t.Errorf("the file came back after Done: %+v", next)
	}
	// Nor does anybody see it as still being handed over.
	if !docs.accepted(handTaker.String(), handGiver, giver.Claims[0]) {
		t.Error("the giver's stale entry would show again after Done")
	}
}

func TestHandoffTakenBackBeforeAccepted(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	list, _ := applyHandoff(nil, "a.blend", handTaker, at)

	// A save does not take it back: the tray marks on every save.
	list, changed, _ := applyClaim(list, "a.blend", false, true, at.Add(time.Minute))
	if changed || list[0].To == "" {
		t.Fatalf("an automatic mark cancelled the hand-over: %+v", list)
	}
	// Marking it by hand does.
	list, changed, _ = applyClaim(list, "a.blend", false, false, at.Add(2*time.Minute))
	if !changed || list[0].To != "" || !list[0].HandedAt.IsZero() {
		t.Fatalf("marking by hand did not take it back: %+v", list)
	}
	giver := roundTrip(t, handGiver, claimsFile{Claims: list})
	if next, changed := planHandoffs(handTaker, claimsFile{}, docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver}), true, at); changed || len(next.Claims) != 0 {
		t.Errorf("a taken-back hand-over was still taken: %+v", next)
	}
}

// A receive-only computer cannot mark anything anybody would see, so it does
// not acknowledge either: that would hide the giver's mark and leave the file
// unmarked for everybody.
func TestHandoffNotTakenWhereItCannotBeSeen(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	giver := claimsFile{}
	giver.Claims, _ = applyHandoff(nil, "a.blend", handTaker, at)
	giver = roundTrip(t, handGiver, giver)
	if next, changed := planHandoffs(handTaker, claimsFile{}, docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver}), false, at); changed || len(next.Accepted) != 0 {
		t.Errorf("took a hand-over it could not show: %+v", next)
	}
}

// Only the addressee takes a hand-over; a third person just sees a mark.
func TestHandoffOnlyForTheAddressee(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	giver := claimsFile{}
	giver.Claims, _ = applyHandoff(nil, "a.blend", handTaker, at)
	giver = roundTrip(t, handGiver, giver)
	if _, changed := planHandoffs(handThird, claimsFile{}, docsOf(map[protocol.DeviceID]claimsFile{handGiver: giver}), true, at); changed {
		t.Error("a third person took somebody else's hand-over")
	}
}

// A giver's file that is on disk but cannot be believed right now is not
// evidence that the hand-over is gone.
func TestHandoffAckSurvivesAnUnreadableGiver(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	taker := roundTrip(t, handTaker, claimsFile{
		Claims:   []claimEntry{{Path: "a.blend", Since: at, From: handGiver.String(), HandedAt: at}},
		Accepted: []handoffAck{{From: handGiver.String(), Path: "a.blend", HandedAt: at}},
	})
	docs := docsOf(map[protocol.DeviceID]claimsFile{handTaker: taker})
	docs.present[handGiver] = true // there, but not in byDevice
	if next, changed := planHandoffs(handTaker, taker, docs, true, at); changed || len(next.Accepted) != 1 {
		t.Errorf("an unreadable giver pruned the acknowledgement: %+v", next)
	}
	// With no file at all, it goes.
	delete(docs.present, handGiver)
	if next, changed := planHandoffs(handTaker, taker, docs, true, at); !changed || len(next.Accepted) != 0 {
		t.Errorf("an absent giver kept the acknowledgement: %+v", next)
	}
}

func TestParseClaimsDocDropsBadHandoffFields(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(claimsFile{
		Version: 1, Device: handGiver.String(),
		Claims: []claimEntry{
			{Path: "a.blend", Since: at, To: "not-a-device", HandedAt: at},
			{Path: "b.blend", Since: at, To: handTaker.String()}, // no time
		},
		Accepted: []handoffAck{{From: "nope", Path: "a.blend", HandedAt: at}},
	})
	doc, ok := parseClaimsDoc(data, handGiver)
	if !ok || len(doc.Claims) != 2 {
		t.Fatalf("marks dropped with their hand-over fields: %+v", doc)
	}
	for _, c := range doc.Claims {
		if c.To != "" || !c.HandedAt.IsZero() {
			t.Errorf("a bad hand-over field survived: %+v", c)
		}
	}
	if len(doc.Accepted) != 0 {
		t.Errorf("a bad acknowledgement survived: %+v", doc.Accepted)
	}
}

// An older build reads "to" and "from" as unknown fields and ignores them,
// and a file with only acknowledgements still has a list of claims.
func TestHandoffFileStaysReadableByOlderBuilds(t *testing.T) {
	at := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	f := roundTrip(t, handTaker, claimsFile{Accepted: []handoffAck{{From: handGiver.String(), Path: "a.blend", HandedAt: at}}})
	var old struct {
		Claims []struct {
			Path  string    `json:"path"`
			Since time.Time `json:"since"`
		} `json:"claims"`
	}
	data, _ := json.Marshal(f)
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	var plain claimsFile
	withMark, _ := json.Marshal(claimsFile{Version: 1, Device: handTaker.String(), Claims: []claimEntry{{Path: "a.blend", Since: at}}})
	if err := json.Unmarshal(withMark, &plain); err != nil {
		t.Fatal(err)
	}
	if string(withMark) != `{"version":1,"device":"`+handTaker.String()+`","claims":[{"path":"a.blend","since":"2026-09-25T17:00:00Z"}]}` {
		t.Errorf("a plain mark gained fields an older build would not expect: %s", withMark)
	}
}
