// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go. See custom/CUSTOMIZATIONS.md.
//
// GET /rest/db/whohas?folder=&file= answers "does everybody have my version
// of this file", per person, including the people who are not connected.
//
// Upstream's /rest/db/file carries an availability list, and it cannot answer
// this: it lists only devices that are *connected* and have the global
// version. Somebody whose computer is off is simply absent, which reads the
// same as somebody who does not have the file. The peer's own index is still
// here while they are away, and it is what this reads -- the same comparison
// the claims use to say whether a mark was delivered
// (lib/model/desuq_claimdelivery.go).

package api

import (
	"net/http"
	"sort"

	"github.com/syncthing/syncthing/lib/protocol"
)

type whoHasPeer struct {
	Device    string `json:"device"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	// Has is the peer's index holding this computer's version of the file.
	Has bool `json:"has"`
	// HeldBack is the peer keeping the file off their computer on purpose:
	// selective sync, or an ignore pattern.
	HeldBack bool `json:"heldBack"`
}

type whoHasResponse struct {
	Folder string `json:"folder"`
	File   string `json:"file"`
	// Exists is whether the file exists for anybody at all.
	Exists bool `json:"exists"`
	// Behind is this computer not having the newest version itself. Then
	// "they do not have my version" is the wrong way round, and the answer
	// is that a newer one is on its way here.
	Behind bool `json:"behind"`
	// ModifiedBy is who made the newest version, by name ("You" for this
	// device), and Modified when.
	ModifiedBy string       `json:"modifiedBy"`
	Modified   string       `json:"modified,omitempty"`
	Peers      []whoHasPeer `json:"peers"`
}

func (s *service) getDBWhoHas(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	cfg, ok := s.cfg.Folders()[qs.Get("folder")]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	name, ok := cleanClaimPath(qs.Get("file"))
	if !ok {
		http.Error(w, errClaimBadPath.Error(), http.StatusBadRequest)
		return
	}
	cd, ok := s.model.(claimDelivery)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}

	gf, gok, err := s.model.CurrentGlobalFile(cfg.ID, name)
	if err != nil {
		httpError(w, err)
		return
	}
	lf, lok, err := s.model.CurrentFolderFile(cfg.ID, name)
	if err != nil {
		httpError(w, err)
		return
	}

	res := whoHasResponse{Folder: cfg.ID, File: name, Peers: []whoHasPeer{}}
	res.Exists = gok && !gf.Deleted
	if gok {
		res.Behind = !lok || !lf.Version.Equal(gf.Version)
		res.Modified = gf.ModTime().Format("2006-01-02T15:04:05Z07:00")
	}

	devices := s.cfg.Devices()
	nameOf := func(dev protocol.DeviceID) string {
		if dev == s.id {
			return "You"
		}
		if d, ok := devices[dev]; ok && d.Name != "" {
			return d.Name
		}
		return dev.Short().String()
	}
	if gok {
		res.ModifiedBy = gf.ModifiedBy.String()
		for _, fd := range cfg.Devices {
			if fd.DeviceID.Short() == gf.ModifiedBy {
				res.ModifiedBy = nameOf(fd.DeviceID)
			}
		}
	}

	for _, fd := range cfg.Devices {
		if fd.DeviceID == s.id {
			continue
		}
		has, heldBack, err := cd.DesuqPeerHasLocal(cfg.ID, fd.DeviceID, name)
		if err != nil {
			httpError(w, err)
			return
		}
		res.Peers = append(res.Peers, whoHasPeer{
			Device:    fd.DeviceID.String(),
			Name:      nameOf(fd.DeviceID),
			Connected: s.model.ConnectedTo(fd.DeviceID),
			Has:       has,
			HeldBack:  heldBack,
		})
	}
	sort.Slice(res.Peers, func(i, j int) bool { return res.Peers[i].Name < res.Peers[j].Name })
	sendJSON(w, res)
}
