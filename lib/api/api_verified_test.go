// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_verified.go.

package api

import (
	"testing"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
)

func TestOfferMembers(t *testing.T) {
	me, alex, kai, mia := hubDev(1), hubDev(2), hubDev(3), hubDev(4)
	cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
		alex: {"assets": {
			{ID: mia, Name: "Mia's laptop"},
			{ID: alex, Name: "Alex"},
			{ID: me, Name: "Me, as Alex calls me"},
			{ID: kai, Name: "Kai"},
		}},
	}
	devices := map[protocol.DeviceID]config.DeviceConfiguration{
		alex: {DeviceID: alex, Name: "Alex (studio)", DesuqVerifiedAt: "2026-09-27T10:00:00Z"},
		kai:  {DeviceID: kai, Name: "Kai"},
	}

	got := offerMembers(map[string][]protocol.DeviceID{"assets": {alex}}, me, cc, devices)
	ms := got["assets"][alex.String()]
	if len(ms) != 3 {
		t.Fatalf("want Alex, Kai and Mia -- never this computer -- got %+v", ms)
	}
	if !ms[0].Offering || ms[0].Name != "Alex (studio)" || !ms[0].Verified {
		t.Errorf("the offering device comes first, under this computer's name for it: %+v", ms[0])
	}
	if ms[1].Name != "Kai" || !ms[1].Known || ms[1].Verified {
		t.Errorf("Kai is known here and not verified: %+v", ms[1])
	}
	if ms[2].Name != "Mia's laptop" || ms[2].Known {
		t.Errorf("Mia is unknown here and keeps the offerer's name for her: %+v", ms[2])
	}

	// An offer from a peer whose cluster config has not been seen (it went
	// offline before sending one) still names the offerer.
	got = offerMembers(map[string][]protocol.DeviceID{"other": {kai}}, me, cc, devices)
	if ms := got["other"][kai.String()]; len(ms) != 1 || ms[0].Name != "Kai" {
		t.Errorf("got %+v", ms)
	}
}
