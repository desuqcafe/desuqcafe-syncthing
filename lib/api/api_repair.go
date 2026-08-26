// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// A folder whose directory is not there stops, says "folder path missing", and
// waits. On the machine this fork is written for that state had been sitting
// on one of three folders since 24 August -- for two days, on a screen that
// says "Stopped" and offers nothing to press. The fix is one of three things,
// none of them obvious to somebody who does not know what a folder marker is:
// plug the drive back in, point the folder at wherever it lives now, or make
// the directory again.
//
// This endpoint is the third one, and it exists mainly so that the other two
// can be *said*. A card that offers "Create it again" alongside "Find it..."
// is a card that has explained the situation.
//
// THE RAIL THAT MATTERS
//
// Making the directory again is the dangerous one, and it is dangerous in a
// way that looks like success. Syncthing refuses to run a folder with no
// marker precisely because an empty directory where a full one used to be is
// indistinguishable from "the user deleted everything" -- and a send-receive
// folder that scans an empty root announces every one of those deletions to
// everybody else. The drive being unplugged becomes the whole team losing the
// asset library, at the speed of a LAN.
//
// So: this will not create a directory for a folder whose index still holds
// files. The refusal names the count, because "45 files" is what makes the
// sentence land. The one case where an empty root is fine is a folder that
// never had anything in it -- a share accepted and never filled, which is
// exactly the folder that has been stuck on the author's own machine.
//
// Recreating the *marker* is the mild one. A marker missing from a directory
// that still has files in it means somebody's cleaner deleted a dot-directory,
// and putting it back is what upstream's own CreateMarker is for. The rail
// there is only that the directory must not be empty: an empty root with a
// missing marker is the remounted-drive case again wearing a different hat.

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

// repairResponse says what was done, in words the card shows verbatim.
type repairResponse struct {
	Folder string `json:"folder"`
	// Created and Marker say which of the two repairs actually happened, so
	// the caller does not have to parse the sentence to know.
	Created bool   `json:"created"`
	Marker  bool   `json:"marker"`
	Message string `json:"message"`
}

// postFolderRepair makes a stopped folder startable again, where that is safe.
func (s *service) postFolderRepair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}

	pathErr := cfg.CheckPath()
	if pathErr == nil {
		sendJSON(w, repairResponse{
			Folder:  req.Folder,
			Message: "That folder is where it should be. Nothing needed fixing.",
		})
		return
	}
	if !errors.Is(pathErr, config.ErrPathMissing) && !errors.Is(pathErr, config.ErrMarkerMissing) {
		// Something else: a path that is a file, a permission problem, a
		// filesystem that will not answer. Not this endpoint's business, and
		// guessing at it would be worse than saying so.
		http.Error(w, pathErr.Error(), http.StatusConflict)
		return
	}

	indexed := s.repairIndexedFiles(req.Folder)
	verdict := repairVerdict(
		errors.Is(pathErr, config.ErrPathMissing),
		repairRootHasContent(cfg),
		indexed,
	)
	if verdict.reason != "" {
		http.Error(w, verdict.reason, http.StatusConflict)
		return
	}

	res := repairResponse{Folder: req.Folder}
	if verdict.createRoot {
		if err := cfg.CreateRoot(); err != nil {
			httpError(w, err)
			return
		}
		res.Created = true
	}
	if err := cfg.CreateMarker(); err != nil {
		httpError(w, err)
		return
	}
	res.Marker = true

	// A folder in the error state is not scanning, and nothing else will
	// prompt it to look again -- upstream retries on its own timer, and this
	// is a person standing in front of the screen waiting for an answer.
	s.model.ScanFolderSubdirs(req.Folder, nil)

	switch {
	case res.Created:
		res.Message = "The folder has been created again. It is empty, so anything the others have will now be copied into it."
	default:
		res.Message = "The folder marker has been put back. Syncing should start again on its own."
	}
	sendJSON(w, res)
}

// repairDecision is what the rails allow.
type repairDecision struct {
	createRoot bool
	// reason is empty when the repair may go ahead, and the sentence shown to
	// the person when it may not.
	reason string
}

// repairVerdict is the whole safety argument, pure so it can be proved without
// a filesystem. See the file comment for why the empty-root cases are the ones
// that matter.
//
// indexed is how many files the folder's *global* index holds. The global one,
// not the local: local state is blanked for anything ignored, and this fork
// has been caught by that four times already.
func repairVerdict(pathMissing, rootHasContent bool, indexed int) repairDecision {
	if pathMissing {
		if indexed > 0 {
			return repairDecision{reason: repairWouldAnnounceDeletion(indexed)}
		}
		// Nothing has ever been in it, so an empty directory is exactly right.
		return repairDecision{createRoot: true}
	}

	// The directory is there and the marker is not.
	if indexed > 0 && !rootHasContent {
		// The shape of a drive that came back empty, or a different disk
		// mounted at the same letter. Putting the marker back here is what
		// turns that into a deletion everybody receives.
		return repairDecision{reason: repairWouldAnnounceDeletion(indexed)}
	}
	return repairDecision{}
}

func repairWouldAnnounceDeletion(indexed int) string {
	return "This folder is supposed to hold " + repairItoa(indexed) + " files, and the directory is empty. " +
		"Setting it up again from here would tell everybody else that you deleted them, and they would " +
		"delete their copies too. Plug the drive back in, or use Settings to point this folder at where " +
		"it lives now."
}

// repairRootHasContent reports whether the folder root has anything in it
// besides Syncthing's own bookkeeping.
//
// The marker and the version archive are excluded because both can exist in a
// root that holds none of the user's files, and "there is a .stversions here"
// is not evidence that the drive came back with the data on it.
func repairRootHasContent(cfg config.FolderConfiguration) bool {
	names, err := cfg.Filesystem().DirNames(".")
	if err != nil {
		return false
	}
	for _, name := range names {
		switch name {
		case cfg.MarkerName, config.DefaultMarkerName, ".stversions", ".stignore":
			continue
		}
		return true
	}
	return false
}

// repairIndexedFiles counts what the global index holds for the folder --
// files only, deletions excluded, because a folder whose entire index is
// tombstones really has nothing to lose.
func (s *service) repairIndexedFiles(folder string) int {
	var n int
	it, errFn := s.model.AllGlobalFiles(folder)
	for f := range it {
		if f.Deleted || f.Type != protocol.FileInfoTypeFile {
			continue
		}
		n++
	}
	if err := errFn(); err != nil {
		// Unreadable index: report something rather than zero. Zero is the
		// answer that unlocks the destructive path, and it must never be
		// reached by accident.
		return 1
	}
	return n
}

// repairItoa avoids pulling strconv in for one call.
func repairItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
