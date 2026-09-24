// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go. See custom/CUSTOMIZATIONS.md.
//
// GET /rest/db/peerheldback?folder=&device= says how much of a folder another
// device is deliberately not keeping. See lib/model/desuq_peerheldback.go for
// why completion cannot.

package api

import (
	"net/http"

	"github.com/syncthing/syncthing/lib/protocol"
)

// peerHeldBacker is satisfied by the real model. An optional interface rather
// than a method on model.Model, so that interface and its generated mocks stay
// upstream's; a model without it answers 501 and the GUI keeps its old words.
type peerHeldBacker interface {
	DesuqPeerHeldBack(folder string, device protocol.DeviceID) (int, int64, error)
}

type peerHeldBackResponse struct {
	Folder string `json:"folder"`
	Device string `json:"device"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

func (s *service) getDBPeerHeldBack(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	folder := qs.Get("folder")
	if _, ok := s.cfg.Folders()[folder]; !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	device, err := protocol.DeviceIDFromString(qs.Get("device"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hb, ok := s.model.(peerHeldBacker)
	if !ok {
		http.Error(w, "not supported by this build", http.StatusNotImplemented)
		return
	}
	files, bytes, err := hb.DesuqPeerHeldBack(folder, device)
	if err != nil {
		httpError(w, err)
		return
	}
	sendJSON(w, peerHeldBackResponse{Folder: folder, Device: device.String(), Files: files, Bytes: bytes})
}
