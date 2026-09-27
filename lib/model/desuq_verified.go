// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork: a new file inside an
// upstream package. It costs two lines in model.go -- the call at the top of
// OnHello and the revocation check in CommitConfiguration -- and lib/api finds
// DesuqUnverifiedKnocks by an optional interface, so the Model interface and
// its mocks are untouched. See custom/CUSTOMIZATIONS.md.
//
// NOTHING SYNCS WITH A DEVICE UNTIL IT HAS BEEN VERIFIED
//
// Adding a device proves two installations agreed on an ID, not whose ID it
// is. The verification card (gui/default/syncthing/desuq/
// deviceHandshakeDirective.js) is how two people prove it, and for as long as
// it was optional it was skipped: a grey "Not verified yet" badge and no
// consequence. So it is now the condition for connecting at all.
//
// The gate is here, after the Hello exchange and before AddConnection,
// because this is the one point every connection passes -- inbound and
// outbound, every transport -- before any index, request or cluster config
// can move. Gating folders instead would have meant guarding every path that
// reads SharedWith, and missing one would leak data. A refused connection
// leaks nothing.
//
// It is after Hello rather than in connectionCheckEarly for one reason: Hello
// carries the name the other side calls itself, and the device ID in it is
// already authenticated by TLS. So a refused device is remembered here as a
// "knock", and the main screen can say "Kai's computer is trying to connect
// -- compare cards to let it in" instead of a bare "Offline" that looks like
// their machine is switched off.
//
// The record is `desuqVerifiedAt` on the device's own config entry. The card
// was first kept in the browser's localStorage, on the reasoning that config
// "would sync a claim about identity between machines". It does not: config.xml
// is never sent to anybody. Keeping it in the browser meant the tray's browser
// and any other browser disagreed about who was verified, and no server-side
// code could act on it.
//
// This is protection against a swapped device ID, not against the person at
// this keyboard: the field is theirs to set, through the card, through the
// advanced settings, or by editing config.xml. What it removes is doing so by
// accident, or by not knowing there was anything to do.

package model

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

var errDeviceUnverified = errors.New("device has not been verified on this computer (compare cards to let it connect)")

// DesuqKnock is one refused attempt by a configured but unverified device.
type DesuqKnock struct {
	Name    string    `json:"name"`
	Address string    `json:"address"`
	At      time.Time `json:"at"`
}

var (
	desuqKnockMut sync.Mutex
	// model -> device -> the last refused attempt. Keyed by model so tests
	// standing up several models in one process do not see each other's.
	desuqKnocks = map[*model]map[protocol.DeviceID]DesuqKnock{}
)

// desuqVerifiedGate is the one call OnHello makes into this file. It lets
// unknown devices through, so upstream's pending-device handling still sees
// them, and refuses a configured device that has not been verified.
func (m *model) desuqVerifiedGate(remoteID protocol.DeviceID, addr net.Addr, hello protocol.Hello) error {
	cfg, ok := m.cfg.Device(remoteID)
	if !ok {
		return nil
	}

	desuqKnockMut.Lock()
	defer desuqKnockMut.Unlock()
	if cfg.DesuqVerifiedAt != "" {
		delete(desuqKnocks[m], remoteID)
		return nil
	}
	knocks := desuqKnocks[m]
	if knocks == nil {
		knocks = map[protocol.DeviceID]DesuqKnock{}
		desuqKnocks[m] = knocks
	}
	address := ""
	if addr != nil {
		address = addr.String()
	}
	knocks[remoteID] = DesuqKnock{Name: hello.DeviceName, Address: address, At: time.Now()}
	return errDeviceUnverified
}

// desuqVerificationRevoked reports whether a config change took a device's
// verification away, which has to close its connections the same way pausing
// does: the gate above only runs when a connection starts.
func desuqVerificationRevoked(from, to string) bool {
	return from != "" && to == ""
}

// DesuqUnverifiedKnocks returns every configured device refused for being
// unverified, and when it last tried, for devices that are still unverified.
func (m *model) DesuqUnverifiedKnocks() map[protocol.DeviceID]DesuqKnock {
	desuqKnockMut.Lock()
	defer desuqKnockMut.Unlock()
	out := make(map[protocol.DeviceID]DesuqKnock, len(desuqKnocks[m]))
	for id, k := range desuqKnocks[m] {
		if cfg, ok := m.cfg.Device(id); ok && cfg.DesuqVerifiedAt == "" {
			out[id] = k
		}
	}
	return out
}
