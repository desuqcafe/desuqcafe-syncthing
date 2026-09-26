// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// Pin a version: "keep this copy, whatever the thirty-day cleanup says". The
// store and the cleanup guards are in lib/versioner/desuq_pins.go; this is the
// button's endpoint. The History screen reads pins back through
// /rest/folder/history, which marks each pinned version.

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/syncthing/syncthing/lib/versioner"
)

type pinRequest struct {
	Folder      string    `json:"folder"`
	File        string    `json:"file"`
	VersionTime time.Time `json:"versionTime"`
	Pinned      bool      `json:"pinned"`
}

var errPinNoSuchVersion = errors.New("that copy is no longer in the archive")

// postFolderPin pins or unpins one archived copy.
//
// Body: {"folder": id, "file": "Scenes/cabin.blend",
// "versionTime": "2026-09-26T15:02:11+02:00", "pinned": true}.
//
// Pinning checks the copy is really there first: a pin on nothing would show
// as protection that does not exist. Unpinning never checks, so a pin whose
// copy has gone can still be taken back.
func (s *service) postFolderPin(w http.ResponseWriter, r *http.Request) {
	var req pinRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	if req.File == "" || req.VersionTime.IsZero() {
		http.Error(w, "file and versionTime are required", http.StatusBadRequest)
		return
	}

	if req.Pinned {
		if !s.folderHasVersioning(req.Folder) {
			http.Error(w, "this folder does not keep older copies", http.StatusConflict)
			return
		}
		versions, err := s.model.GetFolderVersions(req.Folder)
		if err != nil {
			forkHTTPError(w, err)
			return
		}
		if !hasVersion(versions[req.File], req.VersionTime) {
			http.Error(w, errPinNoSuchVersion.Error(), http.StatusNotFound)
			return
		}
	}

	if err := versioner.SetPinned(cfg, req.File, req.VersionTime, req.Pinned); err != nil {
		httpError(w, err)
		return
	}
	sendJSON(w, map[string]bool{"pinned": req.Pinned})
}

// hasVersion matches to the second, which is the archive's resolution.
func hasVersion(list []versioner.FileVersion, at time.Time) bool {
	at = at.Truncate(time.Second)
	for _, v := range list {
		if v.VersionTime.Truncate(time.Second).Equal(at) {
			return true
		}
	}
	return false
}
