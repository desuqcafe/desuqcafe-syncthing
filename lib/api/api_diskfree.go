// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go, so that the divergence from upstream
// stays one line in a file upstream edits often. See custom/CUSTOMIZATIONS.md.
//
// Syncthing enforces minDiskFree per file as it pulls, but never reports how
// much space there actually is: there is no REST endpoint for it and the GUI
// only ever shows the configured reserve. That leaves someone able to accept a
// share far larger than their disk, and find out only when the folder wedges
// at "Out of Sync" with the drive full. This endpoint is the missing half of
// that picture.

package api

import (
	"net/http"
	"path/filepath"

	"github.com/syncthing/syncthing/lib/fs"
)

type diskFreeResponse struct {
	// Path as asked for, after tilde expansion.
	Path string `json:"path"`
	// The existing directory the figures were actually measured on. For a
	// folder path that has not been created yet this is an ancestor, which is
	// the useful answer: it is the same filesystem the folder will land on.
	MeasuredPath string `json:"measuredPath"`
	Free         uint64 `json:"free"`
	Total        uint64 `json:"total"`
}

func (s *service) getSystemDiskFree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	expanded, err := fs.ExpandTilde(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	measured, usage, err := usageForNearestExisting(expanded)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	sendJSON(w, diskFreeResponse{
		Path:         expanded,
		MeasuredPath: measured,
		Free:         usage.Free,
		Total:        usage.Total,
	})
}

// usageForNearestExisting walks up from path until a directory it can measure
// is found. The GUI asks about a path while the user is still typing it, and a
// folder is routinely pointed at a directory that does not exist yet -- but
// the drive it will be created on does, and that is what the caller wants to
// know about.
func usageForNearestExisting(path string) (string, fs.Usage, error) {
	var lastErr error
	for dir := filepath.Clean(path); ; {
		usage, err := fs.NewFilesystem(fs.FilesystemTypeBasic, dir).Usage(".")
		if err == nil {
			return dir, usage, nil
		}
		lastErr = err

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding anything.
			return "", fs.Usage{}, lastErr
		}
		dir = parent
	}
}
