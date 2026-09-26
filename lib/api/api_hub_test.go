// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_hub.go.

package api

import (
	"testing"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
)

func hubDev(b byte) protocol.DeviceID {
	var id protocol.DeviceID
	for i := range id {
		id[i] = b
	}
	return id
}

func TestHubAnalyse(t *testing.T) {
	me, kai, mia := hubDev(1), hubDev(2), hubDev(3)
	folder := func(devs ...protocol.DeviceID) config.FolderConfiguration {
		f := config.FolderConfiguration{ID: "assets", Label: "Project Assets"}
		for _, d := range devs {
			f.Devices = append(f.Devices, config.FolderDeviceConfiguration{DeviceID: d})
		}
		return f
	}
	list := func(devs ...protocol.DeviceID) []model.DesuqPeerFolderDevice {
		var out []model.DesuqPeerFolderDevice
		for _, d := range devs {
			out = append(out, model.DesuqPeerFolderDevice{ID: d, Name: map[protocol.DeviceID]string{me: "Alex", kai: "Kai", mia: "Mia's laptop"}[d]})
		}
		return out
	}
	devices := map[protocol.DeviceID]config.DeviceConfiguration{
		kai: {DeviceID: kai, Name: "Kai"},
		mia:   {DeviceID: mia, Name: "Mia"},
	}
	online := func(protocol.DeviceID) bool { return true }

	t.Run("the middle sees both edges not sharing with each other", func(t *testing.T) {
		cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			kai: {"assets": list(kai, me)},
			mia:   {"assets": list(mia, me)},
		}
		hf := hubAnalyse(folder(me, kai, mia), me, cc, devices, online)
		if len(hf.Through) != 1 || len(hf.Via) != 0 {
			t.Fatalf("got %+v", hf)
		}
		names := hf.Through[0].A.Name + "+" + hf.Through[0].B.Name
		if names != "Kai+Mia" {
			t.Errorf("pair %q", names)
		}
	})

	t.Run("one side sharing is not enough", func(t *testing.T) {
		// Kai pressed Connect directly; Mia has not accepted yet.
		cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			kai: {"assets": list(kai, me, mia)},
			mia:   {"assets": list(mia, me)},
		}
		if hf := hubAnalyse(folder(me, kai, mia), me, cc, devices, online); len(hf.Through) != 1 {
			t.Fatalf("got %+v", hf)
		}
	})

	t.Run("a full mesh says nothing", func(t *testing.T) {
		cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			kai: {"assets": list(kai, me, mia)},
			mia:   {"assets": list(mia, me, kai)},
		}
		if hf := hubAnalyse(folder(me, kai, mia), me, cc, devices, online); len(hf.Through) != 0 || len(hf.Via) != 0 {
			t.Fatalf("got %+v", hf)
		}
	})

	t.Run("nobody heard from, or not accepted, says nothing", func(t *testing.T) {
		if hf := hubAnalyse(folder(me, kai, mia), me, nil, devices, online); len(hf.Through) != 0 {
			t.Fatalf("no cluster configs: %+v", hf)
		}
		cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			kai: {"other": list(kai, me)},
		}
		if hf := hubAnalyse(folder(me, kai, mia), me, cc, devices, online); len(hf.Through) != 0 {
			t.Fatalf("folder not accepted: %+v", hf)
		}
		// Kai is known, Mia has never connected. Found on a pair.
		cc = map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			kai: {"assets": list(kai, me)},
		}
		if hf := hubAnalyse(folder(me, kai, mia), me, cc, devices, online); len(hf.Through) != 0 {
			t.Fatalf("one side never heard from: %+v", hf)
		}
	})

	t.Run("the edge sees who it reaches only through the middle", func(t *testing.T) {
		// This computer is Kai's: it shares the folder with Alex alone.
		alex := me
		cc := map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice{
			alex: {"assets": list(alex, kai, mia)},
		}
		hf := hubAnalyse(folder(kai, alex), kai, cc,
			map[protocol.DeviceID]config.DeviceConfiguration{alex: {DeviceID: alex, Name: "Alex"}}, online)
		if len(hf.Via) != 1 || len(hf.Through) != 0 {
			t.Fatalf("got %+v", hf)
		}
		v := hf.Via[0]
		// Not a device here, so named the way Alex named her.
		if v.Device != mia.String() || v.Name != "Mia's laptop" || len(v.Via) != 1 || v.Via[0].Name != "Alex" {
			t.Fatalf("via %+v", v)
		}
	})
}
