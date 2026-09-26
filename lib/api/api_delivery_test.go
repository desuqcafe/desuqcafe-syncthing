// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_delivery.go.

package api

import (
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

func TestDeliveryTally(t *testing.T) {
	me := protocol.ShortID(0x1111)
	kai := protocol.ShortID(0x2222)
	at := func(min int) int64 {
		return time.Date(2026, 9, 26, 14, min, 0, 0, time.UTC).Unix()
	}
	file := func(name string, by protocol.ShortID, size int64, min int) protocol.FileInfo {
		return protocol.FileInfo{Name: name, ModifiedBy: by, Size: size, ModifiedS: at(min)}
	}
	needed := []protocol.FileInfo{
		file(`Scenes\cabin.blend`, me, 100, 5),
		file("tree.blend", me, 50, 30),
		file("wood.png", kai, 7, 40),
		// A deletion of ours that has not arrived: still on their disk.
		{Name: "old.blend", ModifiedBy: me, Deleted: true, ModifiedS: at(10)},
		// Neither of these is a file anybody made.
		{Name: "Scenes", ModifiedBy: me, Type: protocol.FileInfoTypeDirectory, ModifiedS: at(50)},
		file(`.desuq-claims\ABCDEFG.json`, me, 3, 55),
	}
	var p deliveryPeer
	deliveryTally(&p, needed, me)

	if p.Need != (deliveryCount{Files: 4, Bytes: 157}) {
		t.Errorf("need = %+v", p.Need)
	}
	if p.Yours != (deliveryCount{Files: 3, Bytes: 150}) {
		t.Errorf("yours = %+v", p.Yours)
	}
	want := []string{"tree.blend", "old.blend", `Scenes\cabin.blend`}
	if len(p.Names) != len(want) {
		t.Fatalf("names = %q, want %q", p.Names, want)
	}
	for i := range want {
		if p.Names[i] != want[i] {
			t.Errorf("names = %q, want %q (newest first)", p.Names, want)
		}
	}
	if p.Newest != time.Unix(at(30), 0).Format(time.RFC3339) {
		t.Errorf("newest = %q", p.Newest)
	}

	// Nothing of ours outstanding: no names and no newest, however far
	// behind they are on somebody else's work.
	var q deliveryPeer
	deliveryTally(&q, []protocol.FileInfo{file("wood.png", kai, 7, 40)}, me)
	if q.Yours.Files != 0 || len(q.Names) != 0 || q.Newest != "" || q.Need.Files != 1 {
		t.Errorf("only theirs: %+v", q)
	}
}

func TestDeliveryNamesCapped(t *testing.T) {
	me := protocol.ShortID(1)
	var needed []protocol.FileInfo
	for i := 0; i < 12; i++ {
		needed = append(needed, protocol.FileInfo{Name: string(rune('a' + i)), ModifiedBy: me, ModifiedS: int64(i)})
	}
	var p deliveryPeer
	deliveryTally(&p, needed, me)
	if p.Yours.Files != 12 || len(p.Names) != deliveryNames || p.Names[0] != "l" {
		t.Errorf("got %d files, names %q", p.Yours.Files, p.Names)
	}
}
