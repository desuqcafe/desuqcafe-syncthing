// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. Like api_history.go and
// api_reclaim.go it lives in its own file and costs api.go two lines. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// Two people editing the same .blend file is not an edge case for this fork,
// it is the normal week. Syncthing handles it correctly and then stops: the
// losing copy is renamed to
//
//	scene.sync-conflict-20260824-032916-F67Q3OS.blend
//
// and left in the folder. The tray raises a toast naming scene.blend, and
// after that the product has nothing more to say. A modeller is left with two
// files, one of them named something they will not type, no way to tell which
// is which, and a strong suspicion that deleting either one loses work. What
// actually happens next is that nobody deletes anything, and a year later the
// asset folder has four hundred conflict copies in it.
//
// Binary assets cannot be merged, so the only honest resolution is a choice
// between two whole files. This endpoint lists the choices and performs them.
//
// WHO MADE WHICH COPY -- THE TRAP
//
// The short device ID in the filename is NOT the author of the copy it names.
// lib/model/folder_sendrecv.go moves the *local* file aside with
//
//	moveForConflict(name, file.ModifiedBy.String(), scanChan)
//
// where `file` is the *incoming* version -- the one that won. So the bytes
// inside the conflict copy are the losing edit, and the ID in its name belongs
// to whoever made the winning one. Reading that ID as "Kai's copy" gets the
// attribution exactly backwards, which for a screen whose whole job is "which
// of these two is yours" is worse than saying nothing.
//
// The index knows the real answer for both copies. A file's ModifiedBy is the
// device that last wrote it, and the conflict copy is a file like any other:
// its ModifiedBy is the device that set it aside, and that device's own edit
// is what is inside it. So `who` comes from the index on both sides and the
// filename is read only for `when`. Fifth instance of the pattern this fork
// keeps meeting -- a value that looks like an answer and is about something
// else. See DEPLOYMENT-3D-TEAM.md section 18.
//
// WHY RESOLVING IS A RENAME AND AN ARCHIVE, NEVER A DELETE
//
// Both resolutions keep both copies:
//
//   - Keep the one in use: the conflict copy is archived (moved into
//     .stversions), so it is one click away in the History screen.
//   - Use the one set aside: the copy in use is archived first, then the
//     conflict copy is renamed over it.
//
// Either way nothing is destroyed and the History screen -- which this build
// seeds on, staggered, thirty days -- is the undo. A folder with versioning
// switched off cannot offer that, so there the operation is refused unless the
// caller passes force, and the interface says plainly that the copy will be
// gone for good.
//
// The resolution propagates on its own: the rename is an ordinary local change
// and the peer sees the conflict copy deleted and the file updated. Only one
// side has to do it.

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/versioner"
)

// conflictMarker is the substring lib/model stamps into a conflict copy's
// name. Upstream's own test for a conflict file is exactly
// strings.Contains(filepath.Base(name), ".sync-conflict-"), so this is the
// same test rather than a stricter one -- a name upstream treats as a conflict
// and this endpoint did not would be a file nothing could ever resolve.
const conflictMarker = ".sync-conflict-"

// conflictTimeFormat is the stamp conflictName writes, in local time.
const conflictTimeFormat = "20060102-150405"

// conflictListCap bounds the rows returned per folder. The count above them is
// exact; the list is for reading.
const conflictListCap = 200

var (
	errNotAConflict     = errors.New("not a conflict copy")
	errConflictGone     = errors.New("that copy is no longer on this computer")
	errWouldDestroyOnly = errors.New("this folder keeps no history, so the copy you do not keep would be deleted permanently")
)

// conflictSide is one of the two files, as it exists on this disk right now.
// Present is false for a copy that is in the index but has not arrived here --
// a folder that has been through the selective-sync picker can be in that
// state, and a choice cannot be offered for bytes that are not here.
type conflictSide struct {
	Present  bool      `json:"present"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// By is the device that last wrote this copy, from the index. Empty when
	// the index has no entry for it. "you" is not special-cased here: the
	// caller is told the device name and knows its own.
	By string `json:"by"`
	// Mine says By is this device.
	Mine bool `json:"mine"`
}

// conflictEntry is one decision to be made.
type conflictEntry struct {
	// Conflict is the index name of the copy that was set aside, and the
	// handle the resolve endpoint takes.
	Conflict string `json:"conflict"`
	// Name is the file it is a conflict of -- what it will be called again if
	// the set-aside copy is the one kept.
	Name string `json:"name"`
	// When the copy was set aside, read from its filename. Zero if the name
	// does not carry a parsable stamp, which no Syncthing-made name fails to
	// do but a hand-copied one might.
	When time.Time `json:"when"`
	// Current is the file in use, Aside the conflict copy.
	Current conflictSide `json:"current"`
	Aside   conflictSide `json:"aside"`
}

// conflictFolder is one folder's answer.
type conflictFolder struct {
	Folder string `json:"folder"`
	Label  string `json:"label"`
	// Count and Bytes are exact and cover everything, whether or not the row
	// made it into Rows.
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
	// Versioning is false when resolving would destroy the copy not kept.
	Versioning bool `json:"versioning"`
	// ReceiveOnly says a resolution here is a local change on a folder that
	// does not send them, so it will sit as a local addition until somebody
	// overrides. Worth saying before the click, not after.
	ReceiveOnly bool            `json:"receiveOnly"`
	Rows        []conflictEntry `json:"rows"`
	Truncated   bool            `json:"truncated"`
}

type conflictsResponse struct {
	Total   int              `json:"total"`
	Folders []conflictFolder `json:"folders"`
}

// getFolderConflicts lists unresolved conflict copies, for one folder with
// ?folder= or for every folder without it. The home screen asks for all of
// them and shows a count per card; the History screen's Conflicts tab asks for
// the rows.
func (s *service) getFolderConflicts(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	folders := s.cfg.Folders()

	var ids []string
	if want := qs.Get("folder"); want != "" {
		if _, ok := folders[want]; !ok {
			forkHTTPError(w, errNoSuchFolder)
			return
		}
		ids = []string{want}
	} else {
		for id := range folders {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	res := conflictsResponse{Folders: []conflictFolder{}}
	for _, id := range ids {
		one, err := s.conflictScan(folders[id])
		if err != nil {
			// One unreadable folder should not blank the others: a folder
			// whose path is missing is exactly the folder somebody is on this
			// screen to find out about.
			continue
		}
		res.Total += one.Count
		if one.Count > 0 || len(ids) == 1 {
			res.Folders = append(res.Folders, one)
		}
	}
	sendJSON(w, res)
}

// conflictScan walks one folder's global index and builds its answer.
//
// The index rather than the disk, for the same reason /rest/db/browse uses it:
// it is the shared view, so a conflict copy that exists on a peer and has not
// been pulled here still appears -- with Present false, which is the honest
// thing to show rather than pretending the folder is clean.
func (s *service) conflictScan(cfg config.FolderConfiguration) (conflictFolder, error) {
	out := conflictFolder{
		Folder:      cfg.ID,
		Label:       cfg.Label,
		Versioning:  cfg.Versioning.Type != "",
		ReceiveOnly: cfg.Type == config.FolderTypeReceiveOnly,
		Rows:        []conflictEntry{},
	}
	if out.Label == "" {
		out.Label = cfg.ID
	}

	names := s.shortDeviceNames()
	ffs := cfg.Filesystem()

	it, errFn := s.model.AllGlobalFiles(cfg.ID)
	for f := range it {
		if f.Deleted || f.Type != protocol.FileInfoTypeFile {
			continue
		}
		if !isConflictName(f.Name) {
			continue
		}

		info := conflictParse(f.Name)
		row := conflictEntry{
			Conflict: f.Name,
			Name:     info.Original,
			When:     info.When,
			Aside:    s.conflictSideOf(ffs, cfg.ID, f.Name, names),
			Current:  s.conflictSideOf(ffs, cfg.ID, info.Original, names),
		}

		out.Count++
		out.Bytes += row.Aside.Size
		if len(out.Rows) < conflictListCap {
			out.Rows = append(out.Rows, row)
		} else {
			out.Truncated = true
		}
	}
	if err := errFn(); err != nil {
		return conflictFolder{}, err
	}

	// Newest first: the conflict somebody is here about is the one that just
	// happened.
	sort.Slice(out.Rows, func(i, j int) bool {
		if !out.Rows[i].When.Equal(out.Rows[j].When) {
			return out.Rows[i].When.After(out.Rows[j].When)
		}
		return out.Rows[i].Conflict < out.Rows[j].Conflict
	})
	return out, nil
}

// conflictSideOf describes one copy: real size and mtime off the disk, author
// off the index.
//
// The split matters. Size and mtime are what the person is comparing, and the
// local index lies about both for anything ignored -- SetIgnored blanks Size
// and drops the block hashes. ModifiedBy is not something the disk knows, and
// the index is the only place it exists.
func (s *service) conflictSideOf(ffs fs.Filesystem, folder, name string, names map[string]string) conflictSide {
	var side conflictSide

	if fi, ok, err := s.model.CurrentGlobalFile(folder, name); err == nil && ok && !fi.Deleted {
		short := fi.ModifiedBy.String()
		if who, found := names[short]; found {
			side.By = who
		} else if short != "" {
			side.By = short
		}
		side.Mine = fi.ModifiedBy == s.id.Short()
	}

	if info, err := ffs.Lstat(name); err == nil && info.IsRegular() {
		side.Present = true
		side.Size = info.Size()
		side.Modified = info.ModTime()
	}
	return side
}

// shortDeviceNames maps a ShortID string to a configured device name, which is
// what a conflict row shows. The local device is included: "who wrote this
// copy" is a fair question to ask about your own machine, and the answer is
// its name.
func (s *service) shortDeviceNames() map[string]string {
	out := map[string]string{}
	for id, dev := range s.cfg.Devices() {
		name := dev.Name
		if name == "" {
			name = id.Short().String()
		}
		out[id.Short().String()] = name
	}
	return out
}

// conflictInfo is what a conflict copy's name can be made to admit.
type conflictInfo struct {
	// Original is the file this is a conflict of, in the same spelling and
	// with the same separators as the name it came from -- it is handed
	// straight back to the filesystem.
	Original string
	When     time.Time
	// Short is the device ID in the name. Deliberately not surfaced: see the
	// file comment. Parsed only so the boundary between stamp and ID is
	// checked, which is what makes a malformed name detectable.
	Short string
}

// isConflictName is upstream's test, verbatim in behaviour.
func isConflictName(name string) bool {
	return strings.Contains(conflictBase(name), conflictMarker)
}

// conflictBase is filepath.Base for an index name.
//
// Index names carry the OS separator: AllGlobalFiles hands back `refs\a.png`
// on Windows and `refs/a.png` elsewhere, and this file is also given names
// from a browser, which may spell either. filepath.Base only knows the local
// separator, so both are cut here. Same reasoning as parentDir in
// api_reclaim.go, which learned it the hard way.
func conflictBase(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

// conflictDir is the other half of the same split, keeping the separator.
func conflictDir(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[:i+1]
	}
	return ""
}

// conflictParse takes a conflict copy's name apart. Pure, so the one piece of
// string surgery that decides which file gets overwritten is provable without
// a folder.
//
// The last marker wins, not the first. A conflict copy of a conflict copy is
// rare but reachable, and its "original" is the intermediate copy rather than
// the file at the bottom of the chain -- resolving it should put back what it
// was named after, one step at a time.
// The extension is taken from *after* the marker, not from the whole name.
// conflictName inserts the stamp before the extension, so for a file with no
// extension at all -- `notes` becomes `notes.sync-conflict-...-F67Q3OS` --
// filepath.Ext of the whole thing returns the entire conflict suffix, and
// splitting on that leaves a stem with no marker in it. The file then looks
// like an ordinary file with an odd name and can never be resolved.
func conflictParse(name string) conflictInfo {
	base := conflictBase(name)

	i := strings.LastIndex(base, conflictMarker)
	if i < 0 {
		return conflictInfo{}
	}
	head, rest := base[:i], base[i+len(conflictMarker):]

	ext := filepath.Ext(rest)
	info := conflictInfo{Original: conflictDir(name) + head + ext}

	tag := rest[:len(rest)-len(ext)]
	if len(tag) >= len(conflictTimeFormat) {
		if t, err := time.ParseInLocation(conflictTimeFormat, tag[:len(conflictTimeFormat)], time.Local); err == nil {
			info.When = t
			if len(tag) > len(conflictTimeFormat) && tag[len(conflictTimeFormat)] == '-' {
				info.Short = tag[len(conflictTimeFormat)+1:]
			}
		}
	}
	return info
}

// conflictResolveRequest is the decision.
type conflictResolveRequest struct {
	Folder string `json:"folder"`
	// Conflict is the index name of the copy that was set aside, exactly as
	// the listing gave it.
	Conflict string `json:"conflict"`
	// Keep is "current" to keep the file in use and archive the copy set
	// aside, or "aside" to do the opposite. Spelled after what the screen
	// shows rather than mine/theirs, which the filename cannot support.
	Keep string `json:"keep"`
	// Force allows the operation on a folder with no versioning, where the
	// copy not kept is deleted rather than archived.
	Force bool `json:"force"`
}

type conflictResolveResponse struct {
	Folder   string `json:"folder"`
	Name     string `json:"name"`
	Conflict string `json:"conflict"`
	Kept     string `json:"kept"`
	// Archived says the copy not kept went to the History screen. False means
	// it was deleted, which only happens with Force on a folder that keeps no
	// history.
	Archived bool `json:"archived"`
	// Remaining is how many conflicts are left in this folder afterwards, so
	// the caller can close the screen when it reaches zero without a second
	// round trip.
	Remaining int `json:"remaining"`
}

// postFolderConflict performs one resolution.
func (s *service) postFolderConflict(w http.ResponseWriter, r *http.Request) {
	var req conflictResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}

	if req.Keep != "current" && req.Keep != "aside" {
		http.Error(w, `keep must be "current" or "aside"`, http.StatusBadRequest)
		return
	}

	// The handle is only ever a conflict copy. This endpoint renames and
	// archives files by name from a browser, and this is the check that keeps
	// it from being a general "move any file over any other file" route.
	if !isConflictName(req.Conflict) {
		http.Error(w, errNotAConflict.Error(), http.StatusBadRequest)
		return
	}
	info := conflictParse(req.Conflict)
	if info.Original == "" {
		http.Error(w, errNotAConflict.Error(), http.StatusBadRequest)
		return
	}

	ffs := cfg.Filesystem()

	// The copy set aside has to be here. Anything else -- already resolved in
	// another tab, resolved on the peer and the deletion arrived, never pulled
	// -- is the same answer, and it is not an error worth a 500.
	if st, err := ffs.Lstat(req.Conflict); err != nil || !st.IsRegular() {
		http.Error(w, errConflictGone.Error(), http.StatusConflict)
		return
	}
	currentExists := false
	if st, err := ffs.Lstat(info.Original); err == nil && st.IsRegular() {
		currentExists = true
	}

	var vers versioner.Versioner
	if cfg.Versioning.Type != "" {
		v, err := versioner.New(cfg)
		if err != nil {
			httpError(w, err)
			return
		}
		vers = v
	}

	// Without a versioner one of the two copies is about to stop existing.
	// Refused by default and allowed with force, so that the sentence "the
	// other copy goes to History" is never printed by an interface that cannot
	// keep the promise.
	discards := req.Keep == "current" || currentExists
	if vers == nil && discards && !req.Force {
		http.Error(w, errWouldDestroyOnly.Error(), http.StatusConflict)
		return
	}

	res := conflictResolveResponse{
		Folder:   req.Folder,
		Name:     info.Original,
		Conflict: req.Conflict,
		Kept:     req.Keep,
	}

	switch req.Keep {
	case "current":
		// Keeping what is in use: the copy set aside is put in the archive and
		// nothing else moves. The file in use is not touched at all, which is
		// why this is the resolution offered first.
		archived, err := conflictRetire(ffs, vers, req.Conflict)
		if err != nil {
			httpError(w, err)
			return
		}
		res.Archived = archived

	case "aside":
		// Using the copy set aside: archive what is in use, then rename over
		// it. Archive first -- archiveFile moves the file out of the way, so
		// the destination is free by the time the rename runs, and an archive
		// that fails leaves both copies where they were.
		if currentExists {
			archived, err := conflictRetire(ffs, vers, info.Original)
			if err != nil {
				httpError(w, err)
				return
			}
			res.Archived = archived
		}
		if err := ffs.Rename(req.Conflict, info.Original); err != nil {
			httpError(w, err)
			return
		}
	}

	// Tell the model now rather than waiting for the next scan. Both names,
	// because both changed, and a targeted scan is what makes the folder card
	// honest by the time the browser re-asks.
	s.model.ScanFolderSubdirs(req.Folder, []string{req.Conflict, info.Original})

	if one, err := s.conflictScan(cfg); err == nil {
		res.Remaining = one.Count
	}
	sendJSON(w, res)
}

// conflictRetire moves a copy out of the folder: into the archive when there
// is one, deleted when there is not. Reports which happened, because the two
// are very different promises and the response says which was made.
func conflictRetire(ffs fs.Filesystem, vers versioner.Versioner, name string) (bool, error) {
	if vers != nil {
		if err := vers.Archive(name); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := ffs.Remove(name); err != nil && !fs.IsNotExist(err) {
		return false, err
	}
	return false, nil
}
