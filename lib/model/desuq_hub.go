// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork: a new file inside an
// upstream package. It costs one line in model.go -- the call at the top of
// ClusterConfig -- and lib/api finds DesuqClusterFolders by an optional
// interface, so the Model interface and its mocks are untouched. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// Three people, one folder, and the commonest way to set it up: the person
// who made the folder shares it with each of the other two. Syncthing then
// syncs A-B and A-C, and never B-C, because neither B nor C was told the
// other exists. It works -- every change reaches everybody through A -- until
// A's computer is off, when B and C silently stop syncing with each other
// while both of them are online. Nothing says so anywhere.
//
// Every peer already tells us who it shares each folder with: the device
// list in its cluster config, sent at connect and again whenever its
// configuration changes. Upstream reads it for introductions and auto-accept
// and then drops it. This keeps the last one per peer, in memory, so the
// question can be asked afterwards:
//
//   - on the computer in the middle: "Kai and Mia only reach each other
//     through this computer";
//   - on the computer at the edge: "you get Mia's changes only through
//     Alex's computer" -- and there, since the cluster config carries Mia's
//     device ID and the name Alex gave her, a way to connect to her
//     directly.
//
// Only primary cluster configs carry the lists; secondary connections send an
// empty one to say they are ready, and are skipped exactly as upstream skips
// them. Kept across a disconnect, because "last known" is still the right
// answer about a peer who is away, and replaced on the next connect.

package model

import (
	"sync"

	"github.com/syncthing/syncthing/lib/protocol"
)

// DesuqPeerFolderDevice is one entry in a peer's announced device list.
type DesuqPeerFolderDevice struct {
	ID   protocol.DeviceID
	Name string
}

var (
	desuqCCMut sync.Mutex
	// model -> peer -> folder ID -> the devices that peer shares it with,
	// including itself and us. Keyed by model so that tests standing up
	// several models in one process do not see each other's peers.
	desuqCC = map[*model]map[protocol.DeviceID]map[string][]DesuqPeerFolderDevice{}
)

// desuqRememberClusterConfig is the one call ClusterConfig makes into this
// file.
func (m *model) desuqRememberClusterConfig(peer protocol.DeviceID, cm *protocol.ClusterConfig) {
	if cm == nil || cm.Secondary {
		return
	}
	folders := make(map[string][]DesuqPeerFolderDevice, len(cm.Folders))
	for _, f := range cm.Folders {
		devs := make([]DesuqPeerFolderDevice, 0, len(f.Devices))
		for _, d := range f.Devices {
			// A device the peer shares with encrypted is somebody it does
			// not trust with the contents; not somebody to suggest
			// connecting to.
			if len(d.EncryptionPasswordToken) > 0 {
				continue
			}
			devs = append(devs, DesuqPeerFolderDevice{ID: d.ID, Name: d.Name})
		}
		folders[f.ID] = devs
	}
	desuqCCMut.Lock()
	defer desuqCCMut.Unlock()
	byPeer := desuqCC[m]
	if byPeer == nil {
		byPeer = map[protocol.DeviceID]map[string][]DesuqPeerFolderDevice{}
		desuqCC[m] = byPeer
	}
	byPeer[peer] = folders
}

// DesuqClusterFolders returns, for every peer heard from since start, the
// devices it last said it shares each folder with. A copy: the caller may
// keep it.
func (m *model) DesuqClusterFolders() map[protocol.DeviceID]map[string][]DesuqPeerFolderDevice {
	desuqCCMut.Lock()
	defer desuqCCMut.Unlock()
	out := make(map[protocol.DeviceID]map[string][]DesuqPeerFolderDevice, len(desuqCC[m]))
	for peer, folders := range desuqCC[m] {
		cp := make(map[string][]DesuqPeerFolderDevice, len(folders))
		for id, devs := range folders {
			cp[id] = append([]DesuqPeerFolderDevice(nil), devs...)
		}
		out[peer] = cp
	}
	return out
}
