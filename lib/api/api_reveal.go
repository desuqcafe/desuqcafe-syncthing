// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go, so that the divergence from upstream
// stays one line in a file upstream edits often. See custom/CUSTOMIZATIONS.md.
//
// The web UI shows a folder's path and can do nothing with it. A browser
// cannot open a file manager: a file:// link from an http:// page is blocked
// outright, and there is no way back to the desktop from inside the tab. So
// "where are my files" is answered by a path the reader has to select, copy
// and paste into an Explorer window -- for an audience picked precisely
// because they should not have to.
//
// Only the server can do this, so this is the one thing in the fork's GUI that
// needed a route rather than an existing one. What it will open is deliberately
// narrow: a directory, resolved from a configured folder ID, verified to be
// inside that folder's root. It never takes a path from the caller directly,
// which is what keeps "open this" from becoming "run this" -- explorer.exe
// hands an executable to the shell, and the shell runs it.

package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/syncthing/syncthing/lib/fs"
)

var errBadSubPath = errors.New("sub must be a path inside the folder")

// postSystemReveal opens a synced folder in the host's file manager.
//
// Parameters: folder (required, a configured folder ID) and sub (optional, a
// path relative to that folder's root, slash-separated as everywhere else in
// the API). Answers 204 once the file manager has been started -- not once it
// has appeared, which is not knowable and would mean holding the request open
// for as long as the shell takes.
func (s *service) postSystemReveal(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()

	folder := qs.Get("folder")
	if folder == "" {
		http.Error(w, "folder parameter is required", http.StatusBadRequest)
		return
	}

	cfg, ok := s.cfg.Folders()[folder]
	if !ok {
		http.Error(w, "no such folder", http.StatusNotFound)
		return
	}

	root, err := fs.ExpandTilde(cfg.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	root = filepath.Clean(root)

	target, err := targetWithin(root, qs.Get("sub"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Walk up to something that exists. A folder whose drive is not plugged in
	// gives nothing to open and should say so; a sub-path that has been
	// deleted since the page rendered is better answered by its parent than by
	// an error, which is what a file manager would do anyway.
	dir, err := nearestExistingDir(root, target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := revealDir(dir); err != nil {
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// targetWithin resolves sub against root and refuses anything that leaves it.
//
// The sub-path is checked rather than trusted even though today's only caller
// is the fork's own main screen: the whole value of taking a folder ID instead
// of a path is that the set of openable directories is bounded by the config,
// and an unchecked sub-path would hand that back.
func targetWithin(root, sub string) (string, error) {
	if sub == "" {
		return root, nil
	}
	if strings.ContainsAny(sub, "\x00") {
		return "", errBadSubPath
	}

	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(sub)))

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", errBadSubPath
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errBadSubPath
	}
	return target, nil
}

// nearestExistingDir returns the deepest existing directory at or above target,
// never going above root. A target that is a file resolves to its parent: a
// file manager opened on a file either does nothing or, on Windows, hands it to
// the shell to *run*, which is not what "show me where this is" means.
func nearestExistingDir(root, target string) (string, error) {
	for dir := target; ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if info.IsDir() {
				return dir, nil
			}
			// A file. Its parent is inside root by construction, since dir is.
			return filepath.Dir(dir), nil
		}

		if dir == root || filepath.Dir(dir) == dir {
			return "", err
		}
	}
}
