// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork: a new file inside an
// upstream package, like desuq_peerheldback.go, so it can read another
// device's index without widening the Model interface. lib/api finds the
// method by an optional interface. See custom/CUSTOMIZATIONS.md.

package model

import (
	"github.com/syncthing/syncthing/lib/protocol"
)

// DesuqPeerHasLocal reports whether a peer's index holds this device's current
// version of one file -- which is to say, whether the peer has pulled it.
//
// It exists for the "I'm working on this" marks (lib/api/api_claims.go). A
// mark is a file in the folder, and the only evidence that it reached
// somebody is their own index announcing they now have the same version. Not
// that they are connected, and not that the scan ran: a peer can be online and
// still be holding the claims directory back, and a connected peer takes a
// moment to pull what was just written.
//
// Both sides missing counts as delivered: a marks file that was removed here
// (the last mark taken off) and that the peer never had or has also removed is
// agreement. An entry the peer marked invalid -- which is what ignoring does --
// is never delivered, however current its version, because the bytes are not
// on their disk.
//
// The second result says the peer is holding the file back, as opposed to
// merely not having got round to it: that one does not fix itself.
func (m *model) DesuqPeerHasLocal(folder string, device protocol.DeviceID, name string) (has, heldBack bool, err error) {
	local, lok, err := m.sdb.GetDeviceFile(folder, protocol.LocalDeviceID, name)
	if err != nil {
		return false, false, err
	}
	remote, rok, err := m.sdb.GetDeviceFile(folder, device, name)
	if err != nil {
		return false, false, err
	}
	localGone := !lok || local.Deleted
	if !rok {
		return localGone, false, nil
	}
	if remote.IsInvalid() {
		return false, true, nil
	}
	if localGone {
		return remote.Deleted, false, nil
	}
	return remote.Version.Equal(local.Version), false, nil
}
