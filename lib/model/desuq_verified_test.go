// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See desuq_verified.go.

package model

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

func TestDesuqVerifiedGate(t *testing.T) {
	w, fcfg := newDefaultCfgWrapper(t)
	m := setupModel(t, w)
	defer cleanupModelAndRemoveDir(m, fcfg.Filesystem().URI())

	addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22000}
	hello := protocol.Hello{DeviceName: "Kai's laptop"}

	// device1 is configured and has never been verified.
	if err := m.OnHello(device1, addr, hello); !errors.Is(err, errDeviceUnverified) {
		t.Fatalf("an unverified device must be refused, got %v", err)
	}
	k, ok := m.DesuqUnverifiedKnocks()[device1]
	if !ok || k.Name != "Kai's laptop" || k.Address != addr.String() {
		t.Fatalf("the refusal should be remembered with the name they sent: %+v", k)
	}

	// An unknown device still goes to upstream's pending handling.
	unknown := protocol.DeviceID{9, 9, 9}
	if err := m.OnHello(unknown, addr, hello); !errors.Is(err, errDeviceUnknown) {
		t.Errorf("an unknown device should reach upstream's check, got %v", err)
	}
	if _, ok := m.DesuqUnverifiedKnocks()[unknown]; ok {
		t.Error("an unknown device is pending, not an unverified knock")
	}

	// Verify it: let in, and the knock is gone.
	d, _ := w.Device(device1)
	d.DesuqVerifiedAt = time.Now().Format(time.RFC3339)
	setDevice(t, w, d)
	if err := m.OnHello(device1, addr, hello); err != nil {
		t.Fatalf("a verified device must be let in, got %v", err)
	}
	if len(m.DesuqUnverifiedKnocks()) != 0 {
		t.Error("the knock should clear once verified")
	}

	// Take it away again: refused again.
	d.DesuqVerifiedAt = ""
	setDevice(t, w, d)
	if err := m.OnHello(device1, addr, hello); !errors.Is(err, errDeviceUnverified) {
		t.Errorf("revoking verification must refuse the next connection, got %v", err)
	}
}

func TestDesuqVerificationRevoked(t *testing.T) {
	if !desuqVerificationRevoked("2026-09-27T10:00:00Z", "") {
		t.Error("verified -> empty is a revocation")
	}
	for _, c := range [][2]string{{"", ""}, {"", "x"}, {"x", "y"}, {"x", "x"}} {
		if desuqVerificationRevoked(c[0], c[1]) {
			t.Errorf("%q -> %q is not a revocation", c[0], c[1])
		}
	}
}

// The gate only runs when a connection starts, so taking verification away
// from a device that is already connected has to close it, as pausing does.
func TestDesuqRevokingClosesTheConnection(t *testing.T) {
	w, fcfg := newDefaultCfgWrapper(t)
	d, _ := w.Device(device1)
	d.DesuqVerifiedAt = time.Now().Format(time.RFC3339)
	setDevice(t, w, d)
	m, fc := setupModelWithConnectionFromWrapper(t, w)
	defer cleanupModelAndRemoveDir(m, fcfg.Filesystem().URI())

	d.DesuqVerifiedAt = ""
	setDevice(t, w, d)
	select {
	case <-fc.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection to a device that lost its verification was left open")
	}
}
