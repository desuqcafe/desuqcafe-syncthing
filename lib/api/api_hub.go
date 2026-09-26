// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds two lines to api.go. See custom/CUSTOMIZATIONS.md.
//
// GET /rest/db/hub answers "does everybody in this folder sync with
// everybody else, or does it all go through one computer" -- from both ends:
//
//   - through: pairs of this computer's peers who do not share the folder
//     with each other, so every change between them passes through here.
//     When this computer is off, they stop syncing with each other.
//   - via: people in the folder who are not connected to this computer at
//     all, whose changes arrive only through one of its peers.
//
// POST /rest/db/hub/connect {folder, device} is the way out from the edge:
// add that person as a device and share the folder with them. The device ID
// and name come from the peer's cluster config (lib/model/desuq_hub.go), and
// the request is refused for any device that is not in one -- this is "add
// the person Alex already shares this with", not a general add-device
// route. The other person is then asked to accept, the same as for any new
// device, and the fork's verification card applies (DEPLOYMENT §7).
//
// Why not the introducer flag, which is Syncthing's own answer: it has to be
// set on the edge computers, about the middle one, and it makes that
// computer's future device and folder choices theirs too -- including
// removals. That is a standing grant of trust. This is one person, one
// folder, one click, and it is visible in the device list afterwards.

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
)

type clusterFolders interface {
	DesuqClusterFolders() map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice
}

type hubPerson struct {
	Device    string `json:"device"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

type hubPair struct {
	A hubPerson `json:"a"`
	B hubPerson `json:"b"`
}

type hubVia struct {
	hubPerson
	// Via is who their changes arrive through.
	Via []hubPerson `json:"via"`
}

type hubFolder struct {
	Folder  string    `json:"folder"`
	Label   string    `json:"label"`
	Through []hubPair `json:"through"`
	Via     []hubVia  `json:"via"`
}

type hubResponse struct {
	Folders []hubFolder `json:"folders"`
}

var errHubUnknownDevice = errors.New("that device is not in this folder at any of your peers")

func (s *service) getDBHub(w http.ResponseWriter, _ *http.Request) {
	cf, ok := s.model.(clusterFolders)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}
	cc := cf.DesuqClusterFolders()
	res := hubResponse{Folders: []hubFolder{}}
	folders := s.cfg.Folders()
	ids := make([]string, 0, len(folders))
	for id := range folders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		hf := hubAnalyse(folders[id], s.id, cc, s.cfg.Devices(), s.model.ConnectedTo)
		if len(hf.Through) > 0 || len(hf.Via) > 0 {
			res.Folders = append(res.Folders, hf)
		}
	}
	sendJSON(w, res)
}

// hubAnalyse is the whole rule, on its own so it can be tested without a
// model. A peer whose cluster config does not mention the folder has not
// accepted it, which is a different problem with its own sentence on the
// main screen; it contributes nothing here, either way.
func hubAnalyse(f config.FolderConfiguration, me protocol.DeviceID,
	cc map[protocol.DeviceID]map[string][]model.DesuqPeerFolderDevice,
	devices map[protocol.DeviceID]config.DeviceConfiguration,
	connected func(protocol.DeviceID) bool,
) hubFolder {
	hf := hubFolder{Folder: f.ID, Label: f.Label, Through: []hubPair{}, Via: []hubVia{}}
	if hf.Label == "" {
		hf.Label = f.ID
	}
	person := func(id protocol.DeviceID, fallback string) hubPerson {
		p := hubPerson{Device: id.String(), Name: fallback, Connected: connected(id)}
		if d, ok := devices[id]; ok && d.Name != "" {
			p.Name = d.Name
		}
		if p.Name == "" {
			p.Name = id.Short().String()
		}
		return p
	}

	var peers []protocol.DeviceID
	mine := map[protocol.DeviceID]bool{}
	for _, d := range f.Devices {
		mine[d.DeviceID] = true
		if d.DeviceID != me {
			peers = append(peers, d.DeviceID)
		}
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Compare(peers[j]) < 0 })

	// theirs reports whether peer p shares the folder with q, and whether
	// the answer is known at all.
	theirs := func(p, q protocol.DeviceID) (shares, known bool) {
		devs, ok := cc[p][f.ID]
		if !ok {
			return false, false
		}
		for _, d := range devs {
			if d.ID == q {
				return true, true
			}
		}
		return false, true
	}

	for i := range peers {
		for j := i + 1; j < len(peers); j++ {
			p, q := peers[i], peers[j]
			pq, pk := theirs(p, q)
			qp, qk := theirs(q, p)
			// Direct sync needs both sides to share with each other, so
			// either one not doing so is enough. But only between two people
			// both known to have the folder: somebody who has never
			// connected, or not accepted it, has a different problem, and
			// the main screen already says that one. Seen on a pair: a
			// device that had never connected was paired off as "only
			// syncing through this computer" with somebody it had never met.
			if pk && qk && (!pq || !qp) {
				hf.Through = append(hf.Through, hubPair{A: person(p, ""), B: person(q, "")})
			}
		}
	}

	via := map[protocol.DeviceID]*hubVia{}
	var order []protocol.DeviceID
	for _, p := range peers {
		for _, d := range cc[p][f.ID] {
			if d.ID == me || d.ID == p || mine[d.ID] {
				continue
			}
			v := via[d.ID]
			if v == nil {
				v = &hubVia{hubPerson: person(d.ID, d.Name)}
				via[d.ID] = v
				order = append(order, d.ID)
			}
			v.Via = append(v.Via, person(p, ""))
		}
	}
	for _, id := range order {
		hf.Via = append(hf.Via, *via[id])
	}
	return hf
}

type hubConnectRequest struct {
	Folder string `json:"folder"`
	Device string `json:"device"`
}

func (s *service) postDBHubConnect(w http.ResponseWriter, r *http.Request) {
	var req hubConnectRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cf, ok := s.model.(clusterFolders)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}
	fcfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	dev, err := protocol.DeviceIDFromString(req.Device)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hf := hubAnalyse(fcfg, s.id, cf.DesuqClusterFolders(), s.cfg.Devices(), s.model.ConnectedTo)
	var target *hubVia
	for i := range hf.Via {
		if hf.Via[i].Device == dev.String() {
			target = &hf.Via[i]
		}
	}
	if target == nil {
		http.Error(w, errHubUnknownDevice.Error(), http.StatusNotFound)
		return
	}

	defaults := s.cfg.DefaultDevice()
	waiter, err := s.cfg.Modify(func(cfg *config.Configuration) {
		if _, _, ok := cfg.Device(dev); !ok {
			d := defaults.Copy()
			d.DeviceID = dev
			d.Name = target.Name
			cfg.SetDevice(d)
		}
		f, _, ok := cfg.Folder(fcfg.ID)
		if !ok || f.SharedWith(dev) {
			return
		}
		f.Devices = append(f.Devices, config.FolderDeviceConfiguration{DeviceID: dev})
		cfg.SetFolder(f)
	})
	if err != nil {
		httpError(w, err)
		return
	}
	waiter.Wait()
	sendJSON(w, map[string]string{"device": dev.String(), "name": target.Name})
}
