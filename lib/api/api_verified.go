// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds two lines to api.go. See custom/CUSTOMIZATIONS.md.
//
// GET /rest/cluster/unverified answers "who is trying to connect and being
// refused because nobody here has compared cards with them yet". Nothing
// syncs with an unverified device (lib/model/desuq_verified.go), and without
// this the only thing the screen could say about them is "Offline" -- which
// reads as a computer that is switched off, and sends people looking for the
// wrong problem.
//
// GET /rest/cluster/pending/members answers "who else is in this folder I am
// being offered". Upstream's offer carries a label and the device offering
// it; the rest of the membership is in that device's cluster config, which
// the fork already keeps (lib/model/desuq_hub.go). Accepting a folder is
// joining a group of people, and the offer should say which.

package api

import (
	"net/http"
	"sort"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
)

type unverifiedKnocks interface {
	DesuqUnverifiedKnocks() map[protocol.DeviceID]model.DesuqKnock
}

func (s *service) getClusterUnverified(w http.ResponseWriter, _ *http.Request) {
	uk, ok := s.model.(unverifiedKnocks)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}
	out := map[string]model.DesuqKnock{}
	for id, k := range uk.DesuqUnverifiedKnocks() {
		out[id.String()] = k
	}
	sendJSON(w, out)
}

// offerMember is one person in a folder somebody is offering.
type offerMember struct {
	Device string `json:"device"`
	// Name is what this computer calls them if it knows them, and otherwise
	// what the offering device calls them.
	Name string `json:"name"`
	// Offering marks the device the offer came from.
	Offering bool `json:"offering"`
	// Known is whether they are in this computer's device list at all, and
	// Verified whether they have been verified here. Somebody unknown will not
	// sync with this computer directly; everything from them comes through
	// the people who are known.
	Known    bool `json:"known"`
	Verified bool `json:"verified"`
}

func (s *service) getPendingMembers(w http.ResponseWriter, _ *http.Request) {
	cf, ok := s.model.(clusterFolders)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}
	pending, err := s.model.PendingFolders(protocol.EmptyDeviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	offers := map[string][]protocol.DeviceID{}
	for folder, pf := range pending {
		for dev := range pf.OfferedBy {
			offers[folder] = append(offers[folder], dev)
		}
	}
	sendJSON(w, offerMembers(offers, s.id, cf.DesuqClusterFolders(), s.cfg.Devices()))
}

// offerMembers is the whole rule, on its own so it can be tested without a
// model: folder -> offering device -> everybody in that device's list apart
// from this computer, the offering device first.
func offerMembers(offers map[string][]protocol.DeviceID, me protocol.DeviceID,
	cc map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice,
	devices map[protocol.DeviceID]config.DeviceConfiguration,
) map[string]map[string][]offerMember {
	out := map[string]map[string][]offerMember{}
	for folder, offerers := range offers {
		byOfferer := map[string][]offerMember{}
		for _, from := range offerers {
			var members []offerMember
			seen := map[protocol.DeviceID]bool{me: true}
			add := func(id protocol.DeviceID, theirName string) {
				if seen[id] {
					return
				}
				seen[id] = true
				m := offerMember{Device: id.String(), Name: theirName, Offering: id == from}
				if d, ok := devices[id]; ok {
					m.Known = true
					m.Verified = d.DesuqVerifiedAt != ""
					if d.Name != "" {
						m.Name = d.Name
					}
				}
				if m.Name == "" {
					m.Name = id.Short().String()
				}
				members = append(members, m)
			}
			add(from, "")
			rest := append([]model.DesuqPeerFolderDevice(nil), cc[from][folder]...)
			sort.SliceStable(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
			for _, d := range rest {
				add(d.ID, d.Name)
			}
			byOfferer[from.String()] = members
		}
		out[folder] = byOfferer
	}
	return out
}
